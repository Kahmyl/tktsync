# Production runtime model

TktSync has two Go process types. The API owns authenticated HTTP requests, authoritative PostgreSQL commands and reads, health endpoints, realtime invalidation streams, and optional telemetry export. The Worker materializes due Reservation state, dispatches committed outbox facts, and delivers webhooks. The Go processes do not create or migrate schema themselves. In Docker deployments such as Render Free, the API container entrypoint applies pending ordered migrations and performs the idempotent initial-admin bootstrap before it starts the Go API. Migration or bootstrap failure prevents API startup.

## Concurrency and scaling

The API uses the Go HTTP server's goroutine-per-request model behind a process-wide `HTTP_MAX_IN_FLIGHT` semaphore (default 200). Requests beyond that bound receive `503` and `Retry-After: 1`; health, readiness, and protected metrics remain available. The Worker has independently bounded Reservation, outbox, and webhook workloads (`WORKER_RESERVATION_CONCURRENCY=2`, `WORKER_OUTBOX_CONCURRENCY=2`, and `WORKER_WEBHOOK_CONCURRENCY=4` by default). The batch defaults are 100, 100, and 50. PostgreSQL row locks, `SKIP LOCKED`, idempotency records, and webhook lease fencing coordinate independent Worker replicas; there is no in-process leader.

The Go programs never create replicas. An external runtime scales API replicas using sustained request concurrency, p95/p99 latency, CPU/memory, rejection rate, and database-pool wait. It scales Worker replicas primarily using pending count, oldest-work age, and slot utilization from the structured `worker.backlog` signal. CPU alone is a weak Worker signal. Scale-in must deliver SIGTERM and honor the configured drain periods.

Each process defaults to a PostgreSQL pool of 20 maximum and 2 minimum connections. For a database connection budget `B`, reserved administrative/migration headroom `R`, API replica count `A`, and Worker replica count `W`, safe sizing must satisfy `A × API_DB_MAX + W × WORKER_DB_MAX <= B - R`. TktSync currently uses the same `DB_MAX_CONNECTIONS` value for both process types. Operators must reduce it or cap replicas before exceeding PostgreSQL capacity. Pool acquisition count, empty acquisitions, acquisition duration, and connection states are exposed on the protected metrics endpoint.

## Lifecycle and failure behavior

`/health` reports process liveness and deliberately does not ping PostgreSQL. `/ready` rejects traffic while draining and otherwise performs a two-second database ping; a database outage makes the API not ready without declaring the process dead. Startup fails if configuration is invalid or the initial database connection cannot be established.

SIGINT or SIGTERM makes the API mark readiness false and invoke `http.Server.Shutdown`. In-flight ordinary requests and realtime streams may finish within `SHUTDOWN_TIMEOUT` (10 seconds by default); after the deadline the server closes remaining connections. Telemetry is flushed after HTTP draining, and the database pool closes last. The Worker immediately stops scheduling/claiming new batches, lets in-flight batches finish for `WORKER_SHUTDOWN_TIMEOUT` (10 seconds), then cancels their contexts and exits deterministically.

HTTP defaults are a five-second header timeout, 60-second idle timeout, 15-second ordinary-request context deadline, 60-second report/audit/export deadline, 1 MiB body/header limits, and 200 in-flight ordinary requests. SSE is excluded from the ordinary request deadline and uses heartbeat/cancellation semantics. PostgreSQL defaults are a five-second connect timeout, 65-second statement timeout, three-second lock timeout, 30-minute connection lifetime, and five-minute idle lifetime. Startup requires both HTTP request deadlines to remain strictly inside the PostgreSQL statement budget, so the API cancels work coherently before the database limit. Transaction retries are limited to three attempts, apply only to definite serialization/deadlock failures, and use bounded exponential full jitter. Unknown commit outcomes are never retried internally.

Webhook delivery uses one reusable HTTP client and transport per worker, SSRF-safe DNS/dialing, bounded dial/TLS/header/total deadlines, connection pooling, redirects disabled, a bounded response read, and mandatory body closure. Outbox and webhook failures use one-second-to-five-minute bounded exponential retry delays with jitter. Delivery leases and fenced updates protect multiple replicas and stale attempts.

## Observability

Logs are structured and include service, environment, bounded operation names, durations, statuses, and request/correlation IDs. They must never include credentials, capabilities, raw QR data, webhook secrets, tokens, or arbitrary request bodies. HTTP and Worker panic boundaries record the failure and contain the affected request/job; client responses never receive stacks or panic values.

Set `OTEL_ENABLED=true`, `OTEL_EXPORTER_OTLP_ENDPOINT`, and `OTEL_TRACE_SAMPLE_RATIO` to emit vendor-neutral OTLP traces. W3C trace context is accepted and propagated by the HTTP instrumentation. The exporter flushes during shutdown. Disabled telemetry is a no-op and does not change correctness.

Set `VITE_BROWSER_TELEMETRY_ENDPOINT` at browser build time to export an allowlisted API operation, status, duration, application, and error-class envelope. The transport uses `sendBeacon` with a keepalive fetch fallback, omits credentials/referrer data, and drops failures. It never exports headers, URLs, request bodies, capabilities, Reservation tokens, QR material, or credentials. An empty or unsafe endpoint disables export.

Set `METRICS_ENABLED=true` and a strong `METRICS_BEARER_TOKEN` to expose `/metrics`; startup rejects an enabled but unprotected endpoint. The Prometheus text endpoint reports process request volume/errors/in-flight/rejections/panics/average duration, Go goroutines/heap/GC, and pgx pool capacity/waits. Authorized event operational metrics scope request observations, Reservation/reconciliation work, outbox lag, webhook failures, and Admission outcomes to the selected Event. The explicitly named `process_waiting_database_locks` field remains process/database-wide because PostgreSQL lock waits cannot be truthfully assigned to one Event. Worker structured `worker.backlog` records pending counts, oldest ages, and active/capacity slots every 30 seconds. None of these signals controls business correctness.

Realtime is advisory invalidation over committed outbox facts. On connect/reconnect the server emits `resync`; clients must refetch authoritative state. A realtime outage never changes inventory, Sales, Ticket, or Admission truth. Scanner decisions remain online-authoritative and fail closed.

## Supported tuning knobs

- HTTP: `SHUTDOWN_TIMEOUT`, `HTTP_READ_HEADER_TIMEOUT`, `HTTP_IDLE_TIMEOUT`, `HTTP_REQUEST_TIMEOUT`, `HTTP_LONG_REQUEST_TIMEOUT`, `HTTP_MAX_BODY_BYTES`, `HTTP_MAX_HEADER_BYTES`, `HTTP_MAX_IN_FLIGHT`.
- Database: `DB_MAX_CONNECTIONS`, `DB_MIN_CONNECTIONS`, `DB_MAX_CONNECTION_LIFETIME`, `DB_MAX_CONNECTION_IDLE_TIME`, `DB_CONNECT_TIMEOUT`, `DB_STATEMENT_TIMEOUT`, `DB_LOCK_TIMEOUT`, `DB_TX_MAX_ATTEMPTS`, `DB_TX_RETRY_BASE_DELAY`.
- Worker: `WORKER_RESERVATION_CONCURRENCY`, `WORKER_OUTBOX_CONCURRENCY`, `WORKER_WEBHOOK_CONCURRENCY`, `WORKER_POLL_INTERVAL`, `WORKER_SHUTDOWN_TIMEOUT`, the three `WORKER_*_BATCH_SIZE` values, and `WORKER_WEBHOOK_TIMEOUT`.
- Telemetry: `OTEL_ENABLED`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACE_SAMPLE_RATIO`, `METRICS_ENABLED`, `METRICS_BEARER_TOKEN`, and browser-build `VITE_BROWSER_TELEMETRY_ENDPOINT`.

`APP_ENV=production` rejects missing human-auth JWKS/issuer/audience, capability and QR keyrings, Partner credential replay protection, insecure selector/browser origins, and incomplete enabled-webhook encryption. Development and test retain local defaults.

## Local runtime baseline

Measured on 2026-08-23 on an Apple M4, macOS arm64, Go 1.26, using an in-process no-op API handler over a real loopback HTTP connection. The dataset and PostgreSQL were intentionally absent: this measures HTTP runtime/middleware saturation, not authoritative domain throughput and is not a production capacity promise.

| Concurrency | Throughput req/s | p50 | p95 | p99 | Errors |
|---:|---:|---:|---:|---:|---:|
| 1 | 19,500 | 0.039 ms | 0.083 ms | 0.135 ms | 0 |
| 10 | 65,011 | 0.123 ms | 0.308 ms | 0.510 ms | 0 |
| 50 | 48,795 | 0.754 ms | 2.178 ms | 3.322 ms | 0 |
| 100 | 49,217 | 1.332 ms | 4.423 ms | 6.044 ms | 0 |
| 200 | 45,046 | 3.436 ms | 7.922 ms | 10.092 ms | 0 |

Peak loopback throughput occurred around concurrency 10; tail latency then rose progressively rather than collapsing. The middleware-only benchmark averaged 2.14–2.22 microseconds/op and 7,237–7,238 bytes/op. Run `make benchmark-runtime` to reproduce the non-gating baseline. Availability, hold, confirmation, Admission, and report capacity must additionally be measured against a production-shaped seeded PostgreSQL staging environment; their limiting factors are transactional locks, dataset/query plans, and pool capacity, so this loopback result must not be used as their proxy.
