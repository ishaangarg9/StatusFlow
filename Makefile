.PHONY: db.up db.down migrate.up migrate.down sqlc api worker test test.proof lint fmt docker.api docker.worker docker.web docker.migrate docker.all kind.validate kind.down

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

# Web (Next.js BFF) builds from the web/ context; migrate bundles golang-migrate
# + the SQL migration set for the privileged k8s Job.
docker.web:
	docker build -f Dockerfile.web -t statusflow-web:$${VERSION:-dev} web

docker.migrate:
	docker build -f Dockerfile.migrate -t statusflow-migrate:$${VERSION:-dev} .

docker.all: docker.api docker.worker docker.web docker.migrate

# P11: full local cluster parity (build + kind + helm install + smoke test).
kind.validate:
	./deploy/validate.sh

kind.down:
	kind delete cluster --name statusflow
