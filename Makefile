SHELL := /bin/sh
-include .env

DATABASE_URL ?= postgres://tktsync:tktsync@localhost:$${POSTGRES_PORT:-5432}/tktsync?sslmode=disable
MIGRATE_IMAGE ?= migrate/migrate:v4.18.3
LOAD_ENV = set -a; [ ! -f "$(CURDIR)/.env" ] || . "$(CURDIR)/.env"; set +a;

.PHONY: setup dev dev-api dev-worker dev-admin dev-selector dev-scanner build test lint typecheck format-check db-up db-down db-migrate db-reset verify-m2 verify-m3 verify-m4 verify-m4c

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

verify-m2:
	./scripts/verify-m2.sh

verify-m3:
	./scripts/verify-m3.sh

verify-m4:
	./scripts/verify-m4.sh

verify-m4c:
	./scripts/verify-m4c.sh

.PHONY: verify-m5
verify-m5:
	./scripts/verify-m5.sh
