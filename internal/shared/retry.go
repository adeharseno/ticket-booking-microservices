package shared

import (
	"context"
	"math"
	"math/rand"
	"time"
)

func WithBackoff(ctx context.Context, maxAttempts int, fn func() error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if attempt == maxAttempts-1 {
			break
		}

		backoff := time.Duration(math.Pow(2, float64(attempt))) * 100 * time.Millisecond
		jitter := time.Duration(rand.Int63n(int64(50 * time.Millisecond)))

		select {
		case <-time.After(backoff + jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
