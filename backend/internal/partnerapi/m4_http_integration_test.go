//go:build integration

package partnerapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/adminapi"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/partnerapi"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func TestM4AdminAndPartnerHTTP(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		45*time.Second,
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

	venueService :=
		venuesvc.NewService(runner)
	eventService :=
		eventsvc.NewService(runner)
	partnerService :=
		partnersvc.NewService(runner)
	allocationService :=
		allocsvc.NewService(runner)

	adminUserID := uuid.New()
	adminSubject := uuid.NewString()

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
				'm4-http',
				$2,
				'M4 HTTP Admin',
				'ACTIVE',
				clock_timestamp(),
				clock_timestamp()
			)
		`,
		adminUserID,
		adminSubject,
	); err != nil {
		t.Fatalf("create admin: %v", err)
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
		adminUserID,
	); err != nil {
		t.Fatalf("create admin role: %v", err)
	}

	venueID, err :=
		venueService.CreateVenue(
			ctx,
			adminUserID,
			venuesvc.CreateVenueInput{
				Name: "M4 HTTP Venue " +
					uuid.NewString(),
			},
		)
	if err != nil {
		t.Fatalf("create Venue: %v", err)
	}

	layoutID, _, err :=
		venueService.CreateLayoutVersion(
			ctx,
			adminUserID,
			venueID,
		)
	if err != nil {
		t.Fatalf("create layout: %v", err)
	}

	gaCapacity := 40

	if err := venueService.ReplaceDraftLayout(
		ctx,
		adminUserID,
		layoutID,
		venuesvc.ReplaceLayoutInput{
			Geometry: json.RawMessage(
				`{"stage":"north"}`,
			),
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
			Seats: []venuesvc.SeatInput{
				{
					ObjectKey:  "seat-a1",
					SectionKey: "reserved",
					RowKey:     "row-a",
					SeatLabel:  "1",
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
		adminUserID,
		layoutID,
	); err != nil {
		t.Fatalf("publish layout: %v", err)
	}

	now := time.Now().UTC()

	eventID, err := eventService.Create(
		ctx,
		adminUserID,
		eventsvc.CreateInput{
			VenueID:      venueID,
			Name:         "M4 HTTP Event",
			StartsAt:     timePtr(now.Add(48 * time.Hour)),
			SalesOpenAt:  timePtr(now.Add(-time.Hour)),
			SalesCloseAt: timePtr(now.Add(47 * time.Hour)),
			TimezoneName: "UTC",
		},
	)
	if err != nil {
		t.Fatalf("create Event: %v", err)
	}

	if err := eventService.MaterializeLayout(
		ctx,
		adminUserID,
		eventID,
		layoutID,
	); err != nil {
		t.Fatalf("materialize layout: %v", err)
	}

	priceTierID, err :=
		eventService.CreatePriceTier(
			ctx,
			adminUserID,
			eventID,
			eventsvc.PriceTierInput{
				Code:        "STANDARD",
				Name:        "Standard",
				AmountMinor: 10000,
				Currency:    "NGN",
			},
		)
	if err != nil {
		t.Fatalf("create price tier: %v", err)
	}

	if err := eventService.AssignPricing(
		ctx,
		adminUserID,
		eventID,
		eventsvc.PricingAssignmentInput{
			PriceTierID:       priceTierID,
			SectionObjectKeys: []string{"reserved"},
			GAPoolObjectKeys:  []string{"ga-main"},
		},
	); err != nil {
		t.Fatalf("assign pricing: %v", err)
	}

	if err :=
		eventService.ConfigureTransactionPolicy(
			ctx,
			adminUserID,
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
		t.Fatalf("configure policy: %v", err)
	}

	if err := eventService.OpenSales(
		ctx,
		adminUserID,
		eventID,
	); err != nil {
		t.Fatalf("open sales: %v", err)
	}

	partnerA, err := partnerService.Create(
		ctx,
		adminUserID,
		"M4 HTTP Partner A "+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("create Partner A: %v", err)
	}

	partnerB, err := partnerService.Create(
		ctx,
		adminUserID,
		"M4 HTTP Partner B "+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("create Partner B: %v", err)
	}

	_, credentialA, err :=
		partnerService.CreateCredential(
			ctx,
			adminUserID,
			partnerA,
		)
	if err != nil {
		t.Fatalf("credential A: %v", err)
	}

	_, credentialB, err :=
		partnerService.CreateCredential(
			ctx,
			adminUserID,
			partnerB,
		)
	if err != nil {
		t.Fatalf("credential B: %v", err)
	}

	for _, partnerID := range []uuid.UUID{
		partnerA,
		partnerB,
	} {
		if err :=
			partnerService.GrantEventAccess(
				ctx,
				adminUserID,
				eventID,
				partnerID,
			); err != nil {
			t.Fatalf("grant Event access: %v", err)
		}
	}

	var (
		seatID uuid.UUID
		gaID   uuid.UUID
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM reserved_inventory_units
			WHERE event_id = $1
			  AND snapshot_object_key =
			      'seat-a1'
		`,
		eventID,
	).Scan(&seatID); err != nil {
		t.Fatalf("seat ID: %v", err)
	}

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
	).Scan(&gaID); err != nil {
		t.Fatalf("GA ID: %v", err)
	}

	adminHandler, err := adminapi.New(
		adminapi.Dependencies{
			Database:     pool,
			Transactions: runner,
			HumanAuth: func(
				_ context.Context,
				token string,
			) (auth.HumanPrincipal, error) {
				if token != "admin-token" {
					return auth.HumanPrincipal{},
						io.EOF
				}

				return auth.HumanPrincipal{
					Provider: "m4-http",
					Subject:  adminSubject,
				}, nil
			},
			VenueService:      venueService,
			EventService:      eventService,
			PartnerService:    partnerService,
			AllocationService: allocationService,
		},
	)
	if err != nil {
		t.Fatalf("admin handler: %v", err)
	}

	partnerHandler, err := partnerapi.New(
		partnerapi.Dependencies{
			Database: pool,
			PartnerAuth: auth.NewPartnerAuthenticator(
				pool,
			),
			Availability: inventory.NewService(pool),
		},
	)
	if err != nil {
		t.Fatalf("Partner handler: %v", err)
	}

	apiMux := http.NewServeMux()

	apiMux.Handle(
		"/api/v1/admin/",
		adminHandler,
	)

	apiMux.Handle(
		"/api/v1/partner/",
		partnerHandler,
	)

	handler := httpserver.Handler(
		slog.New(
			slog.NewTextHandler(
				io.Discard,
				nil,
			),
		),
		pool,
		apiMux,
	)

	eventPublicID := publicid.Encode(
		publicid.Event,
		eventID,
	)

	seatPublicID := publicid.Encode(
		publicid.ReservedInventory,
		seatID,
	)

	gaPublicID := publicid.Encode(
		publicid.GAPool,
		gaID,
	)

	blockKey := uuid.NewString()

	blockBody := map[string]any{
		"purpose":                "HOUSE",
		"reserved_inventory_ids": []string{seatPublicID},
		"ga_targets": []map[string]any{
			{
				"inventory_id": gaPublicID,
				"quantity":     5,
			},
		},
	}

	blockFirst := adminRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/events/"+
			eventPublicID+
			"/blocks",
		blockKey,
		blockBody,
	)

	if blockFirst.Code != http.StatusCreated {
		t.Fatalf(
			"create Block status=%d body=%s",
			blockFirst.Code,
			blockFirst.Body.String(),
		)
	}

	blockReplay := adminRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/events/"+
			eventPublicID+
			"/blocks",
		blockKey,
		blockBody,
	)

	if blockReplay.Code != http.StatusCreated {
		t.Fatalf(
			"Block replay status=%d want=%d body=%s",
			blockReplay.Code,
			http.StatusCreated,
			blockReplay.Body.String(),
		)
	}

	var firstBlockLogical any
	if err := json.Unmarshal(
		blockFirst.Body.Bytes(),
		&firstBlockLogical,
	); err != nil {
		t.Fatalf(
			"decode first Block response: %v",
			err,
		)
	}

	var replayBlockLogical any
	if err := json.Unmarshal(
		blockReplay.Body.Bytes(),
		&replayBlockLogical,
	); err != nil {
		t.Fatalf(
			"decode replayed Block response: %v",
			err,
		)
	}

	firstBlockCanonical, err := json.Marshal(
		firstBlockLogical,
	)
	if err != nil {
		t.Fatalf(
			"canonicalize first Block response: %v",
			err,
		)
	}

	replayBlockCanonical, err := json.Marshal(
		replayBlockLogical,
	)
	if err != nil {
		t.Fatalf(
			"canonicalize replayed Block response: %v",
			err,
		)
	}

	if !bytes.Equal(
		firstBlockCanonical,
		replayBlockCanonical,
	) {
		t.Fatalf(
			"Block idempotent replay changed logical response: first=%s replay=%s",
			firstBlockCanonical,
			replayBlockCanonical,
		)
	}

	var blockResponse struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(
		blockFirst.Body.Bytes(),
		&blockResponse,
	); err != nil {
		t.Fatalf("decode Block: %v", err)
	}

	releaseBlock := adminRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/blocks/"+
			blockResponse.ID+
			"/release",
		uuid.NewString(),
		struct{}{},
	)

	if releaseBlock.Code != http.StatusOK {
		t.Fatalf(
			"release Block status=%d body=%s",
			releaseBlock.Code,
			releaseBlock.Body.String(),
		)
	}

	allocationResponse :=
		adminRequest(
			t,
			handler,
			http.MethodPost,
			"/api/v1/admin/events/"+
				eventPublicID+
				"/allocations",
			uuid.NewString(),
			map[string]any{
				"mode": "CHANNEL",
				"partner_id": publicid.Encode(
					publicid.Partner,
					partnerA,
				),
				"purpose": "CHANNEL",
				"release_destination": map[string]any{
					"kind": "SHARED",
				},
				"reserved_inventory_ids": []string{
					seatPublicID,
				},
				"ga_targets": []map[string]any{
					{
						"inventory_id": gaPublicID,
						"quantity":     10,
					},
				},
			},
		)

	if allocationResponse.Code !=
		http.StatusCreated {
		t.Fatalf(
			"create Allocation status=%d body=%s",
			allocationResponse.Code,
			allocationResponse.Body.String(),
		)
	}

	var allocationBody struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(
		allocationResponse.Body.Bytes(),
		&allocationBody,
	); err != nil {
		t.Fatalf("decode Allocation: %v", err)
	}

	eventRead := partnerRequest(
		t,
		handler,
		"/api/v1/partner/events/"+
			eventPublicID,
		credentialA,
	)

	if eventRead.Code != http.StatusOK {
		t.Fatalf(
			"Partner Event read status=%d body=%s",
			eventRead.Code,
			eventRead.Body.String(),
		)
	}

	layoutRead := partnerRequest(
		t,
		handler,
		"/api/v1/partner/events/"+
			eventPublicID+
			"/layout",
		credentialA,
	)

	if layoutRead.Code != http.StatusOK {
		t.Fatalf(
			"Partner layout status=%d body=%s",
			layoutRead.Code,
			layoutRead.Body.String(),
		)
	}

	if strings.Contains(
		layoutRead.Body.String(),
		layoutID.String(),
	) {
		t.Fatal(
			"Partner layout leaked Venue layout UUID",
		)
	}

	availabilityA := partnerRequest(
		t,
		handler,
		"/api/v1/partner/events/"+
			eventPublicID+
			"/availability",
		credentialA,
	)

	availabilityB := partnerRequest(
		t,
		handler,
		"/api/v1/partner/events/"+
			eventPublicID+
			"/availability",
		credentialB,
	)

	if availabilityA.Code != http.StatusOK ||
		availabilityB.Code != http.StatusOK {
		t.Fatalf(
			"availability status A=%d B=%d",
			availabilityA.Code,
			availabilityB.Code,
		)
	}

	assertPartnerAvailability(
		t,
		availabilityA.Body.Bytes(),
		seatPublicID,
		"AVAILABLE",
		2,
	)

	assertPartnerAvailability(
		t,
		availabilityB.Body.Bytes(),
		seatPublicID,
		"UNAVAILABLE",
		1,
	)

	if strings.Contains(
		availabilityA.Body.String(),
		allocationBody.ID,
	) {
		t.Fatal(
			"Partner availability leaked Allocation identity",
		)
	}

	reclassify := adminRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/allocations/"+
			allocationBody.ID+
			"/reclassify",
		uuid.NewString(),
		map[string]any{
			"mode": "NON_PUBLIC",
		},
	)

	if reclassify.Code != http.StatusOK {
		t.Fatalf(
			"reclassify status=%d body=%s",
			reclassify.Code,
			reclassify.Body.String(),
		)
	}

	availabilityA =
		partnerRequest(
			t,
			handler,
			"/api/v1/partner/events/"+
				eventPublicID+
				"/availability",
			credentialA,
		)

	assertPartnerAvailability(
		t,
		availabilityA.Body.Bytes(),
		seatPublicID,
		"UNAVAILABLE",
		1,
	)

	releaseAllocation := adminRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/allocations/"+
			allocationBody.ID+
			"/release",
		uuid.NewString(),
		struct{}{},
	)

	if releaseAllocation.Code != http.StatusOK {
		t.Fatalf(
			"release Allocation status=%d body=%s",
			releaseAllocation.Code,
			releaseAllocation.Body.String(),
		)
	}

	availabilityA =
		partnerRequest(
			t,
			handler,
			"/api/v1/partner/events/"+
				eventPublicID+
				"/availability",
			credentialA,
		)

	assertPartnerAvailability(
		t,
		availabilityA.Body.Bytes(),
		seatPublicID,
		"AVAILABLE",
		1,
	)
}

func adminRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	idempotencyKey string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal admin body: %v", err)
	}

	request := httptest.NewRequest(
		method,
		path,
		bytes.NewReader(raw),
	)

	request.Header.Set(
		"Authorization",
		"Bearer admin-token",
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	request.Header.Set(
		"Idempotency-Key",
		idempotencyKey,
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func partnerRequest(
	t *testing.T,
	handler http.Handler,
	path string,
	credential string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(
		http.MethodGet,
		path,
		nil,
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+credential,
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}

func assertPartnerAvailability(
	t *testing.T,
	raw []byte,
	reservedID string,
	expectedSellability string,
	expectedGAOffers int,
) {
	t.Helper()

	var body struct {
		ReservedUnits []struct {
			InventoryID string `json:"inventory_id"`
			Sellability string `json:"sellability"`
		} `json:"reserved_units"`
		GAPools []struct {
			Offers []json.RawMessage `json:"offers"`
		} `json:"ga_pools"`
	}

	if err := json.Unmarshal(
		raw,
		&body,
	); err != nil {
		t.Fatalf(
			"decode availability: %v",
			err,
		)
	}

	found := false

	for _, item := range body.ReservedUnits {
		if item.InventoryID == reservedID {
			found = true

			if item.Sellability !=
				expectedSellability {
				t.Fatalf(
					"reserved sellability=%s want=%s",
					item.Sellability,
					expectedSellability,
				)
			}
		}
	}

	if !found {
		t.Fatalf(
			"reserved inventory %s missing",
			reservedID,
		)
	}

	if len(body.GAPools) != 1 {
		t.Fatalf(
			"GA pool count=%d want=1",
			len(body.GAPools),
		)
	}

	if len(body.GAPools[0].Offers) !=
		expectedGAOffers {
		t.Fatalf(
			"GA offer count=%d want=%d",
			len(body.GAPools[0].Offers),
			expectedGAOffers,
		)
	}
}

func timePtr(
	value time.Time,
) *time.Time {
	return &value
}
