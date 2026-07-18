package webhook

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeRepository struct {
	mu    sync.Mutex
	saved map[string]PaymentWebhookPayload
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{saved: make(map[string]PaymentWebhookPayload)}
}

func (f *fakeRepository) InsertPayment(ctx context.Context, payload PaymentWebhookPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.saved[payload.PaymentID]; exists {
		return nil 
	}
	f.saved[payload.PaymentID] = payload
	return nil
}

type fakeIdempotencyStore struct {
	mu   sync.Mutex
	seen map[string]bool
}

func newFakeIdempotencyStore() *fakeIdempotencyStore {
	return &fakeIdempotencyStore{seen: make(map[string]bool)}
}

func (f *fakeIdempotencyStore) SetIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen[key] {
		return false, nil
	}
	f.seen[key] = true
	return true, nil
}

type alwaysNewStore struct{}

func (alwaysNewStore) SetIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return true, nil
}

func TestHandlePayment_DuplicateCaughtByFastPath(t *testing.T) {
	repo := newFakeRepository()
	store := newFakeIdempotencyStore()
	svc := NewService(repo, store)

	payload := PaymentWebhookPayload{
		PaymentID:     "pay_123",
		TransactionID: uuid.New(),
		Amount:        150000,
		Status:        "success",
	}

	const numRequests = 20
	var wg sync.WaitGroup
	errs := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = svc.HandlePayment(context.Background(), payload)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		assert.NoError(t, err, "every call should return success, duplicates included")
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Len(t, repo.saved, 1, "only one row should ever be saved for the same payment_id")
}

func TestHandlePayment_DuplicateCaughtByUniqueConstraintWhenFastPathFails(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo, alwaysNewStore{}) 

	payload := PaymentWebhookPayload{
		PaymentID:     "pay_456",
		TransactionID: uuid.New(),
		Amount:        75000,
		Status:        "success",
	}

	const numRequests = 20
	var wg sync.WaitGroup

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.HandlePayment(context.Background(), payload)
		}()
	}
	wg.Wait()

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Len(t, repo.saved, 1, "the unique constraint should still prevent duplicates even if the fast path never caught them")
}
