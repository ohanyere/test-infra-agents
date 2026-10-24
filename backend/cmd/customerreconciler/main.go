package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/customercollector"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		_, processed, err := customercollector.ProcessNextAuditReconciliation(ctx, pool)
		if err != nil && ctx.Err() == nil {
			log.Printf("audit reconciliation error: %v", err)
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
