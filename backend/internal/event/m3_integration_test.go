//go:build integration

package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func TestM3ConfigurationFlow(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
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
				'm3-test',
				$2,
				'M3 Integration',
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
			Name:        "M3 Integration Venue " + uuid.NewString(),
			AddressText: "Test Address",
			Metadata:    json.RawMessage(`{"source":"integration"}`),
		},
	)
	if err != nil {
		t.Fatalf("create Venue: %v", err)
	}

	layoutID, version, err := venueService.CreateLayoutVersion(
		ctx,
		actorID,
		venueID,
	)
	if err != nil {
		t.Fatalf("create layout version: %v", err)
	}

	if version != 1 {
		t.Fatalf("first layout version = %d, want 1", version)
	}

	gaCapacity := 50

	err = venueService.ReplaceDraftLayout(
		ctx,
		actorID,
		layoutID,
		venuesvc.ReplaceLayoutInput{
			Geometry: json.RawMessage(`{"stage":{"side":"north"}}`),
			Sections: []venuesvc.SectionInput{
				{
					ObjectKey: "floor",
					Name:      "Floor",
					Kind:      "RESERVED",
					SortOrder: 1,
				},
				{
					ObjectKey: "standing",
					Name:      "Standing",
					Kind:      "GA",
					SortOrder: 2,
				},
			},
			Rows: []venuesvc.RowInput{
				{
					ObjectKey:  "floor-row-a",
					SectionKey: "floor",
					Label:      "A",
					SortOrder:  1,
				},
			},
			Seats: []venuesvc.SeatInput{
				{
					ObjectKey:  "floor-a-1",
					SectionKey: "floor",
					RowKey:     "floor-row-a",
					SeatLabel:  "1",
					SortOrder:  1,
				},
			},
			GAZones: []venuesvc.GAZoneInput{
				{
					ObjectKey:       "standing-main",
					SectionKey:      "standing",
					Name:            "Standing Main",
					DefaultCapacity: &gaCapacity,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("replace layout: %v", err)
	}

	if err := venueService.PublishLayout(
		ctx,
		actorID,
		layoutID,
	); err != nil {
		t.Fatalf("publish layout: %v", err)
	}

	var layoutState string
	if err := pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM venue_layout_versions
			WHERE id = $1
		`,
		layoutID,
	).Scan(&layoutState); err != nil {
		t.Fatalf("read layout state: %v", err)
	}

	if layoutState != "PUBLISHED" {
		t.Fatalf("layout state = %q, want PUBLISHED", layoutState)
	}

	if _, err := pool.Exec(
		ctx,
		`
			UPDATE venue_layout_versions
			SET geometry_json = '{"changed":true}'::jsonb
			WHERE id = $1
		`,
		layoutID,
	); err == nil {
		t.Fatal("published layout accepted physical mutation")
	}

	now := time.Now().UTC()

	eventID, err := eventService.Create(
		ctx,
		actorID,
		eventsvc.CreateInput{
			VenueID:          venueID,
			Name:             "M3 Integration Event " + uuid.NewString(),
			StartsAt:         ptrTime(now.Add(48 * time.Hour)),
			EndsAt:           ptrTime(now.Add(52 * time.Hour)),
			SalesOpenAt:      ptrTime(now.Add(-time.Hour)),
			SalesCloseAt:     ptrTime(now.Add(47 * time.Hour)),
			AdmissionOpenAt:  ptrTime(now.Add(47 * time.Hour)),
			AdmissionCloseAt: ptrTime(now.Add(53 * time.Hour)),
			TimezoneName:     "UTC",
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
		t.Fatalf("materialize layout: %v", err)
	}

	var (
		reservedCount int
		gaCount       int
		gaAvailable   int
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				(SELECT COUNT(*) FROM reserved_inventory_units WHERE event_id = $1),
				(SELECT COUNT(*) FROM ga_inventory_pools WHERE event_id = $1),
				COALESCE((
					SELECT SUM(gsi.available_quantity)
					FROM ga_shared_inventory gsi
					JOIN ga_inventory_pools gp
					  ON gp.id = gsi.ga_pool_id
					WHERE gp.event_id = $1
				), 0)
		`,
		eventID,
	).Scan(
		&reservedCount,
		&gaCount,
		&gaAvailable,
	); err != nil {
		t.Fatalf("read materialized inventory: %v", err)
	}

	if reservedCount != 1 || gaCount != 1 || gaAvailable != 50 {
		t.Fatalf(
			"unexpected inventory: reserved=%d ga=%d available=%d",
			reservedCount,
			gaCount,
			gaAvailable,
		)
	}

	reservedTierID, err := eventService.CreatePriceTier(
		ctx,
		actorID,
		eventID,
		eventsvc.PriceTierInput{
			Code:        "RESERVED",
			Name:        "Reserved",
			AmountMinor: 25000,
			Currency:    "NGN",
		},
	)
	if err != nil {
		t.Fatalf("create reserved price tier: %v", err)
	}

	gaTierID, err := eventService.CreatePriceTier(
		ctx,
		actorID,
		eventID,
		eventsvc.PriceTierInput{
			Code:        "GA",
			Name:        "General Admission",
			AmountMinor: 10000,
			Currency:    "NGN",
		},
	)
	if err != nil {
		t.Fatalf("create GA price tier: %v", err)
	}

	if err := eventService.AssignPricing(
		ctx,
		actorID,
		eventID,
		eventsvc.PricingAssignmentInput{
			PriceTierID:       reservedTierID,
			SectionObjectKeys: []string{"floor"},
		},
	); err != nil {
		t.Fatalf("assign reserved pricing: %v", err)
	}

	if err := eventService.AssignPricing(
		ctx,
		actorID,
		eventID,
		eventsvc.PricingAssignmentInput{
			PriceTierID:      gaTierID,
			GAPoolObjectKeys: []string{"standing-main"},
		},
	); err != nil {
		t.Fatalf("assign GA pricing: %v", err)
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
			AllowVoidedInventoryRerelease:        false,
		},
	); err != nil {
		t.Fatalf("configure Event policy: %v", err)
	}

	if err := eventService.OpenSales(
		ctx,
		actorID,
		eventID,
	); err != nil {
		t.Fatalf("open sales: %v", err)
	}

	var eventState string
	if err := pool.QueryRow(
		ctx,
		`SELECT state FROM events WHERE id = $1`,
		eventID,
	).Scan(&eventState); err != nil {
		t.Fatalf("read Event state: %v", err)
	}

	if eventState != "ON_SALE" {
		t.Fatalf("Event state = %q, want ON_SALE", eventState)
	}

	err = eventService.MaterializeLayout(
		ctx,
		actorID,
		eventID,
		layoutID,
	)
	if err == nil {
		t.Fatal("live Event accepted silent re-materialization")
	}

	partnerID, err := partnerService.Create(
		ctx,
		actorID,
		"M3 Partner "+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("create Partner: %v", err)
	}

	credentialID, rawCredential, err := partnerService.CreateCredential(
		ctx,
		actorID,
		partnerID,
	)
	if err != nil {
		t.Fatalf("create Partner credential: %v", err)
	}

	if credentialID == uuid.Nil || rawCredential == "" {
		t.Fatal("Partner credential was not returned")
	}

	if err := partnerService.GrantEventAccess(
		ctx,
		actorID,
		eventID,
		partnerID,
	); err != nil {
		t.Fatalf("grant Partner Event access: %v", err)
	}

	authenticator := auth.NewPartnerAuthenticator(pool)
	authorizer := auth.NewAuthorizer(pool)

	principal, err := authenticator.Authenticate(
		ctx,
		rawCredential,
	)
	if err != nil {
		t.Fatalf("authenticate Partner: %v", err)
	}

	if err := authorizer.RequireNewPartnerAcquisition(
		ctx,
		principal,
		eventID,
	); err != nil {
		t.Fatalf("active Partner acquisition authorization: %v", err)
	}

	if err := partnerService.SetEnabled(
		ctx,
		actorID,
		partnerID,
		false,
	); err != nil {
		t.Fatalf("disable Partner: %v", err)
	}

	if _, err := authenticator.Authenticate(
		ctx,
		rawCredential,
	); err != nil {
		t.Fatalf(
			"operational Partner disable incorrectly revoked credential: %v",
			err,
		)
	}

	err = authorizer.RequireNewPartnerAcquisition(
		ctx,
		principal,
		eventID,
	)
	requireBusinessCode(
		t,
		err,
		apierror.CodePartnerDisabled,
	)

	if err := partnerService.SetEnabled(
		ctx,
		actorID,
		partnerID,
		true,
	); err != nil {
		t.Fatalf("re-enable Partner: %v", err)
	}

	if err := partnerService.DisableEventAccess(
		ctx,
		actorID,
		eventID,
		partnerID,
	); err != nil {
		t.Fatalf("disable Partner Event access: %v", err)
	}

	principal, err = authenticator.Authenticate(
		ctx,
		rawCredential,
	)
	if err != nil {
		t.Fatalf("authenticate after access disable: %v", err)
	}

	err = authorizer.RequireNewPartnerAcquisition(
		ctx,
		principal,
		eventID,
	)
	requireBusinessCode(
		t,
		err,
		apierror.CodePartnerEventAccessDisabled,
	)

	if err := partnerService.RevokeCredential(
		ctx,
		actorID,
		credentialID,
	); err != nil {
		t.Fatalf("revoke Partner credential: %v", err)
	}

	if _, err := authenticator.Authenticate(
		ctx,
		rawCredential,
	); err == nil {
		t.Fatal("revoked credential still authenticated")
	}

	var (
		auditCount  int
		outboxCount int
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				(SELECT COUNT(*) FROM audit_events WHERE event_id = $1),
				(SELECT COUNT(*) FROM outbox_events WHERE event_id = $1)
		`,
		eventID,
	).Scan(
		&auditCount,
		&outboxCount,
	); err != nil {
		t.Fatalf("read Event audit/outbox counts: %v", err)
	}

	if auditCount == 0 || outboxCount == 0 {
		t.Fatalf(
			"expected Event audit/outbox facts, got audit=%d outbox=%d",
			auditCount,
			outboxCount,
		)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func requireBusinessCode(
	t *testing.T,
	err error,
	code apierror.Code,
) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected business error %s", code)
	}

	var businessErr *apierror.Error
	if !errors.As(err, &businessErr) {
		t.Fatalf("expected apierror.Error, got %T: %v", err, err)
	}

	if businessErr.Code != code {
		t.Fatalf(
			"business code = %s, want %s",
			businessErr.Code,
			code,
		)
	}
}
