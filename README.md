# TktSync

TktSync is a pnpm monorepo with three React/Vite applications and a Go backend that builds separate API and worker executables. Ordered PostgreSQL migrations define the authoritative business schema.

## Local product stack

Docker Compose runs PostgreSQL, automatic migrations and application seeds, the API, worker, Admin, Selector, and Scanner together. Docker with Compose is the only prerequisite for this path.

```sh
cp .env.example .env
# Add Supabase public/JWT settings if Admin or Scanner login is needed.
make local-up
```

With the default ports, open:

- API: http://localhost:58480 (`/health` and database-backed `/ready`)
- Admin: http://localhost:54470
- Selector: http://localhost:54471
- Scanner: http://localhost:54472
- PostgreSQL: `localhost:55439`

Migrations run after PostgreSQL becomes healthy, then the idempotent seed runs; the API and worker start only after both succeed. A normal cached startup should complete within 60 seconds, while a first image pull/build can take longer.

```sh
make local-ps
make local-logs
make local-down
make local-seed   # rerun application defaults after changing operator settings
make local-reset  # WARNING: destroys the local Compose database volume
```

Override any published port in `.env` or for one command without editing Compose, for example `API_HOST_PORT=58481 make local-up`. The frontend API address is compiled from `API_HOST_PORT` during the image build.

Admin and Scanner operator login use an external Supabase Auth/OIDC project. Set matching `SUPABASE_JWT_ISSUER`, `SUPABASE_JWKS_URL`, and browser-safe `VITE_SUPABASE_URL`/`VITE_SUPABASE_ANON_KEY` values. An anon key is public client configuration; never put a service-role key, JWT signing secret, or other private credential in a `VITE_*` variable.

To authorize one local platform administrator, create a normal test user under Supabase Authentication, copy its User ID, and add it to `.env` as `LOCAL_OPERATOR_AUTH_SUBJECT`. The User ID is the JWT `sub`; it is not the user's email or access token. `LOCAL_OPERATOR_DISPLAY_NAME` controls the TktSync display name, and `LOCAL_OPERATOR_PLATFORM_ADMIN=true` adds the platform role. Run `make local-seed` or `make local-up`, then sign in through Admin or Scanner using that Supabase user's email and password. Supabase retains the password; the seed never receives or stores it. With no subject configured, seeding skips cleanly and the stack still starts.

Normal seeds contain only application defaults: the optional `app_users` identity mapping and platform role. They do not create Supabase users or any venue, event, partner, inventory, reservation, sale, ticket, admission, webhook, audit, or demo data.

See the [local smoke checklist](docs/operations/local-smoke-checklist.md) for the short manual product check.

## Source development

For host-based development, install Node.js 22+, pnpm 10+, and Go 1.25+:

```sh
make setup
make db-up
make db-migrate
make dev
```

Useful repository gates are `make build`, `make test`, `make lint`, `make typecheck`, and `make format-check`. Migration files live in `migrations/`; production code never creates schema dynamically.

Never commit `.env`, signing keys, database passwords, service-role credentials, or other secrets.

## Repository layout

- `apps/` — Admin, Selector, and Scanner React applications
- `backend/` — authoritative Go API, worker, and domain code
- `packages/` — shared UI primitives and generated API client
- `migrations/` — ordered PostgreSQL migrations
- `tests/` — integration, concurrency, end-to-end, and fixture conventions
- `docs/` — architecture, API, operations, and implementation history

Start with the [documentation index](docs/README.md) for governing policy and architecture.
