//go:build integration

package allocation_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/adminapi"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func TestM6NonPublicIssuance(
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
			"create QR keyring: %v",
			err,
		)
	}

	venueService :=
		venuesvc.NewService(
			runner,
		)

	partnerService :=
		partnersvc.NewService(
			runner,
		)

	eventService :=
		eventsvc.NewService(
			runner,
		)

	allocationService :=
		allocsvc.NewService(
			runner,
			keyring,
		)

	reservationService :=
		reservation.NewService(
			runner,
			keyring,
			keyring,
		)

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
				'm6-issuance',
				$2,
				'M6 Issuance Admin',
				'ACTIVE',
				clock_timestamp(),
				clock_timestamp()
			)
		`,
		actorID,
		actorID.String(),
	); err != nil {
		t.Fatalf(
			"create issuance actor: %v",
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
		actorID,
	); err != nil {
		t.Fatalf(
			"create issuance admin role: %v",
			err,
		)
	}

	venueID, err :=
		venueService.CreateVenue(
			ctx,
			actorID,
			venuesvc.CreateVenueInput{
				Name: "M6 Issuance Venue " +
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
			actorID,
			venueID,
		)
	if err != nil {
		t.Fatalf(
			"create layout: %v",
			err,
		)
	}

	gaCapacity := 10

	if err :=
		venueService.ReplaceDraftLayout(
			ctx,
			actorID,
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
			actorID,
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
			actorID,
			eventsvc.CreateInput{
				VenueID: venueID,
				Name: "M6 Issuance Event " +
					uuid.NewString(),
				StartsAt: m6TimePtr(
					now.Add(
						48 * time.Hour,
					),
				),
				SalesOpenAt: m6TimePtr(
					now.Add(
						-time.Hour,
					),
				),
				SalesCloseAt: m6TimePtr(
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
			actorID,
			eventID,
			layoutID,
		); err != nil {
		t.Fatalf(
			"materialize layout: %v",
			err,
		)
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
	).Scan(
		&seatID,
	); err != nil {
		t.Fatalf(
			"lookup seat: %v",
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
			      'ga-main'
		`,
		eventID,
	).Scan(
		&gaID,
	); err != nil {
		t.Fatalf(
			"lookup GA pool: %v",
			err,
		)
	}

	allocationID, err :=
		allocationService.CreateAllocation(
			ctx,
			actorID,
			eventID,
			allocsvc.AllocationInput{
				Mode:                   "NON_PUBLIC",
				Purpose:                "VIP",
				Reason:                 "M6 issuance fixture",
				ReleaseDestinationKind: "SHARED",
				ReservedUnitIDs: []uuid.UUID{
					seatID,
				},
				GATargets: []allocsvc.GATarget{
					{
						PoolID:   gaID,
						Quantity: 3,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create NON_PUBLIC Allocation: %v",
			err,
		)
	}

	issued, err :=
		allocationService.IssueNonPublic(
			ctx,
			actorID,
			allocationID,
			allocsvc.NonPublicIssuanceInput{
				RecipientRef: "vip-group-1",
				Reason:       "Sponsor guest issuance",
				ReservedUnitIDs: []uuid.UUID{
					seatID,
				},
				GATargets: []allocsvc.GATarget{
					{
						PoolID:   gaID,
						Quantity: 2,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"issue NON_PUBLIC inventory: %v",
			err,
		)
	}

	if issued.IssuanceID ==
		uuid.Nil ||
		issued.EventID !=
			eventID ||
		issued.AllocationID !=
			allocationID ||
		issued.IssuedAt.IsZero() {
		t.Fatalf(
			"invalid issuance result: %+v",
			issued,
		)
	}

	if len(issued.Tickets) != 3 {
		t.Fatalf(
			"issuance tickets=%d want=3",
			len(issued.Tickets),
		)
	}

	for _, ticket := range issued.Tickets {
		if ticket.TicketID ==
			uuid.Nil ||
			ticket.CredentialID ==
				uuid.Nil ||
			ticket.State !=
				"ACTIVE" {
			t.Fatalf(
				"invalid issued Ticket: %+v",
				ticket,
			)
		}
	}

	var (
		issuanceCount     int
		issuanceItemCount int
		issuanceQuantity  int
		ticketCount       int
		credentialCount   int
		saleCount         int
		reservedState     string
		gaAvailable       int
		gaReserved        int
		gaSold            int
		gaIssued          int
		gaReleased        int
		auditCount        int
		outboxCount       int
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM non_public_issuances
			WHERE id = $1
			  AND event_id = $2
			  AND allocation_id = $3
		`,
		issued.IssuanceID,
		eventID,
		allocationID,
	).Scan(
		&issuanceCount,
	); err != nil {
		t.Fatalf(
			"count issuance: %v",
			err,
		)
	}

	if issuanceCount != 1 {
		t.Fatalf(
			"issuance count=%d want=1",
			issuanceCount,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*),
				COALESCE(
					SUM(quantity),
					0
				)
			FROM non_public_issuance_items
			WHERE issuance_id = $1
		`,
		issued.IssuanceID,
	).Scan(
		&issuanceItemCount,
		&issuanceQuantity,
	); err != nil {
		t.Fatalf(
			"count issuance items: %v",
			err,
		)
	}

	if issuanceItemCount != 2 ||
		issuanceQuantity != 3 {
		t.Fatalf(
			"issuance items=%d quantity=%d want=2/3",
			issuanceItemCount,
			issuanceQuantity,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM ticket_entitlements t
			JOIN non_public_issuance_items ni
			  ON ni.id =
			     t.origin_issuance_item_id
			WHERE ni.issuance_id = $1
			  AND t.origin_sale_item_id
			      IS NULL
			  AND t.status = 'ACTIVE'
			  AND t.replaces_ticket_entitlement_id
			      IS NULL
		`,
		issued.IssuanceID,
	).Scan(
		&ticketCount,
	); err != nil {
		t.Fatalf(
			"count issued Tickets: %v",
			err,
		)
	}

	if ticketCount != 3 {
		t.Fatalf(
			"issued Ticket count=%d want=3",
			ticketCount,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM qr_credentials q
			JOIN ticket_entitlements t
			  ON t.id =
			     q.ticket_entitlement_id
			JOIN non_public_issuance_items ni
			  ON ni.id =
			     t.origin_issuance_item_id
			WHERE ni.issuance_id = $1
			  AND q.status = 'ACTIVE'
		`,
		issued.IssuanceID,
	).Scan(
		&credentialCount,
	); err != nil {
		t.Fatalf(
			"count issuance credentials: %v",
			err,
		)
	}

	if credentialCount != 3 {
		t.Fatalf(
			"active QR count=%d want=3",
			credentialCount,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM sales
			WHERE event_id = $1
		`,
		eventID,
	).Scan(
		&saleCount,
	); err != nil {
		t.Fatalf(
			"count Sales: %v",
			err,
		)
	}

	if saleCount != 0 {
		t.Fatalf(
			"NON_PUBLIC issuance created %d Sales want=0",
			saleCount,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT disposition
			FROM v_reserved_inventory_current_state
			WHERE reserved_inventory_unit_id =
			      $1
		`,
		seatID,
	).Scan(
		&reservedState,
	); err != nil {
		t.Fatalf(
			"read reserved disposition: %v",
			err,
		)
	}

	if reservedState != "ISSUED" {
		t.Fatalf(
			"reserved disposition=%s want=ISSUED",
			reservedState,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				available_quantity,
				active_reserved_quantity,
				sold_current_quantity,
				issued_current_quantity,
				released_quantity
			FROM ga_allocation_buckets
			WHERE allocation_id = $1
			  AND ga_pool_id = $2
		`,
		allocationID,
		gaID,
	).Scan(
		&gaAvailable,
		&gaReserved,
		&gaSold,
		&gaIssued,
		&gaReleased,
	); err != nil {
		t.Fatalf(
			"read GA Allocation accounting: %v",
			err,
		)
	}

	if gaAvailable != 1 ||
		gaReserved != 0 ||
		gaSold != 0 ||
		gaIssued != 2 ||
		gaReleased != 0 {
		t.Fatalf(
			"GA Allocation available=%d reserved=%d sold=%d issued=%d released=%d want=1/0/0/2/0",
			gaAvailable,
			gaReserved,
			gaSold,
			gaIssued,
			gaReleased,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM audit_events
			WHERE entity_id = $1
			  AND operation =
			      'NON_PUBLIC_ISSUED'
		`,
		issued.IssuanceID,
	).Scan(
		&auditCount,
	); err != nil {
		t.Fatalf(
			"count issuance audit: %v",
			err,
		)
	}

	if auditCount != 1 {
		t.Fatalf(
			"issuance audit count=%d want=1",
			auditCount,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM outbox_events
			WHERE aggregate_id = $1
			  AND fact_type =
			      'ticket.non_public_issued'
		`,
		issued.IssuanceID,
	).Scan(
		&outboxCount,
	); err != nil {
		t.Fatalf(
			"count issuance outbox: %v",
			err,
		)
	}

	if outboxCount != 1 {
		t.Fatalf(
			"issuance outbox count=%d want=1",
			outboxCount,
		)
	}

	_, err =
		allocationService.IssueNonPublic(
			ctx,
			actorID,
			allocationID,
			allocsvc.NonPublicIssuanceInput{
				GATargets: []allocsvc.GATarget{
					{
						PoolID:   gaID,
						Quantity: 2,
					},
				},
			},
		)

	apiErr, ok :=
		apierror.As(err)

	if !ok ||
		apiErr.Code !=
			apierror.CodeInsufficientGAQuantity {
		t.Fatalf(
			"over-issuance error=%v want=INSUFFICIENT_GA_QUANTITY",
			err,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM non_public_issuances
			WHERE allocation_id = $1
		`,
		allocationID,
	).Scan(
		&issuanceCount,
	); err != nil {
		t.Fatalf(
			"recount issuance after failed over-issue: %v",
			err,
		)
	}

	if issuanceCount != 1 {
		t.Fatalf(
			"failed over-issuance partially committed; count=%d want=1",
			issuanceCount,
		)
	}

	httpAllocationID, err :=
		allocationService.CreateAllocation(
			ctx,
			actorID,
			eventID,
			allocsvc.AllocationInput{
				Mode:                   "NON_PUBLIC",
				Purpose:                "PRESS",
				Reason:                 "M6 HTTP issuance fixture",
				ReleaseDestinationKind: "SHARED",
				GATargets: []allocsvc.GATarget{
					{
						PoolID:   gaID,
						Quantity: 2,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create HTTP NON_PUBLIC Allocation: %v",
			err,
		)
	}

	adminHandler, err := adminapi.New(
		adminapi.Dependencies{
			Database:     pool,
			Transactions: runner,
			HumanAuth: func(
				_ context.Context,
				token string,
			) (
				auth.HumanPrincipal,
				error,
			) {
				if token != "m6-admin-token" {
					return auth.HumanPrincipal{},
						io.EOF
				}

				return auth.HumanPrincipal{
					Provider: "m6-issuance",
					Subject: actorID.
						String(),
				}, nil
			},
			VenueService:       venueService,
			EventService:       eventService,
			PartnerService:     partnerService,
			AllocationService:  allocationService,
			ReservationService: reservationService,
		},
	)
	if err != nil {
		t.Fatalf(
			"create M6 Admin handler: %v",
			err,
		)
	}

	adminTicketPublicID :=
		publicid.Encode(
			publicid.Ticket,
			issued.Tickets[0].TicketID,
		)

	adminOriginalCredentialID :=
		issued.Tickets[0].
			CredentialID

	adminReissueKey :=
		uuid.NewString()

	adminReissue :=
		m6AdminIssuanceRequest(
			t,
			adminHandler,
			"/api/v1/admin/tickets/"+
				adminTicketPublicID+
				"/credentials/reissue",
			adminReissueKey,
			struct{}{},
		)

	if adminReissue.Code !=
		http.StatusOK {
		t.Fatalf(
			"Admin credential reissue status=%d body=%s",
			adminReissue.Code,
			adminReissue.Body.String(),
		)
	}

	if bytes.Contains(
		adminReissue.Body.Bytes(),
		[]byte(`"qr_payload"`),
	) {
		t.Fatalf(
			"Admin credential reissue exposed QR payload: %s",
			adminReissue.Body.String(),
		)
	}

	var adminReissued struct {
		TicketID     string `json:"ticket_id"`
		CredentialID string `json:"credential_id"`
		Status       string `json:"status"`
	}

	if err := json.Unmarshal(
		adminReissue.Body.Bytes(),
		&adminReissued,
	); err != nil {
		t.Fatalf(
			"decode Admin credential reissue: %v",
			err,
		)
	}

	if adminReissued.TicketID !=
		adminTicketPublicID ||
		adminReissued.CredentialID == "" ||
		adminReissued.Status != "ACTIVE" {
		t.Fatalf(
			"invalid Admin credential reissue: %s",
			adminReissue.Body.String(),
		)
	}

	adminReissueReplay :=
		m6AdminIssuanceRequest(
			t,
			adminHandler,
			"/api/v1/admin/tickets/"+
				adminTicketPublicID+
				"/credentials/reissue",
			adminReissueKey,
			struct{}{},
		)

	if adminReissueReplay.Code !=
		http.StatusOK {
		t.Fatalf(
			"Admin credential reissue replay status=%d body=%s",
			adminReissueReplay.Code,
			adminReissueReplay.Body.String(),
		)
	}

	var firstAdminReissue any
	var replayAdminReissue any

	if err := json.Unmarshal(
		adminReissue.Body.Bytes(),
		&firstAdminReissue,
	); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		adminReissueReplay.Body.Bytes(),
		&replayAdminReissue,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		firstAdminReissue,
		replayAdminReissue,
	) {
		t.Fatalf(
			"Admin credential reissue replay changed result: first=%s replay=%s",
			adminReissue.Body.String(),
			adminReissueReplay.Body.String(),
		)
	}

	adminReplacementCredentialID, err :=
		publicid.Parse(
			adminReissued.CredentialID,
			publicid.Credential,
		)
	if err != nil {
		t.Fatalf(
			"parse Admin replacement credential: %v",
			err,
		)
	}

	var (
		adminOriginalState    string
		adminReplacementState string
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM qr_credentials
			WHERE id = $1
		`,
		adminOriginalCredentialID,
	).Scan(
		&adminOriginalState,
	); err != nil {
		t.Fatalf(
			"read Admin original credential: %v",
			err,
		)
	}

	if adminOriginalState !=
		"SUPERSEDED" {
		t.Fatalf(
			"Admin original credential state=%s want=SUPERSEDED",
			adminOriginalState,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM qr_credentials
			WHERE id = $1
		`,
		adminReplacementCredentialID,
	).Scan(
		&adminReplacementState,
	); err != nil {
		t.Fatalf(
			"read Admin replacement credential: %v",
			err,
		)
	}

	if adminReplacementState != "ACTIVE" {
		t.Fatalf(
			"Admin replacement credential state=%s want=ACTIVE",
			adminReplacementState,
		)
	}

	adminVoidKey :=
		uuid.NewString()

	adminVoidBody :=
		map[string]any{
			"reason": "administrative cancellation",
		}

	adminVoid :=
		m6AdminIssuanceRequest(
			t,
			adminHandler,
			"/api/v1/admin/tickets/"+
				adminTicketPublicID+
				"/void",
			adminVoidKey,
			adminVoidBody,
		)

	if adminVoid.Code !=
		http.StatusOK {
		t.Fatalf(
			"Admin Ticket void status=%d body=%s",
			adminVoid.Code,
			adminVoid.Body.String(),
		)
	}

	var adminVoided struct {
		TicketID   string `json:"ticket_id"`
		Status     string `json:"status"`
		VoidReason string `json:"void_reason"`
	}

	if err := json.Unmarshal(
		adminVoid.Body.Bytes(),
		&adminVoided,
	); err != nil {
		t.Fatal(err)
	}

	if adminVoided.TicketID !=
		adminTicketPublicID ||
		adminVoided.Status != "VOIDED" ||
		adminVoided.VoidReason !=
			"administrative cancellation" {
		t.Fatalf(
			"invalid Admin Ticket void: %s",
			adminVoid.Body.String(),
		)
	}

	adminVoidReplay :=
		m6AdminIssuanceRequest(
			t,
			adminHandler,
			"/api/v1/admin/tickets/"+
				adminTicketPublicID+
				"/void",
			adminVoidKey,
			adminVoidBody,
		)

	if adminVoidReplay.Code !=
		http.StatusOK {
		t.Fatalf(
			"Admin Ticket void replay status=%d body=%s",
			adminVoidReplay.Code,
			adminVoidReplay.Body.String(),
		)
	}

	var firstAdminVoid any
	var replayAdminVoid any

	if err := json.Unmarshal(
		adminVoid.Body.Bytes(),
		&firstAdminVoid,
	); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		adminVoidReplay.Body.Bytes(),
		&replayAdminVoid,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		firstAdminVoid,
		replayAdminVoid,
	) {
		t.Fatalf(
			"Admin Ticket void replay changed result: first=%s replay=%s",
			adminVoid.Body.String(),
			adminVoidReplay.Body.String(),
		)
	}

	var (
		adminTicketState      string
		adminActiveQRCount    int
		adminSupersededCount  int
		adminRevokedCount     int
		adminTicketAuditCount int
	)

	if err := pool.QueryRow(
		ctx,
		`
			SELECT status
			FROM ticket_entitlements
			WHERE id = $1
		`,
		issued.Tickets[0].TicketID,
	).Scan(
		&adminTicketState,
	); err != nil {
		t.Fatalf(
			"read Admin voided Ticket: %v",
			err,
		)
	}

	if adminTicketState != "VOIDED" {
		t.Fatalf(
			"Admin Ticket state=%s want=VOIDED",
			adminTicketState,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*) FILTER (
					WHERE status = 'ACTIVE'
				),
				COUNT(*) FILTER (
					WHERE status = 'SUPERSEDED'
				),
				COUNT(*) FILTER (
					WHERE status = 'REVOKED'
				)
			FROM qr_credentials
			WHERE ticket_entitlement_id = $1
		`,
		issued.Tickets[0].TicketID,
	).Scan(
		&adminActiveQRCount,
		&adminSupersededCount,
		&adminRevokedCount,
	); err != nil {
		t.Fatalf(
			"count Admin Ticket credentials: %v",
			err,
		)
	}

	if adminActiveQRCount != 0 ||
		adminSupersededCount != 1 ||
		adminRevokedCount != 1 {
		t.Fatalf(
			"Admin Ticket credentials active=%d superseded=%d revoked=%d want=0/1/1",
			adminActiveQRCount,
			adminSupersededCount,
			adminRevokedCount,
		)
	}

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM audit_events
			WHERE ticket_entitlement_id = $1
			  AND operation IN (
			      'TICKET_CREDENTIAL_SUPERSEDED',
			      'TICKET_CREDENTIAL_ISSUED',
			      'TICKET_VOIDED'
			  )
		`,
		issued.Tickets[0].TicketID,
	).Scan(
		&adminTicketAuditCount,
	); err != nil {
		t.Fatalf(
			"count Admin Ticket audit links: %v",
			err,
		)
	}

	if adminTicketAuditCount != 3 {
		t.Fatalf(
			"Admin Ticket linked audit count=%d want=3",
			adminTicketAuditCount,
		)
	}

	httpAllocationPublicID :=
		publicid.Encode(
			publicid.Allocation,
			httpAllocationID,
		)

	gaPublicID :=
		publicid.Encode(
			publicid.GAPool,
			gaID,
		)

	httpBody := map[string]any{
		"recipient_ref": "press-group-1",
		"reason":        "Press allocation",
		"ga_targets": []map[string]any{
			{
				"inventory_id": gaPublicID,
				"quantity":     2,
			},
		},
	}

	idempotencyKey :=
		uuid.NewString()

	firstHTTP :=
		m6AdminIssuanceRequest(
			t,
			adminHandler,
			"/api/v1/admin/allocations/"+
				httpAllocationPublicID+
				"/issuances",
			idempotencyKey,
			httpBody,
		)

	if firstHTTP.Code !=
		http.StatusCreated {
		t.Fatalf(
			"HTTP issuance status=%d body=%s",
			firstHTTP.Code,
			firstHTTP.Body.String(),
		)
	}

	var firstLogical map[string]any

	if err := json.Unmarshal(
		firstHTTP.Body.Bytes(),
		&firstLogical,
	); err != nil {
		t.Fatalf(
			"decode HTTP issuance: %v",
			err,
		)
	}

	if firstLogical["issuance_id"] == nil {
		t.Fatalf(
			"HTTP issuance ID missing: %s",
			firstHTTP.Body.String(),
		)
	}

	tickets, ok :=
		firstLogical["tickets"].([]any)

	if !ok ||
		len(tickets) != 2 {
		t.Fatalf(
			"HTTP issuance Tickets invalid: %s",
			firstHTTP.Body.String(),
		)
	}

	if bytes.Contains(
		firstHTTP.Body.Bytes(),
		[]byte(`"qr_payload"`),
	) {
		t.Fatalf(
			"HTTP issuance exposed raw QR payload: %s",
			firstHTTP.Body.String(),
		)
	}

	replayHTTP :=
		m6AdminIssuanceRequest(
			t,
			adminHandler,
			"/api/v1/admin/allocations/"+
				httpAllocationPublicID+
				"/issuances",
			idempotencyKey,
			httpBody,
		)

	if replayHTTP.Code !=
		http.StatusCreated {
		t.Fatalf(
			"HTTP issuance replay status=%d body=%s",
			replayHTTP.Code,
			replayHTTP.Body.String(),
		)
	}

	var replayLogical map[string]any

	if err := json.Unmarshal(
		replayHTTP.Body.Bytes(),
		&replayLogical,
	); err != nil {
		t.Fatalf(
			"decode HTTP issuance replay: %v",
			err,
		)
	}

	if !reflect.DeepEqual(
		firstLogical,
		replayLogical,
	) {
		t.Fatalf(
			"HTTP issuance replay changed logical result: first=%s replay=%s",
			firstHTTP.Body.String(),
			replayHTTP.Body.String(),
		)
	}

	conflictBody := map[string]any{
		"recipient_ref": "press-group-1",
		"reason":        "Press allocation",
		"ga_targets": []map[string]any{
			{
				"inventory_id": gaPublicID,
				"quantity":     1,
			},
		},
	}

	conflictHTTP :=
		m6AdminIssuanceRequest(
			t,
			adminHandler,
			"/api/v1/admin/allocations/"+
				httpAllocationPublicID+
				"/issuances",
			idempotencyKey,
			conflictBody,
		)

	m6RequireAdminError(
		t,
		conflictHTTP,
		"IDEMPOTENCY_CONFLICT",
	)

	var httpIssuanceCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM non_public_issuances
			WHERE allocation_id = $1
		`,
		httpAllocationID,
	).Scan(
		&httpIssuanceCount,
	); err != nil {
		t.Fatalf(
			"count HTTP issuances: %v",
			err,
		)
	}

	if httpIssuanceCount != 1 {
		t.Fatalf(
			"HTTP issuance count=%d want=1",
			httpIssuanceCount,
		)
	}
}

func m6AdminIssuanceRequest(
	t *testing.T,
	handler http.Handler,
	path string,
	idempotencyKey string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf(
			"marshal M6 Admin request: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		path,
		bytes.NewReader(raw),
	)

	request.Header.Set(
		"Authorization",
		"Bearer m6-admin-token",
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

	handler.ServeHTTP(
		response,
		request,
	)

	return response
}

func m6RequireAdminError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	expected string,
) {
	t.Helper()

	if response.Code >= 200 &&
		response.Code < 300 {
		t.Fatalf(
			"expected %s but status=%d body=%s",
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
			"decode Admin error: %v body=%s",
			err,
			response.Body.String(),
		)
	}

	if body.Error.Code != expected {
		t.Fatalf(
			"Admin error=%s want=%s status=%d body=%s",
			body.Error.Code,
			expected,
			response.Code,
			response.Body.String(),
		)
	}
}

func m6TimePtr(
	value time.Time,
) *time.Time {
	return &value
}
