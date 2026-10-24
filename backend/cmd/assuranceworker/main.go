package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/auth"
	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/evalplane/evalplane/backend/internal/verification"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	credentialDir := os.Getenv("OUTCOME_CREDENTIAL_DIR")
	if credentialDir == "" {
		log.Fatal("OUTCOME_CREDENTIAL_DIR is required for scheduled direct acquisition")
	}
	if info, err := os.Stat(credentialDir); err != nil || !info.IsDir() {
		log.Fatal("OUTCOME_CREDENTIAL_DIR must be an existing directory")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	verifier := verification.NewService(pool, verification.NewRepository(), auth.NewRepository())
	service := operational.NewService(pool, auth.NewRepository(), verifier)
	credentials := operational.FileMonitorCredentials{Directory: credentialDir}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		monitored, monitorErr := operational.ProcessNextMonitoring(ctx, pool, service, credentials, os.Getenv("OUTCOME_PROBE_VANTAGE"))
		if monitorErr != nil && ctx.Err() == nil {
			log.Printf("assurance monitoring error: %v", monitorErr)
		}
		if monitored {
			continue
		}
		processed, err := operational.ProcessNextEvaluation(ctx, pool, service)
		if err != nil && ctx.Err() == nil {
			log.Printf("assurance evaluation error: %v", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
