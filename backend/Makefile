# NOVA API — Makefile.
# All targets are phony (no file artifacts to compare timestamps against).

.PHONY: run build migrate-up migrate-down migrate-status tidy test vet fmt clean help

# Default: show help.
help:
	@echo "NOVA API — available targets:"
	@echo "  make run           — run the API locally (go run ./cmd/server)"
	@echo "  make build         — build the binary to ./bin/nova-api"
	@echo "  make migrate-up    — apply all pending goose migrations"
	@echo "  make migrate-down  — roll back the last migration"
	@echo "  make migrate-status— show migration status"
	@echo "  make tidy          — go mod tidy"
	@echo "  make vet           — go vet ./..."
	@echo "  make fmt           — go fmt ./..."
	@echo "  make test          — go test ./..."
	@echo "  make clean         — remove ./bin"

run:
	@go run ./cmd/server

build:
	@mkdir -p bin
	@go build -o bin/nova-api ./cmd/server

# The migrate-* targets use the goose CLI. Install it once with:
#   go install github.com/pressly/goose/v3/cmd/goose@latest
migrate-up:
	@goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	@goose -dir migrations postgres "$(DATABASE_URL)" down

migrate-status:
	@goose -dir migrations postgres "$(DATABASE_URL)" status

tidy:
	@go mod tidy

vet:
	@go vet ./...

fmt:
	@go fmt ./...

test:
	@go test ./...

clean:
	@rm -rf bin
