.PHONY: up down logs ps reset test test-backend test-frontend lint-frontend build seed

up:
	docker compose up -d --build

down:
	docker compose down

reset:
	docker compose down -v

logs:
	docker compose logs -f api

ps:
	docker compose ps

build:
	cd backend && go build ./...
	cd frontend && npm run build

test: test-backend test-frontend

# Repository tests need a database; without TEST_DATABASE_URL they skip.
test-backend:
	cd backend && go vet ./... && go test ./...

test-frontend:
	cd frontend && npm run build

lint-frontend:
	cd frontend && npm run lint

seed:
	cd backend && go run ./cmd/seed
