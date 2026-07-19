package sync

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AvailabilityUpdate struct {
	TicketID uuid.UUID `json:"ticket_id"`
	Quantity int       `json:"quantity"`
	Version  int64     `json:"version"`
}

type Repository interface {
	ApplyIfNewer(ctx context.Context, update AvailabilityUpdate) (bool, error)
}

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) ApplyIfNewer(ctx context.Context, update AvailabilityUpdate) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO ticket_sync_state (ticket_id, quantity, version)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (ticket_id) DO UPDATE
		 SET quantity = EXCLUDED.quantity, version = EXCLUDED.version, updated_at = now()
		 WHERE ticket_sync_state.version < EXCLUDED.version`,
		update.TicketID, update.Quantity, update.Version,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ApplyUpdate(ctx context.Context, update AvailabilityUpdate) (bool, error) {
	applied, err := s.repo.ApplyIfNewer(ctx, update)
	if err != nil {
		return false, err
	}
	if !applied {
		log.Printf("sync: discarded stale update for ticket %s (version %d)", update.TicketID, update.Version)
	}
	return applied, nil
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Sync(w http.ResponseWriter, r *http.Request) {
	var update AvailabilityUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	applied, err := h.svc.ApplyUpdate(r.Context(), update)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to apply update"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"applied": applied})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
