#!/bin/sh
set -eu

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export DATABASE_URL=${DATABASE_URL:-"postgres://tktsync:tktsync@localhost:${POSTGRES_PORT:-55432}/tktsync?sslmode=disable"}
export GOCACHE=${GOCACHE:-/tmp/tktsync-go-build}

echo "Running the deterministic Partner implementation certification harness..."
cd "$REPO_DIR/backend"

START=$(date +%s)
go test -tags=integration ./internal/partnerapi -run '^TestPartnerReservationHTTP$' -count=1 -v
END=$(date +%s)
echo "RELEASE_LOAD_MEASUREMENT partner_http_journey elapsed_seconds=$((END - START))"

go test -tags=integration ./internal/allocation -run '^TestTicketingNonPublicIssuance$' -count=1 -v
go test -tags=integration ./internal/reservation \
	-run '^(TestReservationLifecycleAndSourceRestoration|TestReservationCheckoutRetryReconciliationAndTokenRecovery|TestReservationDefinitiveFailureDuringReconciliation|TestReservationModificationAndWorkerExpiry|TestTicketingDelayedConfirmationDuringReconciliation|TestSelectionCapabilityReservationHandoff|TestReportingPreservesCommercialIssuanceAndCapacitySemantics)$' \
	-count=1 -v

START=$(date +%s)
go test -tags=integration ./internal/reservation \
  -run '^(TestDatabaseUnavailableFailsAuthoritativeMutationClosed|TestHoldVersusBlockHasOneAuthoritativeWinner|TestPartnerDisableVersusAcquisitionUsesPartnerGate|TestConfirmationVersusCancellationOrdering|TestConfirmationExpiryGuardUsesPostLockDatabaseTime|TestCancellationCleanupIsBoundedAndStateAware|TestVoidAndReissueVersusScanOrdering|TestRealtimeConcurrentConnectionsRemainAdvisory|TestReservationMixedAtomicityAndReservedConcurrency|TestReservationGAContention|TestTicketingConcurrentConfirmationHasOneSale|TestAdmissionConcurrentDistinctScansHaveExactlyOneWinner)$' \
  -count=1 -v
END=$(date +%s)
echo "RELEASE_LOAD_MEASUREMENT authoritative_contention reserved_requests=100 ga_requests=100 scan_requests=100 elapsed_seconds=$((END - START))"

START=$(date +%s)
go test -tags=integration ./internal/reservation \
  -run '^(TestAsyncDeliverySSEEmitsOnlyProcessedCommittedFacts|TestAsyncDeliveryOutboxWebhookRetrySigningAndConcurrentDispatch|TestAsyncDeliveryExpiredLeaseStaleAttemptCannotOverwriteReclaim)$' \
  -count=1 -v
END=$(date +%s)
echo "RELEASE_LOAD_MEASUREMENT async_realtime_webhook elapsed_seconds=$((END - START))"

echo "Partner certification COMPLETE: auth, discovery, availability, hold, checkout, failure, uncertainty, confirmation, lost-response replay, release, ticket credentials, webhook retry/duplicate safety, disable, issuance, and cancellation."
