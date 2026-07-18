package transaction

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/adeharseno/ticket-booking-system/internal/shared"
)

type fakeRepository struct {
	mu          sync.Mutex
	saved       []TransactionRequest
	deadLetters [][]byte
	failNTimes  int
	callCount   int
}

func (f *fakeRepository) Save(ctx context.Context, req TransactionRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.callCount <= f.failNTimes {
		return errors.New("simulated transient db error")
	}
	f.saved = append(f.saved, req)
	return nil
}

func (f *fakeRepository) SaveDeadLetter(ctx context.Context, payload []byte, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deadLetters = append(f.deadLetters, payload)
	return nil
}

func TestEnqueueAndConsume_SucceedsAfterTransientFailure(t *testing.T) {
	queue := shared.NewInMemoryQueue(10)
	repo := &fakeRepository{failNTimes: 2} 
	svc := NewService(queue)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go RunConsumer(ctx, queue, repo)

	err := svc.Enqueue(ctx, TransactionRequest{TicketID: uuid.New(), UserID: uuid.New()})
	assert.NoError(t, err)

	time.Sleep(1500 * time.Millisecond) 

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Len(t, repo.saved, 1, "transaction should eventually be saved after transient failures")
	assert.Len(t, repo.deadLetters, 0, "should not go to dead-letter since it succeeded within the retry budget")
}

func TestConsume_RoutesToDeadLetterAfterExhaustedRetries(t *testing.T) {
	queue := shared.NewInMemoryQueue(10)
	repo := &fakeRepository{failNTimes: 999} 
	svc := NewService(queue)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go RunConsumer(ctx, queue, repo)

	err := svc.Enqueue(ctx, TransactionRequest{TicketID: uuid.New(), UserID: uuid.New()})
	assert.NoError(t, err)

	time.Sleep(1500 * time.Millisecond)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Len(t, repo.saved, 0)
	assert.Len(t, repo.deadLetters, 1, "should be routed to dead-letter after exhausting retries")
}
