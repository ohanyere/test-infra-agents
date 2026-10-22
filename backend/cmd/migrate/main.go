package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/evalplane/evalplane/backend/internal/config"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationsURL = "file://migrations"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("migration command failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	if len(args) == 0 {
		return errors.New("usage: migrate <up|down|version> [count]")
	}

	databaseURL, err := config.LoadDatabaseURL()
	if err != nil {
		return err
	}

	migrator, err := migrate.New(migrationsURL, databaseURL)
	if err != nil {
		// Driver errors can include the connection URL, so do not return the raw error.
		return errors.New("initialize migration driver")
	}
	defer func() {
		sourceErr, databaseErr := migrator.Close()
		if sourceErr != nil {
			logger.Warn("close migration source", "error", sourceErr)
		}
		if databaseErr != nil {
			logger.Warn("close migration database", "error", databaseErr)
		}
	}()

	switch args[0] {
	case "up":
		if len(args) != 1 {
			return errors.New("usage: migrate up")
		}
		logger.Info("applying all pending migrations")
		if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("apply migrations: %w", err)
		}
		version, dirty, versionErr := migrator.Version()
		if versionErr != nil || dirty || version != 39 {
			return errors.New("migration did not reach clean version 39; inspect actual schema and migration state")
		}
		if err := verifyReleaseSchema(databaseURL); err != nil {
			return err
		}
	case "down":
		count, err := downCount(args)
		if err != nil {
			return err
		}
		version, dirty, versionErr := migrator.Version()
		if versionErr != nil {
			return fmt.Errorf("read migration version before rollback: %w", versionErr)
		}
		if dirty {
			return errors.New("migration state is dirty; inspect actual schema before any rollback")
		}
		if err := preflightRollback(databaseURL, int(version), count); err != nil {
			return err
		}
		logger.Info("rolling back migrations", "count", count)
		if err := migrator.Steps(-count); err != nil {
			return fmt.Errorf("roll back migrations: %w", err)
		}
	case "version":
		if len(args) != 1 {
			return errors.New("usage: migrate version")
		}
		version, dirty, err := migrator.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			logger.Info("migration version", "version", 0, "dirty", false)
			return nil
		}
		if err != nil {
			return fmt.Errorf("read migration version: %w", err)
		}
		logger.Info("migration version", "version", version, "dirty", dirty)
	default:
		return fmt.Errorf("unknown command %q; expected up, down, or version", args[0])
	}

	return nil
}

func verifyReleaseSchema(databaseURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errors.New("initialize release schema check")
	}
	defer pool.Close()
	for _, table := range []string{"customer_outcome_sources", "customer_direct_outcome_observations", "assurance_monitoring_plans", "operational_episodes", "operational_guidance_operation_links"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil || !exists {
			return fmt.Errorf("release schema check failed: table %s is missing or unreadable", table)
		}
	}
	for _, table := range []string{"customer_outcome_sources", "customer_direct_outcome_observations"} {
		var definition string
		if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, table, table+"_source_kind_check").Scan(&definition); err != nil || !strings.Contains(definition, "external_reachability") {
			return fmt.Errorf("release schema check failed: %s reachability constraint is missing or unreadable", table)
		}
	}
	return nil
}

func preflightRollback(databaseURL string, version, count int) error {
	if version-count >= 39 || version < 37 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errors.New("initialize rollback preflight connection")
	}
	defer pool.Close()
	checks := []struct {
		version       int
		query, reason string
	}{
		{39, `SELECT EXISTS(SELECT 1 FROM customer_outcome_sources WHERE source_kind='external_reachability') OR EXISTS(SELECT 1 FROM customer_direct_outcome_observations WHERE source_kind='external_reachability')`, "migration 39 rollback would conflict with retained reachability sources or observations"},
		{38, `SELECT EXISTS(SELECT 1 FROM customer_outcome_sources WHERE source_kind IN ('kubernetes_service','kubernetes_ingress','kubernetes_networkpolicy')) OR EXISTS(SELECT 1 FROM customer_direct_outcome_observations WHERE source_kind IN ('kubernetes_service','kubernetes_ingress','kubernetes_networkpolicy'))`, "migration 38 rollback would conflict with retained Kubernetes resource sources or observations"},
		{37, `SELECT EXISTS(SELECT 1 FROM operational_lesson_decisions WHERE decision IN ('rejected','retired')) OR EXISTS(SELECT 1 FROM operational_guidance_operation_links) OR EXISTS(SELECT 1 FROM operational_lessons WHERE supersedes_lesson_id IS NOT NULL)`, "migration 37 rollback would conflict with retained lesson decisions, corrections, or guidance links"},
	}
	for _, check := range checks {
		if version < check.version || version-count >= check.version {
			continue
		}
		var incompatible bool
		if err := pool.QueryRow(ctx, check.query).Scan(&incompatible); err != nil {
			return errors.New("rollback preflight could not inspect retained data; no migration was attempted")
		}
		if incompatible {
			return errors.New(check.reason + "; preserve data and use a reviewed forward repair")
		}
	}
	return nil
}

func downCount(args []string) (int, error) {
	if len(args) == 1 {
		return 1, nil
	}
	if len(args) != 2 {
		return 0, errors.New("usage: migrate down [count]")
	}
	count, err := strconv.Atoi(args[1])
	if err != nil || count < 1 {
		return 0, errors.New("down count must be a positive integer")
	}
	return count, nil
}
