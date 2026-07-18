package accounting

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adeharseno/ticket-booking-system/internal/shared"
)

type OutboxEntry struct {
	ID             uuid.UUID
	TransactionID  uuid.UUID
	Payload        []byte
	Status         string
	AttemptCount   int
	IdempotencyKey uuid.UUID
	CreatedAt      time.Time
}

func SaveOutboxEntryTx(ctx context.Context, tx pgx.Tx, transactionID uuid.UUID, payload []byte) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO outbox (transaction_id, payload, status) VALUES ($1, $2, 'pending')`,
		transactionID, payload,
	)
	return err
}

type Repository interface {
	FetchPending(ctx context.Context, limit int) ([]OutboxEntry, error)
	MarkSent(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID) error
}

type pgRepository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &pgRepository{pool: pool}
}

func (r *pgRepository) FetchPending(ctx context.Context, limit int) ([]OutboxEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, transaction_id, payload, status, attempt_count, idempotency_key, created_at
		 FROM outbox WHERE status = 'pending' ORDER BY created_at ASC LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []OutboxEntry
	for rows.Next() {
		var e OutboxEntry
		if err := rows.Scan(&e.ID, &e.TransactionID, &e.Payload, &e.Status, &e.AttemptCount, &e.IdempotencyKey, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *pgRepository) MarkSent(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox SET status = 'sent', sent_at = now() WHERE id = $1`, id)
	return err
}

func (r *pgRepository) MarkFailed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox SET status = 'failed', attempt_count = attempt_count + 1 WHERE id = $1`, id)
	return err
}

type Client interface {
	Send(ctx context.Context, entry OutboxEntry) error
}

type httpClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTPClient(baseURL string) Client {
	return &httpClient{baseURL: baseURL, http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *httpClient) Send(ctx context.Context, entry OutboxEntry) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/transaction", bytes.NewReader(entry.Payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", entry.IdempotencyKey.String())

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("accounting service returned status %d", resp.StatusCode)
	}
	return nil
}

type CircuitBreaker struct {
	mu           sync.Mutex
	failureCount int
	threshold    int
	cooldown     time.Duration
	openUntil    time.Time
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{threshold: threshold, cooldown: cooldown}
}

func (b *CircuitBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.failureCount < b.threshold {
		return true
	}
	return time.Now().After(b.openUntil)
}

func (b *CircuitBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount = 0
}

func (b *CircuitBreaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount++
	if b.failureCount >= b.threshold {
		b.openUntil = time.Now().Add(b.cooldown)
	}
}

type Publisher struct {
	repo    Repository
	client  Client
	breaker *CircuitBreaker
}

func NewPublisher(repo Repository, client Client, breaker *CircuitBreaker) *Publisher {
	return &Publisher{repo: repo, client: client, breaker: breaker}
}

func (p *Publisher) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.processPending(ctx)
		}
	}
}

func (p *Publisher) processPending(ctx context.Context) {
	entries, err := p.repo.FetchPending(ctx, 20)
	if err != nil {
		log.Printf("accounting publisher: failed to fetch pending entries: %v", err)
		return
	}

	for _, entry := range entries {
		if !p.breaker.Allow() {
			log.Printf("accounting publisher: circuit breaker open, skipping entry %s", entry.ID)
			continue
		}

		sendErr := shared.WithBackoff(ctx, 3, func() error {
			return p.client.Send(ctx, entry)
		})

		if sendErr != nil {
			p.breaker.RecordFailure()
			if err := p.repo.MarkFailed(ctx, entry.ID); err != nil {
				log.Printf("accounting publisher: failed to mark entry %s as failed: %v", entry.ID, err)
			}
			log.Printf("accounting publisher: entry %s failed after retries: %v", entry.ID, sendErr)
			continue
		}

		p.breaker.RecordSuccess()
		if err := p.repo.MarkSent(ctx, entry.ID); err != nil {
			log.Printf("accounting publisher: failed to mark entry %s as sent: %v", entry.ID, err)
		}
	}
}
