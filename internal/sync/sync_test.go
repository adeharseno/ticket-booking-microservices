package sync

import (
	"context"
	"math/rand"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeRepository struct {
	mu    sync.Mutex
	state map[uuid.UUID]AvailabilityUpdate
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{state: make(map[uuid.UUID]AvailabilityUpdate)}
}

func (f *fakeRepository) ApplyIfNewer(ctx context.Context, update AvailabilityUpdate) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	current, exists := f.state[update.TicketID]
	if exists && current.Version >= update.Version {
		return false, nil
	}
	f.state[update.TicketID] = update
	return true, nil
}

func TestApplyUpdate_DiscardsLateArrivingStaleUpdate(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)
	ticketID := uuid.New()
	ctx := context.Background()

	applied, err := svc.ApplyUpdate(ctx, AvailabilityUpdate{TicketID: ticketID, Quantity: 5, Version: 2})
	assert.NoError(t, err)
	assert.True(t, applied)

	applied, err = svc.ApplyUpdate(ctx, AvailabilityUpdate{TicketID: ticketID, Quantity: 2, Version: 5})
	assert.NoError(t, err)
	assert.True(t, applied)

	applied, err = svc.ApplyUpdate(ctx, AvailabilityUpdate{TicketID: ticketID, Quantity: 9, Version: 3})
	assert.NoError(t, err)
	assert.False(t, applied, "a late-arriving update with an older version should be discarded")

	repo.mu.Lock()
	final := repo.state[ticketID]
	repo.mu.Unlock()

	assert.Equal(t, 2, final.Quantity, "final quantity should reflect the highest version seen, not arrival order")
	assert.Equal(t, int64(5), final.Version)
}

func TestApplyUpdate_ConcurrentUpdatesConvergeToHighestVersion(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo)
	ticketID := uuid.New()

	const numUpdates = 30
	versions := rand.Perm(numUpdates)
	var wg sync.WaitGroup

	for _, v := range versions {
		version := int64(v + 1) 
		wg.Add(1)
		go func(version int64) {
			defer wg.Done()
			_, err := svc.ApplyUpdate(context.Background(), AvailabilityUpdate{
				TicketID: ticketID,
				Quantity: int(version),
				Version:  version,
			})
			assert.NoError(t, err)
		}(version)
	}
	wg.Wait()

	repo.mu.Lock()
	final := repo.state[ticketID]
	repo.mu.Unlock()

	assert.EqualValues(t, numUpdates, final.Version, "should converge to the highest version regardless of arrival order")
	assert.Equal(t, numUpdates, final.Quantity)
}
