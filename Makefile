.PHONY: run run-worker test migrate-up infra-up infra-down

run:
	go run ./cmd

run-worker:
	RUN_MODE=worker go run ./cmd

test:
	go test ./... -race -v

migrate-up:
	@echo "TODO: wire up a migration tool (e.g. golang-migrate) pointing at migrations/"

infra-up:
	docker compose -f deployment/docker-compose.yml up -d

infra-down:
	docker compose -f deployment/docker-compose.yml down
