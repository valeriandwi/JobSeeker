SHELL := /bin/bash
PGBIN ?= /opt/homebrew/opt/postgresql@15/bin

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---------- dev database (isolated local cluster on port 5433) ----------
.PHONY: dev-db-init dev-db-up dev-db-down dev-db-status dev-db-psql
dev-db-init: ## Initialize the isolated dev Postgres cluster (.devdb) and create dev/test databases
	@test -d .devdb || $(PGBIN)/initdb -D .devdb -U jobseeker --auth=trust --encoding=UTF8 --no-locale
	@$(PGBIN)/pg_ctl -D .devdb -o "-p 5433 -k /tmp -c listen_addresses=127.0.0.1" -l .devdb/pg.log start || true
	@sleep 1
	@$(PGBIN)/createdb -h 127.0.0.1 -p 5433 -U jobseeker jobseeker_dev 2>/dev/null || true
	@$(PGBIN)/createdb -h 127.0.0.1 -p 5433 -U jobseeker jobseeker_test 2>/dev/null || true
	@$(PGBIN)/pg_isready -h 127.0.0.1 -p 5433

dev-db-up: ## Start the dev Postgres cluster
	@$(PGBIN)/pg_ctl -D .devdb -o "-p 5433 -k /tmp -c listen_addresses=127.0.0.1" -l .devdb/pg.log start

dev-db-down: ## Stop the dev Postgres cluster
	@$(PGBIN)/pg_ctl -D .devdb stop

dev-db-status: ## Show dev Postgres status
	@$(PGBIN)/pg_isready -h 127.0.0.1 -p 5433

dev-db-psql: ## Open psql against the dev database
	@$(PGBIN)/psql -h 127.0.0.1 -p 5433 -U jobseeker -d jobseeker_dev

# ---------- backend ----------
.PHONY: build test test-race test-integration lint fmt vet tidy migrate-up migrate-down migrate-status run-api run-scheduler smoke
build: ## Build all binaries into bin/
	go build -o bin/ ./cmd/...

test: ## Run unit tests
	go test ./...

test-race: ## Run unit tests with the race detector
	go test -race ./...

test-integration: ## Run unit + integration tests (requires dev-db-running; uses jobseeker_test)
	TEST_DATABASE_URL="postgres://jobseeker@127.0.0.1:5433/jobseeker_test?sslmode=disable" go test -race ./...

lint: fmt vet ## Format check + go vet
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else echo "golangci-lint not installed (skipped)"; fi

fmt: ## gofmt the tree
	@gofmt -l -w $$(find . -name '*.go' -not -path './web/*')

vet: ## go vet
	go vet ./...

tidy: ## Tidy and verify modules
	go mod tidy && go mod verify

migrate-up: ## Apply all migrations
	go run ./cmd/migrate up

migrate-down: ## Roll back the most recent migration
	go run ./cmd/migrate down

migrate-status: ## Show migration status
	go run ./cmd/migrate status

run-api: ## Run the API server
	go run ./cmd/api

run-scheduler: ## Run the scheduler process
	go run ./cmd/scheduler

smoke: ## End-to-end smoke test against a running API (see scripts/smoke.sh)
	./scripts/smoke.sh

# ---------- frontend ----------
.PHONY: web-install web-dev web-build web-test
web-install: ## Install frontend dependencies
	cd web && pnpm install

web-dev: ## Run the Vite dev server
	cd web && pnpm dev

web-build: ## Type-check and build the frontend
	cd web && pnpm build

web-test: ## Run frontend tests
	cd web && pnpm test

# ---------- containers ----------
.PHONY: compose-up compose-down
compose-up: ## Start the full stack via Docker Compose
	docker compose -f deploy/docker-compose.yml --env-file .env up -d --build

compose-down: ## Stop the Docker Compose stack
	docker compose -f deploy/docker-compose.yml down
