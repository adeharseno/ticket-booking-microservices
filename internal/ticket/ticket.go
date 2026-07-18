package ticket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSoldOut = errors.New("ticket sold out")

type Ticket struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Stock int       `json:"stock"`
}

type PurchaseRequest struct {
	TicketID uuid.UUID `json:"ticket_id"`
	UserID   uuid.UUID `json:"user_id"`
}

type PurchaseResult struct {
	TransactionID uuid.UUID `json:"transaction_id"`
	TicketID      uuid.UUID `json:"ticket_id"`
	Status        string    `json:"status"`
}

type Repository interface {
	Purchase(ctx context.Context, ticketID, userID uuid.UUID) (uuid.UUID, error)
}

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) Purchase(ctx context.Context, ticketID, userID uuid.UUID) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE tickets SET stock = stock - 1 WHERE id = $1 AND stock > 0`,
		ticketID,
	)
	if err != nil {
		return uuid.Nil, err
	}
	if tag.RowsAffected() == 0 {
		return uuid.Nil, ErrSoldOut
	}

	var transactionID uuid.UUID
	err = tx.QueryRow(ctx,
		`INSERT INTO transactions (ticket_id, user_id, status) VALUES ($1, $2, 'success') RETURNING id`,
		ticketID, userID,
	).Scan(&transactionID)
	if err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return transactionID, nil
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Purchase(ctx context.Context, req PurchaseRequest) (*PurchaseResult, error) {
	transactionID, err := s.repo.Purchase(ctx, req.TicketID, req.UserID)
	if err != nil {
		return nil, err
	}
	return &PurchaseResult{
		TransactionID: transactionID,
		TicketID:      req.TicketID,
		Status:        "success",
	}, nil
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Purchase(w http.ResponseWriter, r *http.Request) {
	var req PurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	result, err := h.svc.Purchase(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrSoldOut) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "ticket sold out"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
