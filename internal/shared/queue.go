package shared

import "context"

type Message struct {
	ID      string
	Payload []byte
}

type Queue interface {
	Publish(ctx context.Context, msg Message) error
	Consume(ctx context.Context) (<-chan Message, error)
}

type InMemoryQueue struct {
	ch chan Message
}

func NewInMemoryQueue(buffer int) *InMemoryQueue {
	return &InMemoryQueue{ch: make(chan Message, buffer)}
}

func (q *InMemoryQueue) Publish(ctx context.Context, msg Message) error {
	select {
	case q.ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *InMemoryQueue) Consume(ctx context.Context) (<-chan Message, error) {
	return q.ch, nil
}
