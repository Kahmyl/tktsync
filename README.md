# TktSync

TktSync is a pnpm monorepo with three React/Vite applications and one Go backend that builds separate API and worker executables. The bootstrap layer provides infrastructure; ordered migrations define the authoritative business schema.

## Prerequisites

- Node.js 22+
- pnpm 10+
- Go 1.25+
- Docker with Compose

## Setup

```sh
cp .env.example .env
pnpm install
cd backend && go mod download && cd ..
make db-up
make db-migrate
```

The example configuration contains local-only placeholders. Never commit `.env`, signing keys, database passwords, service-role credentials, or other secrets. Application logs must use identifiers and redacted error context rather than configuration values.

## Run

Start every process with `make dev`, or run them independently:

```sh
make dev-api       # http://127.0.0.1:8080
make dev-worker
make dev-admin     # Vite selects an available local port
make dev-selector
make dev-scanner
```

The API exposes `GET /health` for liveness and `GET /ready` for database-backed readiness. The worker verifies database connectivity before processing configured background work.

## Development commands

```sh
make build
make test
make lint
make typecheck
make format-check

make db-up
make db-down
make db-migrate
make db-reset       # removes the local database volume, restarts, and migrates
```

If port 5432 is already occupied, set `POSTGRES_PORT` and update the port in `DATABASE_URL` to the same value before starting the database.

`make setup` installs pinned pnpm dependencies and downloads Go modules. Migration files live in `migrations/`; production code never creates schema dynamically. The bootstrap migration deliberately creates no TktSync tables.

## Repository layout

- `apps/` — Admin, selector, and scanner React applications
- `backend/` — authoritative Go codebase and API/worker commands
- `packages/` — shared UI primitives and the generated API client
- `migrations/` — ordered PostgreSQL migrations
- `tests/` — integration, concurrency, end-to-end, and fixture conventions
- `docs/` — architecture, API, operations, and implementation history

## Documentation

Start with the [documentation index](docs/README.md) for the governing policy and architecture, API integration contract, release operations, and historical implementation plan.
