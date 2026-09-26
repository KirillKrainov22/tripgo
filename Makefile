.PHONY: run build generate test migrate migrate-down migrate-status

run:
	@test -f .env || { echo 'Run tripgoctl environment start first: .env is missing' >&2; exit 1; }
	@set -a; . ./.env.example; . ./.env; set +a; go run ./cmd/trip-service

build:
	go build -o bin/trip-service ./cmd/trip-service

generate:
	go tool oapi-codegen -config oapi-codegen.yaml contracts/openapi/trip-service.openapi.yaml

test:
	go test -race ./...

migrate:
	@test -f .env || { echo 'Run tripgoctl environment start first: .env is missing' >&2; exit 1; }
	@set -a; . ./.env; set +a; GOOSE_DRIVER=postgres GOOSE_DBSTRING="$$DATABASE_URL" go tool goose -dir migrations up

migrate-down:
	@test -f .env || { echo 'Run tripgoctl environment start first: .env is missing' >&2; exit 1; }
	@set -a; . ./.env; set +a; GOOSE_DRIVER=postgres GOOSE_DBSTRING="$$DATABASE_URL" go tool goose -dir migrations down

migrate-status:
	@test -f .env || { echo 'Run tripgoctl environment start first: .env is missing' >&2; exit 1; }
	@set -a; . ./.env; set +a; GOOSE_DRIVER=postgres GOOSE_DBSTRING="$$DATABASE_URL" go tool goose -dir migrations status
