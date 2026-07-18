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
After executing the query, the application checks RowsAffected().

1 → purchase succeeds.
0 → ticket is already sold out.
The stock update and transaction record are executed inside the same database transaction, so they either both succeed or both roll back.

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

---

# Trade-offs

| Decision | Reason |
|----------|--------|
| Atomic SQL update | Prevents overselling while keeping the implementation simple |
| Queue-based processing | Reduces request latency and database pressure during traffic spikes |
| Retry with dead-letter | Makes temporary failures recoverable and keeps failed messages visible |
| In-memory queue | Easy to implement for the assessment, but not crash-durable for production |
```

## Section 3 - External API integration

## Section 4 - Duplicate request

## Section 5 - Data synchronization
