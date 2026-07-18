# Architecture Notes

---

# 1. Preventing Race Conditions

## Problem

If multiple users try to buy the last ticket at the same time, a simple **read → check → update** flow can oversell tickets because multiple requests may read the same stock value before it is updated.

## Solution

The purchase uses a single SQL statement to decrease stock only when stock is still available.

```sql
UPDATE tickets
SET stock = stock - 1
WHERE id = $1
  AND stock > 0;
```

After executing the query, the application checks RowsAffected().

1 → purchase succeeds.
0 → ticket is already sold out.
The stock decrement is the only thing that has to stay synchronous - that's what actually fixes the race condition. Once it succeeds, the purchase is enqueued to the same queue Section 2 consumes from, which handles writing the transaction record (and, since Section 3, the outbox row) asynchronously.

## Why this approach?

I considered a few alternatives:

**SELECT ... FOR UPDATE**
Prevents concurrent updates but causes other requests to wait for the row lock.
**Optimistic locking**
Useful for more complex update flows, but unnecessary for a single stock decrement.
**Distributed lock (Redis)**
Suitable for coordinating across multiple services, but adds extra infrastructure that isn't needed here.
For this use case, a conditional UPDATE is simple, efficient, and prevents overselling without introducing additional complexity.

---

# 2. Handling High Traffic

## Problem

Writing every incoming request directly to the database can increase response time and put pressure on the database connection pool during traffic spikes.

## Solution

Incoming requests are validated and pushed into a queue.

The API immediately returns **202 Accepted**, while a background consumer reads from the queue and saves transactions to the database.

If a database operation fails temporarily, the consumer retries using exponential backoff.

If all retry attempts fail, the request is stored in a dead-letter table so it can be investigated later instead of being silently lost.

## Current Limitation

The queue used in this project is an in-memory Go channel.

This keeps the implementation simple for the assessment and is sufficient to demonstrate asynchronous processing, but it is not durable. Messages still waiting in memory would be lost if the application crashes.

For a production system, the queue implementation could be replaced with RabbitMQ or Kafka without changing the application logic because the application already depends on a Queue interface.

## Trade-offs

| Decision | Reason |
|----------|--------|
| Atomic SQL update | Prevents overselling while keeping the implementation simple |
| Queue-based processing | Reduces request latency and database pressure during traffic spikes |
| Retry with dead-letter | Makes temporary failures recoverable and keeps failed messages visible |
| In-memory queue | Easy to implement for the assessment, but not crash-durable for production |

---

# Section 3 — External API Integration

## Problem

Every successful transaction must be sent to the accounting service. The problem is that the service can be slow, unavailable, or return errors. Calling it directly in the request flow would make ticket purchases depend on a third-party service. Another issue is losing the event if the transaction is committed but the process crashes before the API call is made.

## Solution

I used the **Transactional Outbox** pattern.

When a transaction is saved, an outbox record is inserted in the same PostgreSQL transaction. That guarantees both records are committed together.

```go
err = tx.QueryRow(ctx,
    `INSERT INTO transactions (...) RETURNING id`,
    req.TicketID, req.UserID,
).Scan(&transactionID)

accounting.SaveOutboxEntryTx(ctx, tx, transactionID, payload)
return tx.Commit(ctx)
```

A separate worker polls pending outbox records and sends them to the accounting API.

Flow:

- Skip requests while the circuit breaker is open.
- Retry failed requests with exponential backoff.
- Mark successful records as `sent`.
- Mark exhausted retries as `failed`.

Each request includes an `Idempotency-Key` using the outbox UUID, so retries won't create duplicate records if the accounting service supports idempotency.

The circuit breaker is intentionally simple. After reaching a failure threshold, requests are paused for a cooldown period before allowing another attempt.

A small mock accounting server (`cmd/mockaccounting`) is included to simulate failures and verify retry/circuit breaker behavior locally.

## Worker Process

Unlike the reservation consumer, the outbox publisher runs as a separate process (`RUN_MODE=worker`) because it reads from PostgreSQL instead of an in-memory channel.

## Trade-offs

| Decision | Reason |
| --- | --- |
| Transactional Outbox | Prevents losing events after a successful transaction. |
| Polling worker | Simple implementation with a small delivery delay. |
| Simple circuit breaker | Enough to avoid hammering an unavailable service. |
| Idempotency key | Prevents duplicate processing if supported by the third-party API. |

---

## Section 4 - Duplicate request

## Section 5 - Data synchronization