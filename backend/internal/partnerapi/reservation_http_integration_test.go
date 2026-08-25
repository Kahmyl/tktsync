//go:build integration

package partnerapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/partnerapi"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

type reservationAvailabilityResponse struct {
	ReservedUnits []struct {
		Offer *struct {
			OfferID string `json:"offer_id"`
		} `json:"offer"`
	} `json:"reserved_units"`
	GAPools []struct {
		Offers []struct {
			OfferID string `json:"offer_id"`
		} `json:"offers"`
	} `json:"ga_pools"`
}

type reservationReservationItemResponse struct {
	ID              string `json:"id"`
	InventoryKind   string `json:"inventory_kind"`
	Quantity        int    `json:"quantity"`
	UnitAmountMinor int64  `json:"unit_amount_minor"`
	Currency        string `json:"currency"`
}

type reservationReservationResponse struct {
	ID       string                               `json:"id"`
	EventID  string                               `json:"event_id"`
	Status   string                               `json:"status"`
	Currency string                               `json:"currency"`
	Items    []reservationReservationItemResponse `json:"items"`
}

type reservationCreateReservationResponse struct {
	Reservation      reservationReservationResponse `json:"reservation"`
	ReservationToken string                         `json:"reservation_token"`
}

type reservationCheckoutResponse struct {
	ReservationID   string `json:"reservation_id"`
	Status          string `json:"status"`
	CheckoutAttempt struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"checkout_attempt"`
}

func TestPartnerReservationHTTP(
	t *testing.T,
) {
	databaseURL := os.Getenv(
		"DATABASE_URL",
	)
	if databaseURL == "" {
		t.Fatal(
			"DATABASE_URL is required",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

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
	defer pool.Close()

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
				'reservation-http',
				$2,
				'Reservation HTTP Admin',
				'ACTIVE',
				clock_timestamp(),
				clock_timestamp()
			)
		`,
		userID,
		subject,
	); err != nil {
		t.Fatalf(
			"create app user: %v",
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
			"create role: %v",
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

	venueID, err :=
		venueService.CreateVenue(
			ctx,
			userID,
			venuesvc.CreateVenueInput{
				Name: "Reservation HTTP Venue " +
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

	gaCapacity := 30

	if err :=
		venueService.ReplaceDraftLayout(
			ctx,
			userID,
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
				Name: "Reservation HTTP Event " +
					uuid.NewString(),
				StartsAt: reservationTimePtr(
					now.Add(
						48 * time.Hour,
					),
				),
				SalesOpenAt: reservationTimePtr(
					now.Add(
						-time.Hour,
					),
				),
				SalesCloseAt: reservationTimePtr(
					now.Add(
						47 * time.Hour,
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

	priceTierID, err :=
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
				PriceTierID: priceTierID,
				SectionObjectKeys: []string{
					"reserved",
				},
				GAPoolObjectKeys: []string{
					"ga-main",
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

	partnerA, err :=
		partnerService.Create(
			ctx,
			userID,
			"Reservation HTTP Partner A "+
				uuid.NewString(),
		)
	if err != nil {
		t.Fatalf(
			"create Partner A: %v",
			err,
		)
	}

	partnerB, err :=
		partnerService.Create(
			ctx,
			userID,
			"Reservation HTTP Partner B "+
				uuid.NewString(),
		)
	if err != nil {
		t.Fatalf(
			"create Partner B: %v",
			err,
		)
	}

	_, credentialA, err :=
		partnerService.CreateCredential(
			ctx,
			userID,
			partnerA,
		)
	if err != nil {
		t.Fatalf(
			"credential A: %v",
			err,
		)
	}

	_, credentialB, err :=
		partnerService.CreateCredential(
			ctx,
			userID,
			partnerB,
		)
	if err != nil {
		t.Fatalf(
			"credential B: %v",
			err,
		)
	}

	for _, partnerID := range []uuid.UUID{
		partnerA,
		partnerB,
	} {
		if err :=
			partnerService.GrantEventAccess(
				ctx,
				userID,
				eventID,
				partnerID,
			); err != nil {
			t.Fatalf(
				"grant Event access: %v",
				err,
			)
		}
	}

	keyBytes := make(
		[]byte,
		32,
	)

	for index := range keyBytes {
		keyBytes[index] =
			byte(index + 1)
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
			"create Reservation HMAC keyring: %v",
			err,
		)
	}

	reservationService :=
		reservation.NewService(
			runner,
			keyring,
			keyring,
		)

	partnerHandler, err :=
		partnerapi.New(
			partnerapi.Dependencies{
				Database: pool,
				PartnerAuth: auth.
					NewPartnerAuthenticator(
						pool,
					),
				Availability: inventory.NewService(
					pool,
				),
				Transactions:          runner,
				Reservation:           reservationService,
				TicketQRPublicBaseURL: "https://tickets.test",
			},
		)
	if err != nil {
		t.Fatalf(
			"Partner handler: %v",
			err,
		)
	}

	apiMux := http.NewServeMux()

	apiMux.Handle(
		"/api/v1/partner/",
		partnerHandler,
	)
	apiMux.Handle(
		"/api/v1/ticket-qr/",
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

	eventPublicID :=
		publicid.Encode(
			publicid.Event,
			eventID,
		)

	availabilityResponse :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/events/"+
				eventPublicID+
				"/availability",
			credentialA,
			"",
			"",
			nil,
			"",
		)

	if availabilityResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"availability status=%d body=%s",
			availabilityResponse.Code,
			availabilityResponse.Body.String(),
		)
	}

	var availability reservationAvailabilityResponse

	if err := json.Unmarshal(
		availabilityResponse.Body.Bytes(),
		&availability,
	); err != nil {
		t.Fatalf(
			"decode availability: %v",
			err,
		)
	}

	if len(availability.ReservedUnits) != 1 ||
		availability.ReservedUnits[0].
			Offer == nil ||
		availability.ReservedUnits[0].
			Offer.OfferID == "" {
		t.Fatalf(
			"Reserved offer missing: %s",
			availabilityResponse.Body.String(),
		)
	}

	if len(availability.GAPools) != 1 ||
		len(availability.GAPools[0].
			Offers) == 0 ||
		availability.GAPools[0].
			Offers[0].OfferID == "" {
		t.Fatalf(
			"GA offer missing: %s",
			availabilityResponse.Body.String(),
		)
	}

	reservedOffer :=
		availability.ReservedUnits[0].
			Offer.OfferID

	gaOffer :=
		availability.GAPools[0].
			Offers[0].OfferID

	createBody := map[string]any{
		"event_id":             eventPublicID,
		"partner_customer_ref": "customer-1",
		"partner_order_ref":    "order-1",
		"buyer_session_ref":    "browser-1",
		"items": []map[string]any{
			{
				"offer_id": reservedOffer,
				"quantity": 1,
			},
			{
				"offer_id": gaOffer,
				"quantity": 2,
			},
		},
	}

	createKey := uuid.NewString()
	requestID := uuid.NewString()

	firstCreate :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations",
			credentialA,
			createKey,
			"",
			createBody,
			requestID,
		)

	if firstCreate.Code !=
		http.StatusCreated {
		t.Fatalf(
			"create Reservation status=%d body=%s",
			firstCreate.Code,
			firstCreate.Body.String(),
		)
	}

	if firstCreate.Header().Get(
		"Cache-Control",
	) != "no-store" {
		t.Fatalf(
			"create Reservation Cache-Control=%q want=no-store",
			firstCreate.Header().Get(
				"Cache-Control",
			),
		)
	}

	if firstCreate.Header().Get(
		"X-Request-ID",
	) != requestID {
		t.Fatalf(
			"X-Request-ID=%q want=%q",
			firstCreate.Header().Get(
				"X-Request-ID",
			),
			requestID,
		)
	}

	var created reservationCreateReservationResponse

	if err := json.Unmarshal(
		firstCreate.Body.Bytes(),
		&created,
	); err != nil {
		t.Fatalf(
			"decode create Reservation: %v",
			err,
		)
	}

	if created.Reservation.ID == "" ||
		created.Reservation.Status !=
			"HELD" ||
		created.ReservationToken == "" {
		t.Fatalf(
			"invalid create Reservation response: %s",
			firstCreate.Body.String(),
		)
	}

	if len(created.Reservation.Items) != 2 {
		t.Fatalf(
			"create Reservation items=%d want=2 body=%s",
			len(created.Reservation.Items),
			firstCreate.Body.String(),
		)
	}

	replayCreate :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations",
			credentialA,
			createKey,
			"",
			createBody,
			uuid.NewString(),
		)

	if replayCreate.Code !=
		http.StatusCreated {
		t.Fatalf(
			"create replay status=%d body=%s",
			replayCreate.Code,
			replayCreate.Body.String(),
		)
	}

	var firstLogical any
	var replayLogical any

	if err := json.Unmarshal(
		firstCreate.Body.Bytes(),
		&firstLogical,
	); err != nil {
		t.Fatalf(
			"decode first logical response: %v",
			err,
		)
	}

	if err := json.Unmarshal(
		replayCreate.Body.Bytes(),
		&replayLogical,
	); err != nil {
		t.Fatalf(
			"decode replay logical response: %v",
			err,
		)
	}

	if !reflect.DeepEqual(
		firstLogical,
		replayLogical,
	) {
		t.Fatalf(
			"idempotent create replay changed logical result: first=%s replay=%s",
			firstCreate.Body.String(),
			replayCreate.Body.String(),
		)
	}

	conflictBody := map[string]any{
		"event_id":             eventPublicID,
		"partner_customer_ref": "customer-1",
		"partner_order_ref":    "changed-order",
		"buyer_session_ref":    "browser-1",
		"items": []map[string]any{
			{
				"offer_id": reservedOffer,
				"quantity": 1,
			},
			{
				"offer_id": gaOffer,
				"quantity": 2,
			},
		},
	}

	conflict :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations",
			credentialA,
			createKey,
			"",
			conflictBody,
			"",
		)

	reservationRequireErrorCode(
		t,
		conflict,
		"IDEMPOTENCY_CONFLICT",
	)

	getOwn :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID,
			credentialA,
			"",
			"",
			nil,
			"",
		)

	if getOwn.Code !=
		http.StatusOK {
		t.Fatalf(
			"GET own Reservation status=%d body=%s",
			getOwn.Code,
			getOwn.Body.String(),
		)
	}

	if bytes.Contains(
		getOwn.Body.Bytes(),
		[]byte(
			"reservation_token",
		),
	) {
		t.Fatalf(
			"GET Reservation leaked continuation token: %s",
			getOwn.Body.String(),
		)
	}

	getOther :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID,
			credentialB,
			"",
			"",
			nil,
			"",
		)

	reservationRequireErrorCode(
		t,
		getOther,
		"RESOURCE_NOT_FOUND",
	)

	var (
		reservedItemID    string
		gaItemID          string
		gaUnitAmountMinor int64
		gaCurrency        string
	)

	for _, item := range created.Reservation.Items {
		switch item.InventoryKind {
		case "RESERVED":
			reservedItemID = item.ID

		case "GA":
			gaItemID = item.ID
			gaUnitAmountMinor = item.UnitAmountMinor
			gaCurrency = item.Currency
		}
	}

	if reservedItemID == "" ||
		gaItemID == "" {
		t.Fatalf(
			"Reservation item public identities missing: %s",
			firstCreate.Body.String(),
		)
	}

	missingTokenPatch :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPatch,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID,
			credentialA,
			uuid.NewString(),
			"",
			map[string]any{
				"remove_item_ids": []string{
					reservedItemID,
				},
			},
			"",
		)

	reservationRequireErrorCode(
		t,
		missingTokenPatch,
		"HOLD_NOT_OWNED",
	)

	modifyKey := uuid.NewString()

	modified :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPatch,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID,
			credentialA,
			modifyKey,
			created.ReservationToken,
			map[string]any{
				"remove_item_ids": []string{
					reservedItemID,
				},
				"adjust_quantities": []map[string]any{
					{
						"reservation_item_id": gaItemID,
						"new_quantity":        1,
					},
				},
			},
			"",
		)

	if modified.Code != http.StatusOK {
		t.Fatalf(
			"modify Reservation status=%d body=%s",
			modified.Code,
			modified.Body.String(),
		)
	}

	var modifiedReservation reservationReservationResponse

	if err := json.Unmarshal(
		modified.Body.Bytes(),
		&modifiedReservation,
	); err != nil {
		t.Fatalf(
			"decode modified Reservation: %v",
			err,
		)
	}

	if len(modifiedReservation.Items) != 1 {
		t.Fatalf(
			"modified active items=%d want=1 body=%s",
			len(modifiedReservation.Items),
			modified.Body.String(),
		)
	}

	if modifiedReservation.Items[0].ID ==
		gaItemID {
		t.Fatalf(
			"GA quantity decrease mutated the immutable ReservationItem instead of append-preserving history",
		)
	}

	if modifiedReservation.Items[0].Quantity !=
		1 {
		t.Fatalf(
			"replacement GA quantity=%d want=1",
			modifiedReservation.Items[0].Quantity,
		)
	}

	if modifiedReservation.Items[0].
		UnitAmountMinor !=
		gaUnitAmountMinor ||
		modifiedReservation.Items[0].
			Currency != gaCurrency {
		t.Fatalf(
			"replacement GA price snapshot changed: got=%d/%s want=%d/%s",
			modifiedReservation.Items[0].
				UnitAmountMinor,
			modifiedReservation.Items[0].
				Currency,
			gaUnitAmountMinor,
			gaCurrency,
		)
	}

	oldGAItemUUID, err :=
		publicid.Parse(
			gaItemID,
			publicid.ReservationItem,
		)
	if err != nil {
		t.Fatalf(
			"parse original GA ReservationItem: %v",
			err,
		)
	}

	var oldRemoved bool

	if err := pool.QueryRow(
		ctx,
		`
			SELECT removed_at IS NOT NULL
			FROM reservation_items
			WHERE id = $1
		`,
		oldGAItemUUID,
	).Scan(&oldRemoved); err != nil {
		t.Fatalf(
			"read original GA ReservationItem history: %v",
			err,
		)
	}

	if !oldRemoved {
		t.Fatal(
			"original GA ReservationItem was not append-preserved with removed_at",
		)
	}

	modifyReplay :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPatch,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID,
			credentialA,
			modifyKey,
			created.ReservationToken,
			map[string]any{
				"remove_item_ids": []string{
					reservedItemID,
				},
				"adjust_quantities": []map[string]any{
					{
						"reservation_item_id": gaItemID,
						"new_quantity":        1,
					},
				},
			},
			"",
		)

	if modifyReplay.Code !=
		http.StatusOK {
		t.Fatalf(
			"modify replay status=%d body=%s",
			modifyReplay.Code,
			modifyReplay.Body.String(),
		)
	}

	var modifiedLogical any
	var modifiedReplayLogical any

	if err := json.Unmarshal(
		modified.Body.Bytes(),
		&modifiedLogical,
	); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		modifyReplay.Body.Bytes(),
		&modifiedReplayLogical,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		modifiedLogical,
		modifiedReplayLogical,
	) {
		t.Fatalf(
			"modify replay changed logical result: first=%s replay=%s",
			modified.Body.String(),
			modifyReplay.Body.String(),
		)
	}

	checkout :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/checkout",
			credentialA,
			uuid.NewString(),
			created.ReservationToken,
			struct{}{},
			"",
		)

	if checkout.Code != http.StatusOK {
		t.Fatalf(
			"begin checkout status=%d body=%s",
			checkout.Code,
			checkout.Body.String(),
		)
	}

	var checkoutBody reservationCheckoutResponse

	if err := json.Unmarshal(
		checkout.Body.Bytes(),
		&checkoutBody,
	); err != nil {
		t.Fatalf(
			"decode checkout: %v",
			err,
		)
	}

	if checkoutBody.Status !=
		"COMMITTING" ||
		checkoutBody.CheckoutAttempt.ID ==
			"" ||
		checkoutBody.CheckoutAttempt.Status !=
			"ACTIVE" {
		t.Fatalf(
			"invalid checkout response: %s",
			checkout.Body.String(),
		)
	}

	secondCheckout :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/checkout",
			credentialA,
			uuid.NewString(),
			created.ReservationToken,
			struct{}{},
			"",
		)

	reservationRequireErrorCode(
		t,
		secondCheckout,
		"CHECKOUT_ALREADY_ACTIVE",
	)

	unsafeRelease :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/release",
			credentialA,
			uuid.NewString(),
			created.ReservationToken,
			struct{}{},
			"",
		)

	reservationRequireErrorCode(
		t,
		unsafeRelease,
		"PAYMENT_STATUS_UNCERTAIN",
	)

	paymentFailureKey :=
		uuid.NewString()

	retryResponse :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/payment-failure",
			credentialA,
			paymentFailureKey,
			created.ReservationToken,
			map[string]any{
				"checkout_attempt_id": checkoutBody.
					CheckoutAttempt.ID,
				"partner_payment_ref":   "PAY-1",
				"failure_code":          "CARD_DECLINED",
				"requested_disposition": "RETRY",
			},
			"",
		)

	if retryResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"payment failure RETRY status=%d body=%s",
			retryResponse.Code,
			retryResponse.Body.String(),
		)
	}

	var retryBody struct {
		Status string `json:"status"`
	}

	if err := json.Unmarshal(
		retryResponse.Body.Bytes(),
		&retryBody,
	); err != nil {
		t.Fatal(err)
	}

	if retryBody.Status !=
		"PAYMENT_RETRY" {
		t.Fatalf(
			"payment failure RETRY state=%s body=%s",
			retryBody.Status,
			retryResponse.Body.String(),
		)
	}

	retryReplay :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/payment-failure",
			credentialA,
			paymentFailureKey,
			created.ReservationToken,
			map[string]any{
				"checkout_attempt_id": checkoutBody.
					CheckoutAttempt.ID,
				"partner_payment_ref":   "PAY-1",
				"failure_code":          "CARD_DECLINED",
				"requested_disposition": "RETRY",
			},
			"",
		)

	if retryReplay.Code !=
		http.StatusOK {
		t.Fatalf(
			"payment failure replay status=%d body=%s",
			retryReplay.Code,
			retryReplay.Body.String(),
		)
	}

	retryCheckout :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/checkout",
			credentialA,
			uuid.NewString(),
			created.ReservationToken,
			struct{}{},
			"",
		)

	if retryCheckout.Code !=
		http.StatusOK {
		t.Fatalf(
			"retry checkout status=%d body=%s",
			retryCheckout.Code,
			retryCheckout.Body.String(),
		)
	}

	var retryCheckoutBody reservationCheckoutResponse

	if err := json.Unmarshal(
		retryCheckout.Body.Bytes(),
		&retryCheckoutBody,
	); err != nil {
		t.Fatal(err)
	}

	if retryCheckoutBody.
		CheckoutAttempt.ID ==
		checkoutBody.CheckoutAttempt.ID {
		t.Fatalf(
			"retry checkout reused CheckoutAttempt identity",
		)
	}

	releaseAfterFailure :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/payment-failure",
			credentialA,
			uuid.NewString(),
			created.ReservationToken,
			map[string]any{
				"checkout_attempt_id": retryCheckoutBody.
					CheckoutAttempt.ID,
				"partner_payment_ref":   "PAY-2",
				"failure_code":          "CARD_DECLINED",
				"requested_disposition": "RELEASE",
			},
			"",
		)

	if releaseAfterFailure.Code !=
		http.StatusOK {
		t.Fatalf(
			"payment failure RELEASE status=%d body=%s",
			releaseAfterFailure.Code,
			releaseAfterFailure.Body.String(),
		)
	}

	var releaseBody struct {
		Status string `json:"status"`
	}

	if err := json.Unmarshal(
		releaseAfterFailure.Body.Bytes(),
		&releaseBody,
	); err != nil {
		t.Fatal(err)
	}

	if releaseBody.Status !=
		"RELEASED" {
		t.Fatalf(
			"payment failure RELEASE state=%s body=%s",
			releaseBody.Status,
			releaseAfterFailure.Body.String(),
		)
	}

	finalGet :=
		reservationPartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID,
			credentialA,
			"",
			"",
			nil,
			"",
		)

	if finalGet.Code !=
		http.StatusOK {
		t.Fatalf(
			"final GET status=%d body=%s",
			finalGet.Code,
			finalGet.Body.String(),
		)
	}

	var finalReservation reservationReservationResponse

	if err := json.Unmarshal(
		finalGet.Body.Bytes(),
		&finalReservation,
	); err != nil {
		t.Fatal(err)
	}

	if finalReservation.Status !=
		"RELEASED" {
		t.Fatalf(
			"final Reservation state=%s want=RELEASED body=%s",
			finalReservation.Status,
			finalGet.Body.String(),
		)
	}

	t.Run(
		"TicketingConfirmationHTTP",
		func(t *testing.T) {
			ticketingAssertPartnerConfirmationHTTP(
				t,
				handler,
				pool,
				credentialA,
				credentialB,
				eventPublicID,
			)
		},
	)
}

func reservationPartnerHTTP(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	credential string,
	idempotencyKey string,
	reservationToken string,
	body any,
	requestID string,
) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader

	if body != nil {
		raw, err := json.Marshal(
			body,
		)
		if err != nil {
			t.Fatalf(
				"marshal request: %v",
				err,
			)
		}

		reader = bytes.NewReader(raw)
	}

	request := httptest.NewRequest(
		method,
		path,
		reader,
	)

	request.Header.Set(
		"Authorization",
		"Bearer "+credential,
	)

	if body != nil {
		request.Header.Set(
			"Content-Type",
			"application/json",
		)
	}

	if idempotencyKey != "" {
		request.Header.Set(
			"Idempotency-Key",
			idempotencyKey,
		)
	}

	if reservationToken != "" {
		request.Header.Set(
			"X-TktSync-Reservation-Token",
			reservationToken,
		)
	}

	if requestID != "" {
		request.Header.Set(
			"X-Request-ID",
			requestID,
		)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		request,
	)

	return response
}

func reservationRequireErrorCode(
	t *testing.T,
	response *httptest.ResponseRecorder,
	expected string,
) {
	t.Helper()

	if response.Code >= 200 &&
		response.Code < 300 {
		t.Fatalf(
			"expected error %s but status=%d body=%s",
			expected,
			response.Code,
			response.Body.String(),
		)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(
		response.Body.Bytes(),
		&body,
	); err != nil {
		t.Fatalf(
			"decode error response: %v body=%s",
			err,
			response.Body.String(),
		)
	}

	if body.Error.Code != expected {
		t.Fatalf(
			"error code=%s want=%s status=%d body=%s",
			body.Error.Code,
			expected,
			response.Code,
			response.Body.String(),
		)
	}
}

func reservationTimePtr(
	value time.Time,
) *time.Time {
	return &value
}
