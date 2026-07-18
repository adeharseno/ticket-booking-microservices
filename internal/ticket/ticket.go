package ticket

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
	TicketID uuid.UUID `json:"ticket_id"`
	Status   string    `json:"status"`
}

type Repository interface {
	DecrementStock(ctx context.Context, ticketID uuid.UUID) (bool, error)
}

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) DecrementStock(ctx context.Context, ticketID uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE tickets SET stock = stock - 1 WHERE id = $1 AND stock > 0`,
		ticketID,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

type TransactionPublisher interface {
	Enqueue(ctx context.Context, ticketID, userID uuid.UUID) error
}

type Service struct {
	repo      Repository
	publisher TransactionPublisher
}

func NewService(repo Repository, publisher TransactionPublisher) *Service {
	return &Service{repo: repo, publisher: publisher}
}

func (s *Service) Purchase(ctx context.Context, req PurchaseRequest) (*PurchaseResult, error) {
	ok, err := s.repo.DecrementStock(ctx, req.TicketID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrSoldOut
	}

	if err := s.publisher.Enqueue(ctx, req.TicketID, req.UserID); err != nil {
		log.Printf("ticket: purchase succeeded but failed to enqueue transaction for ticket %s: %v", req.TicketID, err)
	}

	return &PurchaseResult{TicketID: req.TicketID, Status: "success"}, nil
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
