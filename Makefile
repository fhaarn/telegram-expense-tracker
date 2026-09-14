.PHONY: run build test vet fmt check-fmt db-up db-down integration migrate-up

run:
	go run ./cmd/bot
build:
	go build -o bin/bot ./cmd/bot
test:
	go test -race ./...
vet:
	go vet ./...
fmt:
	gofmt -w cmd internal
check-fmt:
	@test -z "$$(gofmt -l cmd internal)" || (echo "Run make fmt"; exit 1)
db-up:
	docker compose up -d --wait postgres
db-down:
	docker compose down
integration:
	go test -race -tags=integration ./internal/postgres
migrate-up:
	@echo "Schema migrations are not implemented yet; see migrations/README.md."
	@exit 1
