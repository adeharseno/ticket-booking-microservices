package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/adeharseno/ticket-booking-system/internal/accounting"
	"github.com/adeharseno/ticket-booking-system/internal/shared"
	"github.com/adeharseno/ticket-booking-system/internal/sync"
	"github.com/adeharseno/ticket-booking-system/internal/ticket"
	"github.com/adeharseno/ticket-booking-system/internal/transaction"
	"github.com/adeharseno/ticket-booking-system/internal/webhook"
)

type txPublisherAdapter struct {
	svc *transaction.Service
}

func (a *txPublisherAdapter) Enqueue(ctx context.Context, ticketID, userID uuid.UUID) error {
	return a.svc.Enqueue(ctx, transaction.TransactionRequest{
		TicketID: ticketID,
		UserID:   userID,
	})
}

func main() {
	_ = godotenv.Load()

	mode := os.Getenv("RUN_MODE")

	switch mode {
	case "worker":
		runWorker()
	default:
		runAPI()
	}
}

func runAPI() {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://ticketing:ticketing@localhost:5432/ticketing"
	}

	pool, err := shared.NewPostgresPool(ctx, dsn)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	txQueue := shared.NewInMemoryQueue(1000)
	txRepo := transaction.NewRepository(pool)
	txSvc := transaction.NewService(txQueue)
	txHandler := transaction.NewHandler(txSvc)

	go func() {
		if err := transaction.RunConsumer(ctx, txQueue, txRepo); err != nil {
			log.Printf("transaction consumer stopped: %v", err)
		}
	}()

	ticketRepo := ticket.NewRepository(pool)
	ticketSvc := ticket.NewService(ticketRepo, &txPublisherAdapter{svc: txSvc})
	ticketHandler := ticket.NewHandler(ticketSvc)

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisClient := shared.NewRedisClient(redisAddr)

	webhookRepo := webhook.NewRepository(pool)
	idempotencyStore := webhook.NewRedisIdempotencyStore(redisClient)
	webhookSvc := webhook.NewService(webhookRepo, idempotencyStore)
	webhookHandler := webhook.NewHandler(webhookSvc)

	syncRepo := sync.NewRepository(pool)
	syncSvc := sync.NewService(syncRepo)
	syncHandler := sync.NewHandler(syncSvc)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/tickets/purchase", ticketHandler.Purchase)
	r.Post("/transactions", txHandler.Create)
	r.Post("/webhooks/payment", webhookHandler.Payment)
	r.Post("/sync/ticket-availability", syncHandler.Sync)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("API server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}

func runWorker() {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://ticketing:ticketing@localhost:5432/ticketing"
	}

	pool, err := shared.NewPostgresPool(ctx, dsn)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	accountingURL := os.Getenv("ACCOUNTING_API_URL")
	if accountingURL == "" {
		accountingURL = "http://localhost:9999"
	}

	accountingRepo := accounting.NewRepository(pool)
	accountingClient := accounting.NewHTTPClient(accountingURL)
	breaker := accounting.NewCircuitBreaker(5, 30*time.Second)
	publisher := accounting.NewPublisher(accountingRepo, accountingClient, breaker)

	log.Printf("worker mode: polling outbox every 5s, sending to %s", accountingURL)
	publisher.Run(ctx, 5*time.Second)
}
