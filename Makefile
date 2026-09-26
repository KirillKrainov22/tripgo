.PHONY: run build generate test migrate migrate-down migrate-status docker-build docker-run

DOCKER_IMAGE ?= tripgo:lab1
DOCKER_PORT ?= 8080

run:
	@test -f .env || { echo 'Run tripgoctl environment start first: .env is missing' >&2; exit 1; }
	@set -a; . ./.env.example; . ./.env; set +a; go run ./cmd/trip-service

build:
	go build -o bin/trip-service ./cmd/trip-service

docker-build:
	docker build -f deploy/Dockerfile -t $(DOCKER_IMAGE) .

docker-run:
	@test -f .env || { echo 'Run tripgoctl environment start first: .env is missing' >&2; exit 1; }
	@set -a; . ./.env.example; . ./.env; set +a; \
	DATABASE_URL=$$(printf '%s' "$$DATABASE_URL" | sed 's/@localhost:/@host.docker.internal:/; s/@127\.0\.0\.1:/@host.docker.internal:/'); \
	docker run --rm --read-only -p 127.0.0.1:$(DOCKER_PORT):8080 \
		--env-file .env.example --env-file .env \
		-e HTTP_ADDR=:8080 -e DATABASE_URL $(DOCKER_IMAGE)

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
