package webhook

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type PaymentWebhookPayload struct {
	PaymentID     string    `json:"payment_id"`
	TransactionID uuid.UUID `json:"transaction_id"`
	Amount        float64   `json:"amount"`
	Status        string    `json:"status"`
}

type Repository interface {
	InsertPayment(ctx context.Context, payload PaymentWebhookPayload) error
}

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) InsertPayment(ctx context.Context, payload PaymentWebhookPayload) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO transaction_payment (payment_id, transaction_id, amount, status)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (payment_id) DO NOTHING`,
		payload.PaymentID, payload.TransactionID, payload.Amount, payload.Status,
	)
	return err
}

type IdempotencyStore interface {
	SetIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

type redisIdempotencyStore struct {
	client *redis.Client
}

func NewRedisIdempotencyStore(client *redis.Client) IdempotencyStore {
	return &redisIdempotencyStore{client: client}
}

func (s *redisIdempotencyStore) SetIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, "1", ttl).Result()
}

type Service struct {
	repo  Repository
	store IdempotencyStore
}

func NewService(repo Repository, store IdempotencyStore) *Service {
	return &Service{repo: repo, store: store}
}

func (s *Service) HandlePayment(ctx context.Context, payload PaymentWebhookPayload) error {
	isNew, err := s.store.SetIfNotExists(ctx, "webhook:payment:"+payload.PaymentID, time.Hour)
	if err != nil {
		log.Printf("webhook: idempotency store check failed for %s, falling back to DB constraint: %v", payload.PaymentID, err)
	} else if !isNew {
		return nil
	}

	return s.repo.InsertPayment(ctx, payload)
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Payment(w http.ResponseWriter, r *http.Request) {
	var payload PaymentWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if payload.PaymentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payment_id is required"})
		return
	}

	if err := h.svc.HandlePayment(r.Context(), payload); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process payment webhook"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
