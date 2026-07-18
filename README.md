# Ticket Booking System

## Project Structure

```
cmd/                Application entrypoint
docs/               Architecture notes
internal/           Application modules
internal/shared/    Shared components
migrations/         Database migrations
deployment/         Docker Compose
```

## Running the Application

Start the required services.

```bash
make infra-up
```

Run database migrations.

```bash
make migrate-up
```

Start the API.

```bash
make run
```

Start the worker (optional).

```bash
make run-worker
```

## Running Tests

```bash
make test
```

## Documentation

Additional design notes and architecture decisions are available in:

```
docs/ARCHITECTURE.md
```