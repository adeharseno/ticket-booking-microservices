package sync

// Section 5 - Data Synchronization (out-of-order updates)
//
// The network doesn't guarantee delivery order. An update with quantity=2
// can arrive before an earlier update with quantity=5, and if we just
// apply whatever shows up last, the destination ends up with stale data.
//
// Fix: never trust arrival order, trust a version number generated at the
// source when the update was created. Only apply an incoming update if its
// version is strictly greater than whatever's currently stored for that
// ticket_id; otherwise discard it silently - it's not an error, it's just
// old news.
//
// One wrinkle: the first update for a given ticket_id has nothing to
// compare against yet, so this uses INSERT ... ON CONFLICT DO UPDATE
// instead of a plain UPDATE, handling "first time" and "newer version"
// in a single atomic statement:
//
//   INSERT INTO ticket_sync_state (ticket_id, quantity, version)
//   VALUES ($1, $2, $3)
//   ON CONFLICT (ticket_id) DO UPDATE
//   SET quantity = EXCLUDED.quantity, version = EXCLUDED.version
//   WHERE ticket_sync_state.version < EXCLUDED.version
//
// If the row didn't exist, it's inserted (1 row affected). If it existed
// and the incoming version is newer, it's updated (1 row affected). If it
// existed and the incoming version is stale, nothing happens (0 rows
// affected) - Postgres detects the conflict, evaluates the WHERE clause,
// finds it false, and skips the update without inserting a duplicate.

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---- model ----

type AvailabilityUpdate struct {
	TicketID uuid.UUID `json:"ticket_id"`
	Quantity int       `json:"quantity"`
	Version  int64     `json:"version"`
}

// ---- repository ----

type Repository interface {
	// ApplyIfNewer applies the update only if its version is newer than
	// what's currently stored. Returns false (not an error) if the update
	// was stale and got discarded.
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

// ---- service ----

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

// ---- handler ----

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Sync handles POST /sync/ticket-availability. A stale update isn't an
// error from the caller's point of view either - it just gets ignored,
// reflected in the "applied": false response.
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
