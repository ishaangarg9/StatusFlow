.PHONY: db.up db.down migrate.up migrate.down sqlc api worker test test.proof lint fmt docker.api docker.worker

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

fmt:
	gofmt -w $$(git ls-files '*.go')

lint:
	@unformatted="$$(gofmt -l $$(git ls-files '*.go'))"; \
	if [ -n "$$unformatted" ]; then echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	staticcheck ./...

# Image builds (VERSION stamps the binary via -ldflags).
docker.api:
	docker build -f Dockerfile.api -t statusflow-api:$${VERSION:-dev} --build-arg VERSION=$${VERSION:-dev} .

docker.worker:
	docker build -f Dockerfile.worker -t statusflow-worker:$${VERSION:-dev} --build-arg VERSION=$${VERSION:-dev} .
