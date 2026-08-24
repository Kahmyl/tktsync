//go:build integration

package reservation_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

type fixture struct {
	pool           *pgxpool.Pool
	runner         *database.Runner
	userID         uuid.UUID
	userSubject    string
	eventID        uuid.UUID
	partnerID      uuid.UUID
	otherPartnerID uuid.UUID
	seatIDs        map[string]uuid.UUID
	gaMainID       uuid.UUID
	gaSmallID      uuid.UUID
	reservation    *reservation.Service
	allocation     *allocsvc.Service
}

func TestReservationLifecycleAndSourceRestoration(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	allocationID, err :=
		f.allocation.CreateAllocation(
			ctx,
			f.userID,
			f.eventID,
			allocsvc.AllocationInput{
				Mode:                   "CHANNEL",
				PartnerID:              &f.partnerID,
				Purpose:                "CHANNEL",
				ReleaseDestinationKind: "SHARED",
				ReservedUnitIDs: []uuid.UUID{
					f.seatIDs["A3"],
				},
				GATargets: []allocsvc.GATarget{
					{
						PoolID:   f.gaMainID,
						Quantity: 5,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create active Allocation: %v",
			err,
		)
	}

	activeHold, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind:      reservation.InventoryReserved,
						InventoryID:        f.seatIDs["A3"],
						Quantity:           1,
						SourceKind:         reservation.SourceAllocation,
						SourceAllocationID: &allocationID,
					},
					{
						InventoryKind:      reservation.InventoryGA,
						InventoryID:        f.gaMainID,
						Quantity:           2,
						SourceKind:         reservation.SourceAllocation,
						SourceAllocationID: &allocationID,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"hold active Allocation: %v",
			err,
		)
	}

	if err := f.reservation.Release(
		ctx,
		f.partnerID,
		activeHold.Token,
	); err != nil {
		t.Fatalf(
			"release active Allocation hold: %v",
			err,
		)
	}

	assertClaimType(
		t,
		ctx,
		f.pool,
		f.seatIDs["A3"],
		"ALLOCATION",
	)

	var (
		activeAvailable int
		activeReserved  int
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				available_quantity,
				active_reserved_quantity
			FROM ga_allocation_buckets
			WHERE allocation_id = $1
			  AND ga_pool_id = $2
		`,
		allocationID,
		f.gaMainID,
	).Scan(
		&activeAvailable,
		&activeReserved,
	); err != nil {
		t.Fatalf(
			"read restored active GA Allocation: %v",
			err,
		)
	}

	if activeAvailable != 5 ||
		activeReserved != 0 {
		t.Fatalf(
			"active Allocation GA restoration available=%d reserved=%d",
			activeAvailable,
			activeReserved,
		)
	}

	releasedAllocationID, err :=
		f.allocation.CreateAllocation(
			ctx,
			f.userID,
			f.eventID,
			allocsvc.AllocationInput{
				Mode:                   "CHANNEL",
				PartnerID:              &f.partnerID,
				Purpose:                "CHANNEL",
				ReleaseDestinationKind: "SHARED",
				ReservedUnitIDs: []uuid.UUID{
					f.seatIDs["A4"],
				},
				GATargets: []allocsvc.GATarget{
					{
						PoolID:   f.gaMainID,
						Quantity: 10,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create releasable Allocation: %v",
			err,
		)
	}

	releasedSourceHold, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind:      reservation.InventoryReserved,
						InventoryID:        f.seatIDs["A4"],
						Quantity:           1,
						SourceKind:         reservation.SourceAllocation,
						SourceAllocationID: &releasedAllocationID,
					},
					{
						InventoryKind:      reservation.InventoryGA,
						InventoryID:        f.gaMainID,
						Quantity:           3,
						SourceKind:         reservation.SourceAllocation,
						SourceAllocationID: &releasedAllocationID,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"hold releasable Allocation: %v",
			err,
		)
	}

	if err :=
		f.allocation.ReleaseAllocation(
			ctx,
			f.userID,
			releasedAllocationID,
		); err != nil {
		t.Fatalf(
			"release Allocation during hold: %v",
			err,
		)
	}

	assertClaimType(
		t,
		ctx,
		f.pool,
		f.seatIDs["A4"],
		"RESERVATION",
	)

	if err := f.reservation.Release(
		ctx,
		f.partnerID,
		releasedSourceHold.Token,
	); err != nil {
		t.Fatalf(
			"release hold after Allocation release: %v",
			err,
		)
	}

	assertNoActiveClaim(
		t,
		ctx,
		f.pool,
		f.seatIDs["A4"],
	)

	var (
		sourceAvailable int
		sourceReserved  int
		sourceReleased  int
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				available_quantity,
				active_reserved_quantity,
				released_quantity
			FROM ga_allocation_buckets
			WHERE allocation_id = $1
			  AND ga_pool_id = $2
		`,
		releasedAllocationID,
		f.gaMainID,
	).Scan(
		&sourceAvailable,
		&sourceReserved,
		&sourceReleased,
	); err != nil {
		t.Fatalf(
			"read released source bucket: %v",
			err,
		)
	}

	if sourceAvailable != 0 ||
		sourceReserved != 0 ||
		sourceReleased != 10 {
		t.Fatalf(
			"released source bucket available=%d reserved=%d released=%d",
			sourceAvailable,
			sourceReserved,
			sourceReleased,
		)
	}

	var sharedAvailable int

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT available_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		f.gaMainID,
	).Scan(
		&sharedAvailable,
	); err != nil {
		t.Fatalf(
			"read shared GA: %v",
			err,
		)
	}

	if sharedAvailable != 35 {
		t.Fatalf(
			"shared GA after source release=%d want=35",
			sharedAvailable,
		)
	}

	unauthorized, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.otherPartnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind:      reservation.InventoryGA,
						InventoryID:        f.gaMainID,
						Quantity:           1,
						SourceKind:         reservation.SourceAllocation,
						SourceAllocationID: &allocationID,
					},
				},
			},
		)

	_ = unauthorized

	assertAPIErrorCode(
		t,
		err,
		apierror.
			CodeInventoryNotEligibleForPartner,
	)
}

func TestReservationMixedAtomicityAndReservedConcurrency(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	blocker, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind: reservation.InventoryReserved,
						InventoryID:   f.seatIDs["A2"],
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create blocker hold: %v",
			err,
		)
	}

	var beforeGA int

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT available_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		f.gaMainID,
	).Scan(
		&beforeGA,
	); err != nil {
		t.Fatalf(
			"read GA before mixed hold: %v",
			err,
		)
	}

	_, err = f.reservation.Create(
		ctx,
		reservation.CreateInput{
			EventID:   f.eventID,
			PartnerID: f.partnerID,
			Items: []reservation.ItemInput{
				{
					InventoryKind: reservation.InventoryReserved,
					InventoryID:   f.seatIDs["A2"],
					Quantity:      1,
					SourceKind:    reservation.SourceShared,
				},
				{
					InventoryKind: reservation.InventoryGA,
					InventoryID:   f.gaMainID,
					Quantity:      3,
					SourceKind:    reservation.SourceShared,
				},
			},
		},
	)
	assertAPIErrorCode(
		t,
		err,
		apierror.CodeInventoryUnavailable,
	)

	var afterGA int

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT available_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		f.gaMainID,
	).Scan(
		&afterGA,
	); err != nil {
		t.Fatalf(
			"read GA after failed mixed hold: %v",
			err,
		)
	}

	if beforeGA != afterGA {
		t.Fatalf(
			"failed mixed hold moved GA: before=%d after=%d",
			beforeGA,
			afterGA,
		)
	}

	if err := f.reservation.Release(
		ctx,
		f.partnerID,
		blocker.Token,
	); err != nil {
		t.Fatalf(
			"release blocker: %v",
			err,
		)
	}

	var (
		successes atomic.Int64
		mu        sync.Mutex
		winner    *reservation.Created
		wg        sync.WaitGroup
	)

	var unavailable atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			created, err :=
				f.reservation.Create(
					ctx,
					reservation.CreateInput{
						EventID:   f.eventID,
						PartnerID: f.partnerID,
						Items: []reservation.ItemInput{
							{
								InventoryKind: reservation.InventoryReserved,
								InventoryID:   f.seatIDs["A1"],
								Quantity:      1,
								SourceKind:    reservation.SourceShared,
							},
						},
					},
				)

			if err != nil {
				if apiErr, ok := apierror.As(err); ok && apiErr.Code == apierror.CodeInventoryUnavailable {
					unavailable.Add(1)
				}
				return
			}

			successes.Add(1)

			mu.Lock()
			value := created
			winner = &value
			mu.Unlock()
		}()
	}

	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf(
			"reserved concurrency successes=%d want=1",
			successes.Load(),
		)
	}
	if unavailable.Load() != 99 {
		t.Fatalf("reserved concurrency unavailable=%d want=99", unavailable.Load())
	}

	if winner == nil {
		t.Fatal(
			"reserved concurrency winner missing",
		)
	}

	assertClaimType(
		t,
		ctx,
		f.pool,
		f.seatIDs["A1"],
		"RESERVATION",
	)

	if err := f.reservation.Release(
		ctx,
		f.partnerID,
		winner.Token,
	); err != nil {
		t.Fatalf(
			"release concurrency winner: %v",
			err,
		)
	}
}

func TestReservationGAContention(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	var (
		successes atomic.Int64
		mu        sync.Mutex
		winners   []reservation.Created
		wg        sync.WaitGroup
	)

	var unavailable atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			created, err :=
				f.reservation.Create(
					ctx,
					reservation.CreateInput{
						EventID:   f.eventID,
						PartnerID: f.partnerID,
						Items: []reservation.ItemInput{
							{
								InventoryKind: reservation.InventoryGA,
								InventoryID:   f.gaSmallID,
								Quantity:      1,
								SourceKind:    reservation.SourceShared,
							},
						},
					},
				)
			if err != nil {
				if apiErr, ok := apierror.As(err); ok && apiErr.Code == apierror.CodeInsufficientGAQuantity {
					unavailable.Add(1)
				}
				return
			}

			successes.Add(1)

			mu.Lock()
			winners = append(
				winners,
				created,
			)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if successes.Load() != 10 {
		t.Fatalf(
			"GA concurrency successes=%d want=10",
			successes.Load(),
		)
	}
	if unavailable.Load() != 90 {
		t.Fatalf("GA concurrency unavailable=%d want=90", unavailable.Load())
	}

	var (
		available int
		reserved  int
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				available_quantity,
				active_reserved_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		f.gaSmallID,
	).Scan(
		&available,
		&reserved,
	); err != nil {
		t.Fatalf(
			"read GA contention counters: %v",
			err,
		)
	}

	if available != 0 ||
		reserved != 10 {
		t.Fatalf(
			"GA contention available=%d reserved=%d",
			available,
			reserved,
		)
	}

	for _, winner := range winners {
		if err := f.reservation.Release(
			ctx,
			f.partnerID,
			winner.Token,
		); err != nil {
			t.Fatalf(
				"release GA winner: %v",
				err,
			)
		}
	}
}

func TestReservationCheckoutRetryReconciliationAndTokenRecovery(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	created, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				PartnerCustomerRef: "cust-" +
					uuid.NewString(),
				Items: []reservation.ItemInput{
					{
						InventoryKind: reservation.InventoryReserved,
						InventoryID:   f.seatIDs["A5"],
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
					{
						InventoryKind: reservation.InventoryGA,
						InventoryID:   f.gaMainID,
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create checkout hold: %v",
			err,
		)
	}

	recovered, err :=
		f.reservation.RecoverToken(
			ctx,
			created.ReservationID,
			f.partnerID,
		)
	if err != nil {
		t.Fatalf(
			"recover token: %v",
			err,
		)
	}

	if recovered != created.Token {
		t.Fatal(
			"deterministic token recovery changed token",
		)
	}

	replacement := "A"
	if created.Token[len(created.Token)-1] == 'A' {
		replacement = "B"
	}
	tampered := created.Token[:len(created.Token)-1] + replacement

	_, err = f.reservation.BeginCheckout(
		ctx,
		f.partnerID,
		tampered,
	)
	assertAPIErrorCode(
		t,
		err,
		apierror.CodeHoldNotOwned,
	)

	checkout1, err :=
		f.reservation.BeginCheckout(
			ctx,
			f.partnerID,
			created.Token,
		)
	if err != nil {
		t.Fatalf(
			"begin checkout: %v",
			err,
		)
	}

	if checkout1.AttemptNumber != 1 ||
		checkout1.State != "COMMITTING" {
		t.Fatalf(
			"unexpected first checkout: %+v",
			checkout1,
		)
	}

	retry, err :=
		f.reservation.PaymentFailed(
			ctx,
			f.partnerID,
			created.Token,
			"pay-failed",
			"DECLINED",
		)
	if err != nil {
		t.Fatalf(
			"payment failed transition: %v",
			err,
		)
	}

	if retry.State != "PAYMENT_RETRY" {
		t.Fatalf(
			"retry state=%s",
			retry.State,
		)
	}

	checkout2, err :=
		f.reservation.BeginCheckout(
			ctx,
			f.partnerID,
			created.Token,
		)
	if err != nil {
		t.Fatalf(
			"begin retry checkout: %v",
			err,
		)
	}

	if checkout2.AttemptNumber != 2 {
		t.Fatalf(
			"retry attempt number=%d want=2",
			checkout2.AttemptNumber,
		)
	}

	recon, err :=
		f.reservation.MarkPaymentUncertain(
			ctx,
			f.partnerID,
			created.Token,
			"pay-unknown",
			"TIMEOUT",
		)
	if err != nil {
		t.Fatalf(
			"mark uncertain: %v",
			err,
		)
	}

	if recon.State != "RECONCILING" {
		t.Fatalf(
			"reconciliation state=%s",
			recon.State,
		)
	}

	reconAgain, err :=
		f.reservation.MarkPaymentUncertain(
			ctx,
			f.partnerID,
			created.Token,
			"pay-unknown",
			"TIMEOUT",
		)
	if err != nil {
		t.Fatalf(
			"repeat uncertain: %v",
			err,
		)
	}

	if !reconAgain.
		ReconciliationExpiresAt.Equal(
		recon.
			ReconciliationExpiresAt,
	) {
		t.Fatal(
			"repeated uncertainty reset reconciliation deadline",
		)
	}

	if err :=
		f.reservation.ResolveNoPayment(
			ctx,
			f.partnerID,
			created.Token,
			"NOT_CHARGED",
		); err != nil {
		t.Fatalf(
			"resolve no payment: %v",
			err,
		)
	}

	assertReservationState(
		t,
		ctx,
		f.pool,
		created.ReservationID,
		"RELEASED",
	)

	assertNoActiveClaim(
		t,
		ctx,
		f.pool,
		f.seatIDs["A5"],
	)
}

func TestReservationDefinitiveFailureDuringReconciliation(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	created, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind: reservation.InventoryReserved,
						InventoryID:   f.seatIDs["A5"],
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create reconciliation hold: %v",
			err,
		)
	}

	checkout1, err :=
		f.reservation.BeginCheckout(
			ctx,
			f.partnerID,
			created.Token,
		)
	if err != nil {
		t.Fatalf(
			"begin reconciliation checkout: %v",
			err,
		)
	}

	if _, err :=
		f.reservation.MarkPaymentUncertain(
			ctx,
			f.partnerID,
			created.Token,
			"pay-uncertain-1",
			"TIMEOUT",
		); err != nil {
		t.Fatalf(
			"mark first checkout uncertain: %v",
			err,
		)
	}

	retry, err :=
		f.reservation.PaymentFailure(
			ctx,
			f.partnerID,
			created.Token,
			checkout1.CheckoutAttemptID,
			"pay-uncertain-1",
			"DECLINED",
			"RETRY",
		)
	if err != nil {
		t.Fatalf(
			"definitive failure during reconciliation retry: %v",
			err,
		)
	}

	if retry.State != "PAYMENT_RETRY" {
		t.Fatalf(
			"reconciliation failure retry state=%s want=PAYMENT_RETRY",
			retry.State,
		)
	}

	var attempt1State string

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM checkout_attempts
			WHERE id = $1
		`,
		checkout1.CheckoutAttemptID,
	).Scan(&attempt1State); err != nil {
		t.Fatalf(
			"read first checkout attempt: %v",
			err,
		)
	}

	if attempt1State != "PAYMENT_FAILED" {
		t.Fatalf(
			"first uncertain attempt state=%s want=PAYMENT_FAILED",
			attempt1State,
		)
	}

	checkout2, err :=
		f.reservation.BeginCheckout(
			ctx,
			f.partnerID,
			created.Token,
		)
	if err != nil {
		t.Fatalf(
			"begin checkout after reconciliation retry: %v",
			err,
		)
	}

	if checkout2.AttemptNumber != 2 {
		t.Fatalf(
			"second checkout attempt number=%d want=2",
			checkout2.AttemptNumber,
		)
	}

	if _, err :=
		f.reservation.MarkPaymentUncertain(
			ctx,
			f.partnerID,
			created.Token,
			"pay-uncertain-2",
			"TIMEOUT",
		); err != nil {
		t.Fatalf(
			"mark second checkout uncertain: %v",
			err,
		)
	}

	released, err :=
		f.reservation.PaymentFailure(
			ctx,
			f.partnerID,
			created.Token,
			checkout2.CheckoutAttemptID,
			"pay-uncertain-2",
			"DECLINED",
			"RELEASE",
		)
	if err != nil {
		t.Fatalf(
			"definitive failure during reconciliation release: %v",
			err,
		)
	}

	if released.State != "RELEASED" {
		t.Fatalf(
			"reconciliation failure release state=%s want=RELEASED",
			released.State,
		)
	}

	assertReservationState(
		t,
		ctx,
		f.pool,
		created.ReservationID,
		"RELEASED",
	)

	assertNoActiveClaim(
		t,
		ctx,
		f.pool,
		f.seatIDs["A5"],
	)
}

func TestReservationModificationAndWorkerExpiry(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	created, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind: reservation.InventoryReserved,
						InventoryID:   f.seatIDs["A6"],
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create modifiable hold: %v",
			err,
		)
	}

	originalExpiry :=
		created.HoldExpiresAt

	modified, err :=
		f.reservation.Modify(
			ctx,
			f.partnerID,
			created.Token,
			[]reservation.ItemInput{
				{
					InventoryKind: reservation.InventoryReserved,
					InventoryID:   f.seatIDs["A7"],
					Quantity:      1,
					SourceKind:    reservation.SourceShared,
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"modify Reservation: %v",
			err,
		)
	}

	if !modified.HoldExpiresAt.Equal(
		originalExpiry,
	) {
		t.Fatal(
			"Reservation modification reset hold timer",
		)
	}

	assertNoActiveClaim(
		t,
		ctx,
		f.pool,
		f.seatIDs["A6"],
	)

	assertClaimType(
		t,
		ctx,
		f.pool,
		f.seatIDs["A7"],
		"RESERVATION",
	)

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE reservations
			SET
				hold_expires_at =
				    clock_timestamp() -
				    interval '1 second'
			WHERE id = $1
		`,
		created.ReservationID,
	); err != nil {
		t.Fatalf(
			"force due hold: %v",
			err,
		)
	}

	materializer :=
		reservation.NewMaterializer(
			f.pool,
			f.reservation,
			500,
		)

	if err := materializer.RunOnce(
		ctx,
	); err != nil {
		t.Fatalf(
			"materializer: %v",
			err,
		)
	}

	assertReservationState(
		t,
		ctx,
		f.pool,
		created.ReservationID,
		"EXPIRED",
	)

	assertNoActiveClaim(
		t,
		ctx,
		f.pool,
		f.seatIDs["A7"],
	)
}

func newFixture(
	t *testing.T,
) fixture {
	t.Helper()

	databaseURL :=
		os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal(
			"DATABASE_URL is required",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		45*time.Second,
	)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"open database: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	runner := database.NewRunner(
		pool,
		5,
		5*time.Millisecond,
	)

	userID := uuid.New()
	subject := uuid.NewString()

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO app_users (
				id,
				auth_provider,
				auth_subject,
				display_name,
				state,
				created_at,
				updated_at
			)
			VALUES (
				$1,
				'reservation',
				$2,
				'Reservation Admin',
				'ACTIVE',
				clock_timestamp(),
				clock_timestamp()
			)
		`,
		userID,
		subject,
	); err != nil {
		t.Fatalf(
			"create Reservation app user: %v",
			err,
		)
	}

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO platform_user_roles (
				user_id,
				role,
				created_at
			)
			VALUES (
				$1,
				'PLATFORM_ADMIN',
				clock_timestamp()
			)
		`,
		userID,
	); err != nil {
		t.Fatalf(
			"create Reservation role: %v",
			err,
		)
	}

	venueService :=
		venuesvc.NewService(
			runner,
		)

	eventService :=
		eventsvc.NewService(
			runner,
		)

	partnerService :=
		partnersvc.NewService(
			runner,
		)

	allocationService :=
		allocsvc.NewService(
			runner,
		)

	venueID, err :=
		venueService.CreateVenue(
			ctx,
			userID,
			venuesvc.CreateVenueInput{
				Name: "Reservation Venue " +
					uuid.NewString(),
			},
		)
	if err != nil {
		t.Fatalf(
			"create Venue: %v",
			err,
		)
	}

	layoutID, _, err :=
		venueService.CreateLayoutVersion(
			ctx,
			userID,
			venueID,
		)
	if err != nil {
		t.Fatalf(
			"create layout: %v",
			err,
		)
	}

	mainCapacity := 40
	smallCapacity := 10

	seats := make(
		[]venuesvc.SeatInput,
		0,
		7,
	)

	for i := 1; i <= 7; i++ {
		seats = append(
			seats,
			venuesvc.SeatInput{
				ObjectKey: "A" +
					string(
						rune(
							'0'+i,
						),
					),
				SectionKey: "reserved",
				RowKey:     "row-a",
				SeatLabel: string(
					rune(
						'0' + i,
					),
				),
			},
		)
	}

	if err :=
		venueService.ReplaceDraftLayout(
			ctx,
			userID,
			layoutID,
			venuesvc.ReplaceLayoutInput{
				Sections: []venuesvc.SectionInput{
					{
						ObjectKey: "reserved",
						Name:      "Reserved",
						Kind:      "RESERVED",
					},
					{
						ObjectKey: "ga",
						Name:      "GA",
						Kind:      "GA",
					},
				},
				Rows: []venuesvc.RowInput{
					{
						ObjectKey:  "row-a",
						SectionKey: "reserved",
						Label:      "A",
					},
				},
				Seats: seats,
				GAZones: []venuesvc.GAZoneInput{
					{
						ObjectKey:       "ga-main",
						SectionKey:      "ga",
						Name:            "GA Main",
						DefaultCapacity: &mainCapacity,
					},
					{
						ObjectKey:       "ga-small",
						SectionKey:      "ga",
						Name:            "GA Small",
						DefaultCapacity: &smallCapacity,
					},
				},
			},
		); err != nil {
		t.Fatalf(
			"replace layout: %v",
			err,
		)
	}

	if err :=
		venueService.PublishLayout(
			ctx,
			userID,
			layoutID,
		); err != nil {
		t.Fatalf(
			"publish layout: %v",
			err,
		)
	}

	now := time.Now().UTC()

	eventID, err :=
		eventService.Create(
			ctx,
			userID,
			eventsvc.CreateInput{
				VenueID: venueID,
				Name: "Reservation Event " +
					uuid.NewString(),
				StartsAt: timePtr(
					now.Add(
						48 *
							time.Hour,
					),
				),
				SalesOpenAt: timePtr(
					now.Add(
						-time.Hour,
					),
				),
				SalesCloseAt: timePtr(
					now.Add(
						47 *
							time.Hour,
					),
				),
				TimezoneName: "UTC",
			},
		)
	if err != nil {
		t.Fatalf(
			"create Event: %v",
			err,
		)
	}

	if err :=
		eventService.MaterializeLayout(
			ctx,
			userID,
			eventID,
			layoutID,
		); err != nil {
		t.Fatalf(
			"materialize layout: %v",
			err,
		)
	}

	tierID, err :=
		eventService.CreatePriceTier(
			ctx,
			userID,
			eventID,
			eventsvc.PriceTierInput{
				Code:        "STANDARD",
				Name:        "Standard",
				AmountMinor: 10000,
				Currency:    "NGN",
			},
		)
	if err != nil {
		t.Fatalf(
			"create price tier: %v",
			err,
		)
	}

	if err :=
		eventService.AssignPricing(
			ctx,
			userID,
			eventID,
			eventsvc.PricingAssignmentInput{
				PriceTierID: tierID,
				SectionObjectKeys: []string{
					"reserved",
				},
				GAPoolObjectKeys: []string{
					"ga-main",
					"ga-small",
				},
			},
		); err != nil {
		t.Fatalf(
			"assign pricing: %v",
			err,
		)
	}

	if err :=
		eventService.ConfigureTransactionPolicy(
			ctx,
			userID,
			eventID,
			eventsvc.TransactionPolicyInput{
				HoldDurationSeconds:                  300,
				CheckoutProtectionSeconds:            90,
				PaymentRetrySeconds:                  30,
				ReconciliationSeconds:                60,
				MaxReservationLifetimeSeconds:        1200,
				MaxHoldQuantity:                      20,
				MaxActiveReservationsPerPartner:      200,
				MaxActiveReservationsPerBuyerSession: 20,
			},
		); err != nil {
		t.Fatalf(
			"configure transaction policy: %v",
			err,
		)
	}

	if err := eventService.OpenSales(
		ctx,
		userID,
		eventID,
	); err != nil {
		t.Fatalf(
			"open sales: %v",
			err,
		)
	}

	partnerID, err :=
		partnerService.Create(
			ctx,
			userID,
			"Reservation Partner "+
				uuid.NewString(),
		)
	if err != nil {
		t.Fatalf(
			"create Partner: %v",
			err,
		)
	}

	otherPartnerID, err :=
		partnerService.Create(
			ctx,
			userID,
			"Reservation Other Partner "+
				uuid.NewString(),
		)
	if err != nil {
		t.Fatalf(
			"create other Partner: %v",
			err,
		)
	}

	for _, partner := range []uuid.UUID{
		partnerID,
		otherPartnerID,
	} {
		if err :=
			partnerService.
				GrantEventAccess(
					ctx,
					userID,
					eventID,
					partner,
				); err != nil {
			t.Fatalf(
				"grant Event access: %v",
				err,
			)
		}
	}

	seatIDs := map[string]uuid.UUID{}

	for i := 1; i <= 7; i++ {
		key := "A" +
			string(
				rune(
					'0'+i,
				),
			)

		var id uuid.UUID

		if err := pool.QueryRow(
			ctx,
			`
				SELECT id
				FROM reserved_inventory_units
				WHERE event_id = $1
				  AND snapshot_object_key = $2
			`,
			eventID,
			key,
		).Scan(
			&id,
		); err != nil {
			t.Fatalf(
				"lookup seat %s: %v",
				key,
				err,
			)
		}

		seatIDs[key] = id
	}

	var (
		gaMainID  uuid.UUID
		gaSmallID uuid.UUID
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM ga_inventory_pools
			WHERE event_id = $1
			  AND snapshot_object_key =
			      'ga-main'
		`,
		eventID,
	).Scan(
		&gaMainID,
	); err != nil {
		t.Fatalf(
			"lookup ga-main: %v",
			err,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM ga_inventory_pools
			WHERE event_id = $1
			  AND snapshot_object_key =
			      'ga-small'
		`,
		eventID,
	).Scan(
		&gaSmallID,
	); err != nil {
		t.Fatalf(
			"lookup ga-small: %v",
			err,
		)
	}

	keyBytes := make(
		[]byte,
		32,
	)

	for i := range keyBytes {
		keyBytes[i] =
			byte(i + 1)
	}

	keyring, err :=
		auth.ParseHMACKeyring(
			1,
			"1:"+
				base64.
					RawURLEncoding.
					EncodeToString(
						keyBytes,
					),
		)
	if err != nil {
		t.Fatalf(
			"create HMAC keyring: %v",
			err,
		)
	}

	return fixture{
		pool:           pool,
		runner:         runner,
		userID:         userID,
		userSubject:    subject,
		eventID:        eventID,
		partnerID:      partnerID,
		otherPartnerID: otherPartnerID,
		seatIDs:        seatIDs,
		gaMainID:       gaMainID,
		gaSmallID:      gaSmallID,
		reservation: reservation.NewService(
			runner,
			keyring,
			keyring,
		),
		allocation: allocationService,
	}
}

func assertClaimType(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	inventoryID uuid.UUID,
	want string,
) {
	t.Helper()

	var got string

	if err := pool.QueryRow(
		ctx,
		`
			SELECT claim_type
			FROM reserved_inventory_claims
			WHERE reserved_inventory_unit_id = $1
			  AND ended_at IS NULL
		`,
		inventoryID,
	).Scan(
		&got,
	); err != nil {
		t.Fatalf(
			"read active claim: %v",
			err,
		)
	}

	if got != want {
		t.Fatalf(
			"claim type=%s want=%s",
			got,
			want,
		)
	}
}

func assertNoActiveClaim(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	inventoryID uuid.UUID,
) {
	t.Helper()

	var count int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM reserved_inventory_claims
			WHERE reserved_inventory_unit_id = $1
			  AND ended_at IS NULL
		`,
		inventoryID,
	).Scan(
		&count,
	); err != nil {
		t.Fatalf(
			"count active claims: %v",
			err,
		)
	}

	if count != 0 {
		t.Fatalf(
			"active claims=%d want=0",
			count,
		)
	}
}

func assertReservationState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	reservationID uuid.UUID,
	want string,
) {
	t.Helper()

	var got string

	if err := pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM reservations
			WHERE id = $1
		`,
		reservationID,
	).Scan(
		&got,
	); err != nil {
		t.Fatalf(
			"read Reservation state: %v",
			err,
		)
	}

	if got != want {
		t.Fatalf(
			"Reservation state=%s want=%s",
			got,
			want,
		)
	}
}

func assertAPIErrorCode(
	t *testing.T,
	err error,
	want apierror.Code,
) {
	t.Helper()

	if err == nil {
		t.Fatalf(
			"expected error code %s",
			want,
		)
	}

	var apiErr *apierror.Error

	if !errors.As(
		err,
		&apiErr,
	) {
		t.Fatalf(
			"error is not API error: %v",
			err,
		)
	}

	if apiErr.Code != want {
		t.Fatalf(
			"error code=%s want=%s error=%v",
			apiErr.Code,
			want,
			err,
		)
	}
}

func timePtr(
	value time.Time,
) *time.Time {
	return &value
}
