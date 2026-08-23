# Release Traceability

This file maps the final release gate to executable evidence. The governing specifications remain the authority.

## Concurrency and ordering

| Requirement | Executable evidence |
| --- | --- |
| Hold vs hold, 100 contenders | `TestReservationMixedAtomicityAndReservedConcurrency` |
| Hold vs Block | `TestHoldVersusBlockHasOneAuthoritativeWinner` |
| GA capacity 10, 100 contenders | `TestReservationGAContention` |
| Confirmation vs expiry/database time | `TestConfirmationExpiryGuardUsesPostLockDatabaseTime` |
| Confirmation vs cancellation, both orders and concurrent | `TestConfirmationVersusCancellationOrdering` |
| Scan vs scan, 100 contenders | `TestAdmissionConcurrentDistinctScansHaveExactlyOneWinner` |
| QR reissue vs scan | `TestVoidAndReissueVersusScanOrdering/reissue-versus-old-credential-scan` |
| Ticket void vs scan | `TestVoidAndReissueVersusScanOrdering/void-versus-scan` |
| Partner disable vs acquisition | `TestPartnerDisableVersusAcquisitionUsesPartnerGate` |

## Idempotency and failure

| Requirement | Executable evidence |
| --- | --- |
| Same key/same intent replay and lost response | `TestPartnerReservationHTTP`, `TestAdminHTTPIdempotencyAndCredentialReplay`, `TestAdmissionHTTPIdempotencyReplayAndConflict` |
| Same key/different intent conflict | The same HTTP tests above |
| Concurrent first request | `TestConcurrentClaimExecutesOnce` |
| Failure before commit | `TestClaimRollbackDoesNotPersistInProgress`, `TestAuditAndOutboxRollbackTogether` |
| PostgreSQL deadlock retry | `TestRunnerRetriesAnActualPostgreSQLDeadlock` |
| PostgreSQL unavailable | `TestDatabaseUnavailableFailsAuthoritativeMutationClosed` |
| Realtime unavailable | `TestDisabledRealtimeFailsClosed` |
| Worker/dispatcher/webhook outage, retry, and lease fencing | Reservation worker and asynchronous-delivery tests |
| Commit-backed async recovery | Committed/rolled-back outbox and webhook tests |

## Security and isolation

- Partner authentication, disable, Event access, cross-Partner Reservation ownership, and report isolation: Configuration/Allocation/Reservation/Reporting integration suites.
- Selection capability scope, expiry, cleanup, exact registered HTTPS return URL, and stable retry intent: Selection integration and browser tests.
- Scanner role scope, credential forgery/tamper, wrong Event, revocation, supersession, Ticket void, cancellation, and manual-override hard guards: Admission integration suite.
- Webhook signature/retry/replay behavior, encrypted historical keys, SSRF denial, disable/unsubscribe, and lease fencing: asynchronous-delivery integration plus webhook unit tests.
- Exact CORS allowlist: `TestCORSUsesExactAllowlist`.
- Browser CSP and `no-referrer`: `scripts/check-release-contract.mjs`.
- Lifecycle role scope, required cancellation reason, audit, and idempotency: `TestAdminLifecycleAuthorizationReasonAuditAndIdempotency`.

## Partner and end-to-end journeys

`scripts/certify-partner-integration.sh` is the single deterministic Partner certification harness. It exercises real HTTP authentication, discovery/availability, mixed Reserved+GA holds, expiry/release, checkout, payment failure and uncertainty, confirmation, response replay, Tickets and QR recovery, issuance without Sale, selection handoff, webhook retry/duplicate safety, Partner disable, and Event cancellation.

The component suites additionally prove Reserved-only, GA quantity-to-independent-Ticket issuance, mixed atomicity, channel source restoration, non-public issuance without Sale, historical Sale preservation, cancellation cleanup, and denial of new Sale after cancellation.

## Load and operations

The Partner harness records elapsed measurements for the HTTP journey, 100-way Reserved/GA/admission contention, 50 simultaneous realtime connections, and async/webhook recovery. The governing specification defines correctness invariants but no numeric latency SLO, so the gate reports measurements without inventing a pass threshold.

`scripts/verify-fresh-database.sh` creates and destroys only a uniquely named disposable database, applies the complete immutable migration chain, asserts migration/table state, and runs configuration, reporting, lifecycle, and concurrency semantic tests. The [MVP Release Runbook](release-runbook.md) covers deployment, rollback, backups, recovery, key rotation, and alert response. CI runs `make verify-release` as the final release job.
