.PHONY: run build test test-unit vet fmt check-fmt db-up db-down integration migrate-up webhook-register

run:
	go run ./cmd/bot
build:
	go build -o bin/bot ./cmd/bot
test:
	bash scripts/test.sh
	python3 -m unittest discover -s scripts -p 'test_*.py'
test-unit:
	go test -race ./...
	python3 -m unittest discover -s scripts -p 'test_*.py'
vet:
	go vet ./...
fmt:
	gofmt -w cmd internal migrations
check-fmt:
	@test -z "$$(gofmt -l cmd internal migrations)" || (echo "Run make fmt"; exit 1)
db-up:
	docker compose up -d --wait postgres
db-down:
	docker compose down
integration:
	bash scripts/test.sh ./internal/postgres ./internal/telegram
migrate-up:
	go run ./cmd/migrate

webhook-register:
	go run ./cmd/webhook
