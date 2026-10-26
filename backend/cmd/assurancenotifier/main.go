package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/evalplane/evalplane/backend/internal/operational"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	endpoint := os.Getenv("ASSURANCE_WEBHOOK_URL")
	secret := os.Getenv("ASSURANCE_WEBHOOK_SECRET")
	if databaseURL == "" || endpoint == "" || len(secret) < 16 {
		log.Fatal("DATABASE_URL, ASSURANCE_WEBHOOK_URL, and a signing secret of at least 16 bytes are required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	sender := operational.WebhookSender{Endpoint: endpoint, Secret: []byte(secret)}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		processed, err := operational.ProcessNextDelivery(ctx, pool, sender)
		if err != nil && ctx.Err() == nil {
			log.Printf("assurance notification error: %v", err)
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
