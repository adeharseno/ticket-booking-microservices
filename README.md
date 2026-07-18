# Ticket Booking System

## Why this structure

One binary (`cmd/main.go`), API or worker mode is determined via the env var
`RUN_MODE`, to simplify demo and development without having to run
two separate processes. Each scenario is a single file (`internal/<module>/<module>.go`)
containing a model + repository + service + handler with a clear comment section -
the goal is to allow a scenario to be read from top to bottom without jumping
between files. Shared infrastructure (db, redis, queue, retry, router) is separated in
`internal/shared` because it is used across modules.

Queue Section 2 uses an in-memory channel first (not RabbitMQ/Kafka),
wrapped in the `Queue` interface for easy swapping to a real message broker
without changing the module code - see the trade-off notes in `docs/ARCHITECTURE.md`.

## Project Structure

- `cmd/main.go` - single entrypoint (API server, or worker via `RUN_MODE=worker`)
- `internal/<module>/<module>.go` - one file per scenario
- `internal/shared/` - shared infrastructure (db, redis, queue, retry, router)
- `migrations/` - SQL migrations for all tables

## How to Run

```bash
make infra-up # start postgres + redis via docker-compose
make migrate-up # run migrations (see migrations/)
make run # run API server
make run-worker # run worker (separate terminal)
```

## How to Test

```bash
make test
```

## Design Explanation

See `docs/ARCHITECTURE.md` for analysis, assumptions, and trade-offs for each scenario.

## Scenario Mapping

| Sections | Files | Main concepts |
|---|---|---|
| 1 - Race conditions | `internal/ticket/ticket.go` | Atomic conditional update |
| 2 - High traffic | `internal/transaction/transaction.go` | Queue-based async processing |
| 3 - External APIs | `internal/accounting/accounting.go` | Transactional outbox + retry + circuit breaker |
| 4 - Duplicate request | `internal/webhook/webhook.go` | Idempotency key + unique constraint |
| 5 - Data sync | `internal/sync/sync.go` | Version-based ordering |