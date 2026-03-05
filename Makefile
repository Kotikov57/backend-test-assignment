SHELL := /bin/bash

APP_NAME := api
DB_CONTAINER := withdrawals-test-db
DB_PORT := 5433
DB_USER := postgres
DB_PASSWORD := postgres
DB_NAME := withdrawals_test
TEST_DATABASE_URL ?= postgres://$(DB_USER):$(DB_PASSWORD)@localhost:$(DB_PORT)/$(DB_NAME)?sslmode=disable

.PHONY: help build run fmt test cover test-db-up test-db-down test-cover

help:
	@echo "Available targets:"
	@echo "  make build         - Build binary to ./bin/api"
	@echo "  make run           - Run API locally (requires DATABASE_URL and AUTH_TOKEN)"
	@echo "  make fmt           - Format all Go files"
	@echo "  make test          - Run all tests"
	@echo "  make cover         - Run tests with coverage"
	@echo "  make test-db-up    - Start local PostgreSQL container for tests (port $(DB_PORT))"
	@echo "  make test-db-down  - Stop and remove test PostgreSQL container"
	@echo "  make test-cover    - Start test DB, run HTTP tests with coverage, stop DB"

build:
	@mkdir -p bin
	go build -o ./bin/$(APP_NAME) ./cmd/api

run:
	go run ./cmd/api

fmt:
	gofmt -w $$(find . -name '*.go')

test:
	go test ./...

cover:
	go test ./... -cover

test-db-up:
	@docker rm -f $(DB_CONTAINER) >/dev/null 2>&1 || true
	@docker run -d \
		--name $(DB_CONTAINER) \
		-e POSTGRES_USER=$(DB_USER) \
		-e POSTGRES_PASSWORD=$(DB_PASSWORD) \
		-e POSTGRES_DB=$(DB_NAME) \
		-p $(DB_PORT):5432 \
		postgres:14 >/dev/null
	@echo "waiting for postgres..."
	@for i in {1..30}; do \
		docker exec $(DB_CONTAINER) pg_isready -U $(DB_USER) -d $(DB_NAME) >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	@echo "postgres is ready on port $(DB_PORT)"

test-db-down:
	@docker rm -f $(DB_CONTAINER) >/dev/null 2>&1 || true

test-cover: test-db-up
	@set -e; \
	trap '$(MAKE) test-db-down' EXIT; \
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./internal/http -v -cover
