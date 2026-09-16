package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/evalplane/evalplane/backend/internal/config"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
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
	case "down":
		count, err := downCount(args)
		if err != nil {
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
