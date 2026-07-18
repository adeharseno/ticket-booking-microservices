.PHONY: run run-worker test migrate-up infra-up infra-down

run:
	go run ./cmd

run-worker:
	RUN_MODE=worker go run ./cmd

run-mock-accounting:
	go run ./cmd/mockaccounting

test:
	go test ./... -race -v

migrate-up:
	@for f in migrations/*.sql; do \
		echo "Applying $$f"; \
		docker compose -f deployment/docker-compose.yml exec -T postgres \
			psql -U ticketing -d ticketing < $$f; \
	done

infra-up:
	docker compose -f deployment/docker-compose.yml up -d

infra-down:
	docker compose -f deployment/docker-compose.yml down
