.PHONY: db.up db.down migrate.up migrate.down sqlc api worker test lint

db.up:
	docker compose up -d db

db.down:
	docker compose down

migrate.up:
	migrate -path internal/db/migrations -database "$$MIGRATIONS_DATABASE_URL" up

migrate.down:
	migrate -path internal/db/migrations -database "$$MIGRATIONS_DATABASE_URL" down 1

sqlc:
	sqlc generate

api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

test:
	go test ./...

test.proof:
	go test ./test/isolation/... ./test/authz/...

lint:
	go vet ./...
	staticcheck ./... || true
