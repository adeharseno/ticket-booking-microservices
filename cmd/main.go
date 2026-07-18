package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"

	"github.com/adeharseno/ticket-booking-system/internal/shared"
	"github.com/adeharseno/ticket-booking-system/internal/ticket"
)

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
	
	ticketRepo := ticket.NewRepository(pool)
	ticketSvc := ticket.NewService(ticketRepo)
	ticketHandler := ticket.NewHandler(ticketSvc)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/tickets/purchase", ticketHandler.Purchase)

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
	log.Println("worker mode: nothing to run yet (Section 2/3 pending)")
	select {}
}
