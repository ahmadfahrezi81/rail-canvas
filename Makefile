.DEFAULT_GOAL := help
IMAGE := rail-canvas
# Same toolchain for the module and the pinned tools (else go run @version picks its own).
export GOTOOLCHAIN := go1.27.1

GOOSE := go run github.com/pressly/goose/v3/cmd/goose@v3.28.0
SQLC  := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
OAPI  := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
MIGRATIONS := db/migrations

# Loads .env into the recipe's shell. Values are never echoed.
ENV := set -a; [ -f .env ] && . ./.env; set +a;
# Test database: same server as MIGRATE_DATABASE_URL, database rail_canvas_test.
TEST_URL = $${MIGRATE_DATABASE_URL%/*}/rail_canvas_test

.PHONY: help dev test test-db vet fmt check web-dev web-build generate build docker-build docker-run \
	migrate-new migrate-up migrate-status db-app-password db-seed-dev db-test-setup

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

dev: ## Run the API locally (reads .env)
	@$(ENV) cd api && go run ./cmd/api

test: ## Go tests (database tests skip without TEST_DATABASE_URL)
	cd api && go test ./...

test-db: ## Go tests including the database tests, against rail_canvas_test
	@$(ENV) cd api && TEST_DATABASE_URL="$(TEST_URL)" go test -count=1 ./...

vet: ## go vet
	cd api && go vet ./...

fmt: ## gofmt every Go file
	cd api && gofmt -w .

check: vet test web-build ## gofmt, vet, tests, frontend build: what CI runs
	@cd api && test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

web-dev: ## Run the frontend on :5173 (reads web/.env.local)
	cd web && pnpm dev

web-build: ## Type-check and build the frontend
	cd web && pnpm -s build

generate: ## Regenerate apigen + web types (openapi.yaml) and store (sqlc)
	cd api && $(OAPI) -config oapi-codegen.yaml openapi.yaml
	cd api && $(SQLC) generate
	cd web && pnpm -s generate

build: ## Build every binary into api/bin/
	cd api && CGO_ENABLED=0 go build -o bin/ ./cmd/...

docker-build: ## Build the image Railway builds
	docker build -t $(IMAGE) .

docker-run: docker-build ## Run the image on :8080
	docker run --rm -p 8080:8080 $(IMAGE)

migrate-new: ## New migration: make migrate-new name=create_things
	@test -n "$(name)" || (echo "usage: make migrate-new name=..."; exit 1)
	cd api && $(GOOSE) -dir $(MIGRATIONS) create $(name) sql

migrate-up: ## Apply migrations to MIGRATE_DATABASE_URL
	@$(ENV) cd api && $(GOOSE) -dir $(MIGRATIONS) postgres "$$MIGRATE_DATABASE_URL" up

migrate-status: ## Show applied migrations on MIGRATE_DATABASE_URL
	@$(ENV) cd api && $(GOOSE) -dir $(MIGRATIONS) postgres "$$MIGRATE_DATABASE_URL" status

db-app-password: ## Let the app role log in with APP_DB_PASSWORD
	@$(ENV) test -n "$$APP_DB_PASSWORD" || (echo "APP_DB_PASSWORD is empty in .env"; exit 1); \
	echo "ALTER ROLE app WITH LOGIN PASSWORD :'pw';" | psql "$$MIGRATE_DATABASE_URL" -X -q -v ON_ERROR_STOP=1 -v pw="$$APP_DB_PASSWORD" && echo "app can log in"

db-seed-dev: ## Insert the dev space and print its id (for DEV_SPACE_ID)
	@$(ENV) psql "$$MIGRATE_DATABASE_URL" -X -q -A -t -v ON_ERROR_STOP=1 -f api/db/seed/dev_space.sql

db-test-setup: ## Create rail_canvas_test if missing and migrate it
	@$(ENV) psql "$$MIGRATE_DATABASE_URL" -X -q -A -t -c "SELECT 1 FROM pg_database WHERE datname = 'rail_canvas_test'" | grep -q 1 \
		|| psql "$$MIGRATE_DATABASE_URL" -X -q -c "CREATE DATABASE rail_canvas_test"
	@$(ENV) cd api && $(GOOSE) -dir $(MIGRATIONS) postgres "$(TEST_URL)" up
