package ticket

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeRepository struct {
	mu    sync.Mutex
	stock int
}

func (f *fakeRepository) DecrementStock(ctx context.Context, ticketID uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.stock <= 0 {
		return false, nil
	}
	f.stock--
	return true, nil
}

type fakePublisher struct {
	enqueued int32
}

func (f *fakePublisher) Enqueue(ctx context.Context, ticketID, userID uuid.UUID) error {
	atomic.AddInt32(&f.enqueued, 1)
	return nil
}

func TestPurchase_OnlyOneSucceedsWhenStockIsOne(t *testing.T) {
	repo := &fakeRepository{stock: 1}
	publisher := &fakePublisher{}
	svc := NewService(repo, publisher)

	const numRequests = 20
	var successCount int32
	var soldOutCount int32
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Purchase(context.Background(), PurchaseRequest{
				TicketID: uuid.New(),
				UserID:   uuid.New(),
			})
			switch {
			case err == nil:
				atomic.AddInt32(&successCount, 1)
			case errors.Is(err, ErrSoldOut):
				atomic.AddInt32(&soldOutCount, 1)
			}
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, successCount, "exactly 1 purchase should succeed when stock is 1")
	assert.EqualValues(t, numRequests-1, soldOutCount, "the rest should receive ErrSoldOut")
	assert.EqualValues(t, 1, publisher.enqueued, "only the successful purchase should be enqueued for recording")
}
