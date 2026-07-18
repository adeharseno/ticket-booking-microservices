package transaction

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adeharseno/ticket-booking-system/internal/accounting"
	"github.com/adeharseno/ticket-booking-system/internal/shared"
)

type Transaction struct {
	ID        uuid.UUID `json:"id"`
	TicketID  uuid.UUID `json:"ticket_id"`
	UserID    uuid.UUID `json:"user_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type TransactionRequest struct {
	TicketID uuid.UUID `json:"ticket_id"`
	UserID   uuid.UUID `json:"user_id"`
}

type Repository interface {
	Save(ctx context.Context, req TransactionRequest) error
	SaveDeadLetter(ctx context.Context, payload []byte, reason string) error
}

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) Save(ctx context.Context, req TransactionRequest) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) 

	var transactionID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (ticket_id, user_id, status) VALUES ($1, $2, 'success') RETURNING id`,
		req.TicketID, req.UserID,
	).Scan(&transactionID)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := accounting.SaveOutboxEntryTx(ctx, tx, transactionID, payload); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *pgRepository) SaveDeadLetter(ctx context.Context, payload []byte, reason string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO dead_letter_transactions (payload, error_reason) VALUES ($1, $2)`,
		payload, reason,
	)
	return err
}

type Service struct {
	queue shared.Queue
}

func NewService(queue shared.Queue) *Service {
	return &Service{queue: queue}
}

func (s *Service) Enqueue(ctx context.Context, req TransactionRequest) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return s.queue.Publish(ctx, shared.Message{ID: uuid.NewString(), Payload: payload})
}

func RunConsumer(ctx context.Context, queue shared.Queue, repo Repository) error {
	msgs, err := queue.Consume(ctx)
	if err != nil {
		return err
	}

	const maxAttempts = 3

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-msgs:
			var req TransactionRequest
			if err := json.Unmarshal(msg.Payload, &req); err != nil {
				log.Printf("transaction consumer: invalid payload %s, sending to dead-letter: %v", msg.ID, err)
				_ = repo.SaveDeadLetter(ctx, msg.Payload, err.Error())
				continue
			}

			saveErr := shared.WithBackoff(ctx, maxAttempts, func() error {
				return repo.Save(ctx, req)
			})
			if saveErr != nil {
				log.Printf("transaction consumer: exhausted retries for %s, sending to dead-letter: %v", msg.ID, saveErr)
				_ = repo.SaveDeadLetter(ctx, msg.Payload, saveErr.Error())
			}
		}
	}
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req TransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := h.svc.Enqueue(r.Context(), req); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to accept transaction"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
