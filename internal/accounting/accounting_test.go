package accounting

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeRepository struct {
	mu      sync.Mutex
	pending []OutboxEntry
	sent    []uuid.UUID
	failed  []uuid.UUID
}

func (f *fakeRepository) FetchPending(ctx context.Context, limit int) ([]OutboxEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.pending
	f.pending = nil
	return out, nil
}

func (f *fakeRepository) MarkSent(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, id)
	return nil
}

func (f *fakeRepository) MarkFailed(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, id)
	return nil
}

type fakeClient struct {
	mu         sync.Mutex
	calls      int
	failNTimes int
}

func (f *fakeClient) Send(ctx context.Context, entry OutboxEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failNTimes {
		return errors.New("simulated 500 from accounting service")
	}
	return nil
}

func TestPublisher_SucceedsAfterTransientFailure(t *testing.T) {
	entryID := uuid.New()
	repo := &fakeRepository{pending: []OutboxEntry{{ID: entryID, IdempotencyKey: uuid.New()}}}
	client := &fakeClient{failNTimes: 2} 
	breaker := NewCircuitBreaker(5, time.Second)

	p := NewPublisher(repo, client, breaker)
	p.processPending(context.Background())

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Equal(t, []uuid.UUID{entryID}, repo.sent)
	assert.Empty(t, repo.failed)
}

func TestPublisher_MarksFailedAfterExhaustedRetries(t *testing.T) {
	entryID := uuid.New()
	repo := &fakeRepository{pending: []OutboxEntry{{ID: entryID, IdempotencyKey: uuid.New()}}}
	client := &fakeClient{failNTimes: 999} 
	breaker := NewCircuitBreaker(5, time.Second)

	p := NewPublisher(repo, client, breaker)
	p.processPending(context.Background())

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Equal(t, []uuid.UUID{entryID}, repo.failed)
	assert.Empty(t, repo.sent)
}

func TestCircuitBreaker_OpensAfterThresholdAndRecoversAfterCooldown(t *testing.T) {
	breaker := NewCircuitBreaker(3, 50*time.Millisecond)

	assert.True(t, breaker.Allow(), "should allow calls when closed")

	breaker.RecordFailure()
	breaker.RecordFailure()
	assert.True(t, breaker.Allow(), "should still allow calls below threshold")

	breaker.RecordFailure()
	assert.False(t, breaker.Allow(), "should reject calls once threshold is hit and cooldown hasn't passed")

	time.Sleep(60 * time.Millisecond)
	assert.True(t, breaker.Allow(), "should allow a trial call again after cooldown passes")

	breaker.RecordSuccess()
	assert.True(t, breaker.Allow(), "should stay closed after a successful trial")
}
