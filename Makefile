SHELL := /bin/sh
-include .env

DATABASE_URL ?= postgres://tktsync:tktsync@localhost:$${POSTGRES_PORT:-5432}/tktsync?sslmode=disable
MIGRATE_IMAGE ?= migrate/migrate:v4.18.3
LOAD_ENV = set -a; [ ! -f "$(CURDIR)/.env" ] || . "$(CURDIR)/.env"; set +a;

.PHONY: setup dev dev-api dev-worker dev-admin dev-selector dev-scanner build test lint typecheck format-check db-up db-down db-migrate db-reset verify-schema verify-platform-foundation verify-event-configuration verify-inventory-allocation verify-api-contract verify-reservations verify-ticketing verify-admissions verify-async-delivery verify-selection verify-reporting verify-fresh-database certify-partner-integration verify-release

setup:
	pnpm install --frozen-lockfile
	cd backend && go mod download

dev:
	pnpm exec concurrently -k -n api,worker,admin,selector,scanner "$(MAKE) dev-api" "$(MAKE) dev-worker" "$(MAKE) dev-admin" "$(MAKE) dev-selector" "$(MAKE) dev-scanner"

dev-api:
	$(LOAD_ENV) cd backend && go run ./cmd/api

dev-worker:
	$(LOAD_ENV) cd backend && go run ./cmd/worker

dev-admin:
	pnpm --filter @tktsync/admin-web dev

dev-selector:
	pnpm --filter @tktsync/selector-web dev

dev-scanner:
	pnpm --filter @tktsync/scanner-web dev

build:
	cd backend && go build -o bin/api ./cmd/api && go build -o bin/worker ./cmd/worker
	pnpm build

test:
	cd backend && go test ./...
	pnpm test

lint:
	test -z "$$(cd backend && gofmt -l .)"
	cd backend && go vet ./...
	pnpm lint
	pnpm format:check

typecheck:
	pnpm typecheck

format-check:
	test -z "$$(cd backend && gofmt -l .)"
	pnpm format:check

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose down

db-migrate:
	docker run --rm --network host -v "$(CURDIR)/migrations:/migrations:ro" $(MIGRATE_IMAGE) -path=/migrations -database "$(DATABASE_URL)" up

db-reset:
	docker compose down -v
	docker compose up -d --wait postgres
	$(MAKE) db-migrate

verify-schema:
	./scripts/verify-schema.sh

verify-platform-foundation:
	./scripts/verify-platform-foundation.sh

verify-event-configuration:
	./scripts/verify-event-configuration.sh

verify-inventory-allocation:
	./scripts/verify-inventory-allocation.sh

verify-api-contract:
	./scripts/verify-api-contract.sh

verify-reservations:
	./scripts/verify-reservations.sh

verify-ticketing:
	./scripts/verify-ticketing.sh

verify-admissions:
	./scripts/verify-admissions.sh

verify-async-delivery:
	./scripts/verify-async-delivery.sh

verify-selection:
	./scripts/verify-selection.sh

verify-reporting:
	./scripts/verify-reporting.sh

verify-fresh-database:
	./scripts/verify-fresh-database.sh

certify-partner-integration:
	./scripts/certify-partner-integration.sh

verify-release:
	./scripts/verify-release.sh
