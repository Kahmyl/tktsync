//go:build integration

package allocation_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func TestRestrictionsAllocationsAndAvailability(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		40*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	runner := database.NewRunner(
		pool,
		3,
		5*time.Millisecond,
	)

	venueService := venuesvc.NewService(runner)
	eventService := eventsvc.NewService(runner)
	partnerService := partnersvc.NewService(runner)
	allocationService := allocsvc.NewService(runner)
	availabilityService := inventory.NewService(pool)

	actorID := uuid.New()

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
				'allocation-test',
				$2,
				'Allocation Integration',
				'ACTIVE',
				clock_timestamp(),
				clock_timestamp()
			)
		`,
		actorID,
		actorID.String(),
	); err != nil {
		t.Fatalf("create actor: %v", err)
	}

	venueID, err := venueService.CreateVenue(
		ctx,
		actorID,
		venuesvc.CreateVenueInput{
			Name: "Allocation Venue " +
				uuid.NewString(),
			Metadata: json.RawMessage(`{}`),
		},
	)
	if err != nil {
		t.Fatalf("create Venue: %v", err)
	}

	layoutID, _, err :=
		venueService.CreateLayoutVersion(
			ctx,
			actorID,
			venueID,
		)
	if err != nil {
		t.Fatalf(
			"create layout version: %v",
			err,
		)
	}

	gaCapacity := 50

	if err := venueService.ReplaceDraftLayout(
		ctx,
		actorID,
		layoutID,
		venuesvc.ReplaceLayoutInput{
			Geometry: json.RawMessage(`{}`),
			Sections: []venuesvc.SectionInput{
				{
					ObjectKey: "reserved",
					Name:      "Reserved",
					Kind:      "RESERVED",
					SortOrder: 1,
				},
				{
					ObjectKey: "ga",
					Name:      "GA",
					Kind:      "GA",
					SortOrder: 2,
				},
			},
			Rows: []venuesvc.RowInput{
				{
					ObjectKey:  "row-a",
					SectionKey: "reserved",
					Label:      "A",
					SortOrder:  1,
				},
			},
			Seats: []venuesvc.SeatInput{
				{
					ObjectKey:  "seat-a1",
					SectionKey: "reserved",
					RowKey:     "row-a",
					SeatLabel:  "1",
					SortOrder:  1,
				},
				{
					ObjectKey:  "seat-a2",
					SectionKey: "reserved",
					RowKey:     "row-a",
					SeatLabel:  "2",
					SortOrder:  2,
				},
			},
			GAZones: []venuesvc.GAZoneInput{
				{
					ObjectKey:       "ga-main",
					SectionKey:      "ga",
					Name:            "GA Main",
					DefaultCapacity: &gaCapacity,
				},
			},
		},
	); err != nil {
		t.Fatalf("replace layout: %v", err)
	}

	if err := venueService.PublishLayout(
		ctx,
		actorID,
		layoutID,
	); err != nil {
		t.Fatalf("publish layout: %v", err)
	}

	now := time.Now().UTC()

	eventID, err := eventService.Create(
		ctx,
		actorID,
		eventsvc.CreateInput{
			VenueID:      venueID,
			Name:         "Allocation Event " + uuid.NewString(),
			StartsAt:     ptr(now.Add(48 * time.Hour)),
			SalesOpenAt:  ptr(now.Add(-time.Hour)),
			SalesCloseAt: ptr(now.Add(47 * time.Hour)),
			TimezoneName: "UTC",
		},
	)
	if err != nil {
		t.Fatalf("create Event: %v", err)
	}

	if err := eventService.MaterializeLayout(
		ctx,
		actorID,
		eventID,
		layoutID,
	); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	priceID, err := eventService.CreatePriceTier(
		ctx,
		actorID,
		eventID,
		eventsvc.PriceTierInput{
			Code:        "STANDARD",
			Name:        "Standard",
			AmountMinor: 10000,
			Currency:    "NGN",
		},
	)
	if err != nil {
		t.Fatalf("price tier: %v", err)
	}

	if err := eventService.AssignPricing(
		ctx,
		actorID,
		eventID,
		eventsvc.PricingAssignmentInput{
			PriceTierID:       priceID,
			SectionObjectKeys: []string{"reserved"},
			GAPoolObjectKeys:  []string{"ga-main"},
		},
	); err != nil {
		t.Fatalf("assign pricing: %v", err)
	}

	if err := eventService.ConfigureTransactionPolicy(
		ctx,
		actorID,
		eventID,
		eventsvc.TransactionPolicyInput{
			HoldDurationSeconds:                  600,
			CheckoutProtectionSeconds:            120,
			PaymentRetrySeconds:                  60,
			ReconciliationSeconds:                300,
			MaxReservationLifetimeSeconds:        1200,
			MaxHoldQuantity:                      8,
			MaxActiveReservationsPerPartner:      20,
			MaxActiveReservationsPerBuyerSession: 3,
		},
	); err != nil {
		t.Fatalf("policy: %v", err)
	}

	if err := eventService.OpenSales(
		ctx,
		actorID,
		eventID,
	); err != nil {
		t.Fatalf("open sales: %v", err)
	}

	partnerA, err := partnerService.Create(
		ctx,
		actorID,
		"Allocation Partner A "+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("create Partner A: %v", err)
	}

	partnerB, err := partnerService.Create(
		ctx,
		actorID,
		"Allocation Partner B "+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("create Partner B: %v", err)
	}

	for _, partnerID := range []uuid.UUID{
		partnerA,
		partnerB,
	} {
		if err := partnerService.GrantEventAccess(
			ctx,
			actorID,
			eventID,
			partnerID,
		); err != nil {
			t.Fatalf(
				"grant Event access: %v",
				err,
			)
		}
	}

	var (
		seat1  uuid.UUID
		seat2  uuid.UUID
		gaPool uuid.UUID
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM reserved_inventory_units
			WHERE event_id = $1
			  AND snapshot_object_key = 'seat-a1'
		`,
		eventID,
	).Scan(&seat1); err != nil {
		t.Fatalf("seat 1: %v", err)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM reserved_inventory_units
			WHERE event_id = $1
			  AND snapshot_object_key = 'seat-a2'
		`,
		eventID,
	).Scan(&seat2); err != nil {
		t.Fatalf("seat 2: %v", err)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM ga_inventory_pools
			WHERE event_id = $1
			  AND snapshot_object_key = 'ga-main'
		`,
		eventID,
	).Scan(&gaPool); err != nil {
		t.Fatalf("GA pool: %v", err)
	}

	blockID, err := allocationService.CreateBlock(
		ctx,
		actorID,
		eventID,
		allocsvc.BlockInput{
			Purpose:         "PRODUCTION",
			Reason:          "integration test",
			ReservedUnitIDs: []uuid.UUID{seat1},
			GATargets: []allocsvc.GATarget{
				{
					PoolID:   gaPool,
					Quantity: 10,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("create Block: %v", err)
	}

	assertGA(
		t,
		ctx,
		pool,
		gaPool,
		40,
		10,
		0,
	)

	availableA, err :=
		availabilityService.PartnerAvailability(
			ctx,
			partnerA,
			eventID,
		)
	if err != nil {
		t.Fatalf("availability after Block: %v", err)
	}

	if reservedSellability(
		availableA,
		seat1,
	) != "UNAVAILABLE" {
		t.Fatal("blocked seat remained sellable")
	}

	if err := allocationService.ReleaseBlock(
		ctx,
		actorID,
		blockID,
	); err != nil {
		t.Fatalf("release Block: %v", err)
	}

	assertGA(
		t,
		ctx,
		pool,
		gaPool,
		50,
		0,
		0,
	)

	allocationID, err :=
		allocationService.CreateAllocation(
			ctx,
			actorID,
			eventID,
			allocsvc.AllocationInput{
				Mode:                   "CHANNEL",
				PartnerID:              &partnerA,
				Purpose:                "CHANNEL",
				Reason:                 "integration channel allocation",
				ReleaseDestinationKind: "SHARED",
				ReservedUnitIDs:        []uuid.UUID{seat1},
				GATargets: []allocsvc.GATarget{
					{
						PoolID:   gaPool,
						Quantity: 20,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create Allocation: %v",
			err,
		)
	}

	assertGA(
		t,
		ctx,
		pool,
		gaPool,
		30,
		0,
		20,
	)

	availableA, err =
		availabilityService.PartnerAvailability(
			ctx,
			partnerA,
			eventID,
		)
	if err != nil {
		t.Fatalf(
			"Partner A availability: %v",
			err,
		)
	}

	availableB, err :=
		availabilityService.PartnerAvailability(
			ctx,
			partnerB,
			eventID,
		)
	if err != nil {
		t.Fatalf(
			"Partner B availability: %v",
			err,
		)
	}

	if reservedSellability(
		availableA,
		seat1,
	) != "AVAILABLE" {
		t.Fatal(
			"own channel allocation not sellable to Partner A",
		)
	}

	if reservedSellability(
		availableB,
		seat1,
	) != "UNAVAILABLE" {
		t.Fatal(
			"Partner B saw Partner A allocated seat",
		)
	}

	if gaOfferCount(
		availableA,
		gaPool,
	) != 2 {
		t.Fatal(
			"Partner A should have shared and channel GA offers",
		)
	}

	if gaOfferCount(
		availableB,
		gaPool,
	) != 1 {
		t.Fatal(
			"Partner B should see only shared GA",
		)
	}

	var sharedBefore int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT available_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		gaPool,
	).Scan(&sharedBefore); err != nil {
		t.Fatalf("shared GA before conflict: %v", err)
	}

	_, err = allocationService.CreateBlock(
		ctx,
		actorID,
		eventID,
		allocsvc.BlockInput{
			Purpose:         "HOUSE",
			ReservedUnitIDs: []uuid.UUID{seat1},
			GATargets: []allocsvc.GATarget{
				{
					PoolID:   gaPool,
					Quantity: 5,
				},
			},
		},
	)

	requireCode(
		t,
		err,
		apierror.CodeInventoryUnavailable,
	)

	var sharedAfter int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT available_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		gaPool,
	).Scan(&sharedAfter); err != nil {
		t.Fatalf("shared GA after conflict: %v", err)
	}

	if sharedBefore != sharedAfter {
		t.Fatalf(
			"failed mixed Block changed GA: %d -> %d",
			sharedBefore,
			sharedAfter,
		)
	}

	if err := allocationService.ReleaseAllocation(
		ctx,
		actorID,
		allocationID,
	); err != nil {
		t.Fatalf(
			"release Allocation: %v",
			err,
		)
	}

	assertGA(
		t,
		ctx,
		pool,
		gaPool,
		50,
		0,
		0,
	)

	availableA, err =
		availabilityService.PartnerAvailability(
			ctx,
			partnerA,
			eventID,
		)
	if err != nil {
		t.Fatalf(
			"availability after Allocation release: %v",
			err,
		)
	}

	if reservedSellability(
		availableA,
		seat1,
	) != "AVAILABLE" {
		t.Fatal(
			"released allocation seat did not return to shared availability",
		)
	}

	start := make(chan struct{})
	results := make(
		chan error,
		2,
	)

	var winnerMu sync.Mutex
	var winner uuid.UUID

	for i := 0; i < 2; i++ {
		go func() {
			<-start

			blockID, err :=
				allocationService.CreateBlock(
					ctx,
					actorID,
					eventID,
					allocsvc.BlockInput{
						Purpose:         "HOUSE",
						ReservedUnitIDs: []uuid.UUID{seat2},
					},
				)

			if err == nil {
				winnerMu.Lock()
				winner = blockID
				winnerMu.Unlock()
			}

			results <- err
		}()
	}

	close(start)

	successes := 0
	conflicts := 0

	for i := 0; i < 2; i++ {
		err := <-results

		if err == nil {
			successes++
			continue
		}

		var businessErr *apierror.Error

		if errors.As(
			err,
			&businessErr,
		) &&
			businessErr.Code ==
				apierror.CodeInventoryUnavailable {
			conflicts++
			continue
		}

		t.Fatalf(
			"unexpected concurrent Block result: %v",
			err,
		)
	}

	if successes != 1 ||
		conflicts != 1 {
		t.Fatalf(
			"concurrent Block results success=%d conflict=%d",
			successes,
			conflicts,
		)
	}

	if winner != uuid.Nil {
		if err := allocationService.ReleaseBlock(
			ctx,
			actorID,
			winner,
		); err != nil {
			t.Fatalf(
				"release concurrency winner: %v",
				err,
			)
		}
	}
}

func ptr(
	value time.Time,
) *time.Time {
	return &value
}

func reservedSellability(
	availability inventory.Availability,
	id uuid.UUID,
) string {
	for _, item := range availability.ReservedUnits {
		if item.InventoryID == id {
			return item.Sellability
		}
	}

	return ""
}

func gaOfferCount(
	availability inventory.Availability,
	id uuid.UUID,
) int {
	for _, item := range availability.GAPools {
		if item.InventoryID == id {
			return len(item.Offers)
		}
	}

	return -1
}

func assertGA(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	gaPool uuid.UUID,
	sharedAvailable int,
	blocked int,
	allocatedAvailable int,
) {
	t.Helper()

	var (
		actualShared    int
		actualBlocked   int
		actualAllocated int
		capacity        int
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				gp.capacity,
				gsi.available_quantity,
				COALESCE((
					SELECT SUM(blocked_quantity)
					FROM ga_block_buckets
					WHERE ga_pool_id = gp.id
				), 0),
				COALESCE((
					SELECT SUM(available_quantity)
					FROM ga_allocation_buckets
					WHERE ga_pool_id = gp.id
				), 0)
			FROM ga_inventory_pools gp
			JOIN ga_shared_inventory gsi
			  ON gsi.ga_pool_id = gp.id
			WHERE gp.id = $1
		`,
		gaPool,
	).Scan(
		&capacity,
		&actualShared,
		&actualBlocked,
		&actualAllocated,
	); err != nil {
		t.Fatalf("read GA state: %v", err)
	}

	if actualShared != sharedAvailable ||
		actualBlocked != blocked ||
		actualAllocated != allocatedAvailable {
		t.Fatalf(
			"GA state shared=%d blocked=%d allocated=%d",
			actualShared,
			actualBlocked,
			actualAllocated,
		)
	}

	if capacity !=
		actualShared+
			actualBlocked+
			actualAllocated {
		t.Fatalf(
			"GA pool does not balance: capacity=%d components=%d",
			capacity,
			actualShared+
				actualBlocked+
				actualAllocated,
		)
	}
}

func requireCode(
	t *testing.T,
	err error,
	code apierror.Code,
) {
	t.Helper()

	if err == nil {
		t.Fatalf(
			"expected business error %s",
			code,
		)
	}

	var businessErr *apierror.Error

	if !errors.As(
		err,
		&businessErr,
	) {
		t.Fatalf(
			"expected apierror.Error, got %T: %v",
			err,
			err,
		)
	}

	if businessErr.Code != code {
		t.Fatalf(
			"business code=%s want=%s",
			businessErr.Code,
			code,
		)
	}
}
