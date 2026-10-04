.DEFAULT_GOAL := help
IMAGE := rail-canvas

.PHONY: help dev test vet fmt check build docker-build docker-run

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

dev: ## Run the API locally (reads .env if present)
	@set -a; [ -f .env ] && . ./.env; set +a; cd api && go run ./cmd/api

test: ## Run Go tests
	cd api && go test ./...

vet: ## go vet
	cd api && go vet ./...

fmt: ## gofmt every Go file
	cd api && gofmt -w .

check: vet test ## Everything CI runs on the API
	@cd api && test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

build: ## Build every binary into api/bin/
	cd api && CGO_ENABLED=0 go build -o bin/ ./cmd/...

docker-build: ## Build the image Railway builds
	docker build -t $(IMAGE) .

docker-run: docker-build ## Run the image on :8080
	docker run --rm -p 8080:8080 $(IMAGE)
