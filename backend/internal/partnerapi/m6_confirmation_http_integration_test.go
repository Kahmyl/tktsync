//go:build integration

package partnerapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type m6ConfirmationResponse struct {
	ReservationID string `json:"reservation_id"`
	Status        string `json:"status"`
	Sale          struct {
		ID                string `json:"id"`
		PartnerOrderRef   string `json:"partner_order_ref"`
		PartnerPaymentRef string `json:"partner_payment_ref"`
	} `json:"sale"`
	Tickets []struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		CredentialID string `json:"credential_id"`
	} `json:"tickets"`
}

type m6CredentialResponse struct {
	TicketID     string `json:"ticket_id"`
	CredentialID string `json:"credential_id"`
	Status       string `json:"status"`
	QRPayload    string `json:"qr_payload"`
}

func m6AssertPartnerConfirmationHTTP(
	t *testing.T,
	handler http.Handler,
	pool *pgxpool.Pool,
	credentialA string,
	credentialB string,
	eventPublicID string,
) {
	t.Helper()

	availabilityResponse :=
		m5PartnerHTTP(
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
			"M6 availability status=%d body=%s",
			availabilityResponse.Code,
			availabilityResponse.Body.String(),
		)
	}

	var availability m5AvailabilityResponse

	if err := json.Unmarshal(
		availabilityResponse.Body.Bytes(),
		&availability,
	); err != nil {
		t.Fatalf(
			"decode M6 availability: %v",
			err,
		)
	}

	if len(availability.ReservedUnits) == 0 ||
		availability.ReservedUnits[0].
			Offer == nil ||
		len(availability.GAPools) == 0 ||
		len(availability.GAPools[0].
			Offers) == 0 {
		t.Fatalf(
			"M6 offers missing: %s",
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
		"partner_customer_ref": "m6-customer",
		"partner_order_ref":    "m6-cart",
		"buyer_session_ref":    "m6-browser",
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

	createdResponse :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations",
			credentialA,
			uuid.NewString(),
			"",
			createBody,
			"",
		)

	if createdResponse.Code !=
		http.StatusCreated {
		t.Fatalf(
			"M6 create status=%d body=%s",
			createdResponse.Code,
			createdResponse.Body.String(),
		)
	}

	var created m5CreateReservationResponse

	if err := json.Unmarshal(
		createdResponse.Body.Bytes(),
		&created,
	); err != nil {
		t.Fatalf(
			"decode M6 create: %v",
			err,
		)
	}

	checkoutResponse :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/checkout",
			credentialA,
			uuid.NewString(),
			created.ReservationToken,
			nil,
			"",
		)

	if checkoutResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"M6 checkout status=%d body=%s",
			checkoutResponse.Code,
			checkoutResponse.Body.String(),
		)
	}

	var checkout m5CheckoutResponse

	if err := json.Unmarshal(
		checkoutResponse.Body.Bytes(),
		&checkout,
	); err != nil {
		t.Fatalf(
			"decode M6 checkout: %v",
			err,
		)
	}

	confirmKey := uuid.NewString()

	confirmBody := map[string]any{
		"checkout_attempt_id": checkout.
			CheckoutAttempt.ID,
		"partner_order_ref":   "ORD-M6-HTTP",
		"partner_payment_ref": "PAY-M6-HTTP",
	}

	confirmedResponse :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/confirm",
			credentialA,
			confirmKey,
			created.ReservationToken,
			confirmBody,
			uuid.NewString(),
		)

	if confirmedResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"M6 confirm status=%d body=%s",
			confirmedResponse.Code,
			confirmedResponse.Body.String(),
		)
	}

	if bytes.Contains(
		confirmedResponse.Body.Bytes(),
		[]byte(`"qr_payload"`),
	) {
		t.Fatalf(
			"confirmation exposed raw QR payload: %s",
			confirmedResponse.Body.String(),
		)
	}

	var confirmed m6ConfirmationResponse

	if err := json.Unmarshal(
		confirmedResponse.Body.Bytes(),
		&confirmed,
	); err != nil {
		t.Fatalf(
			"decode M6 confirmation: %v",
			err,
		)
	}

	if confirmed.ReservationID !=
		created.Reservation.ID ||
		confirmed.Status != "CONFIRMED" ||
		confirmed.Sale.ID == "" ||
		confirmed.Sale.PartnerOrderRef !=
			"ORD-M6-HTTP" ||
		confirmed.Sale.PartnerPaymentRef !=
			"PAY-M6-HTTP" {
		t.Fatalf(
			"invalid M6 confirmation: %s",
			confirmedResponse.Body.String(),
		)
	}

	if len(confirmed.Tickets) != 3 {
		t.Fatalf(
			"M6 confirmation tickets=%d want=3 body=%s",
			len(confirmed.Tickets),
			confirmedResponse.Body.String(),
		)
	}

	for _, ticket := range confirmed.Tickets {
		if ticket.ID == "" ||
			ticket.Status != "ACTIVE" ||
			ticket.CredentialID == "" {
			t.Fatalf(
				"invalid M6 Ticket result: %+v",
				ticket,
			)
		}
	}

	replay :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/confirm",
			credentialA,
			confirmKey,
			created.ReservationToken,
			confirmBody,
			uuid.NewString(),
		)

	if replay.Code != http.StatusOK {
		t.Fatalf(
			"M6 confirm replay status=%d body=%s",
			replay.Code,
			replay.Body.String(),
		)
	}

	var firstLogical any
	var replayLogical any

	if err := json.Unmarshal(
		confirmedResponse.Body.Bytes(),
		&firstLogical,
	); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		replay.Body.Bytes(),
		&replayLogical,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		firstLogical,
		replayLogical,
	) {
		t.Fatalf(
			"M6 confirmation replay changed logical result: first=%s replay=%s",
			confirmedResponse.Body.String(),
			replay.Body.String(),
		)
	}

	conflictBody := map[string]any{
		"checkout_attempt_id": checkout.
			CheckoutAttempt.ID,
		"partner_order_ref":   "ORD-M6-CHANGED",
		"partner_payment_ref": "PAY-M6-HTTP",
	}

	conflict :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/reservations/"+
				created.Reservation.ID+
				"/confirm",
			credentialA,
			confirmKey,
			created.ReservationToken,
			conflictBody,
			"",
		)

	m5RequireErrorCode(
		t,
		conflict,
		"IDEMPOTENCY_CONFLICT",
	)

	credentialResponse :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/tickets/"+
				confirmed.Tickets[0].ID+
				"/credential",
			credentialA,
			"",
			"",
			nil,
			uuid.NewString(),
		)

	if credentialResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"M6 credential status=%d body=%s",
			credentialResponse.Code,
			credentialResponse.Body.String(),
		)
	}

	if credentialResponse.Header().Get(
		"Cache-Control",
	) != "no-store" {
		t.Fatalf(
			"M6 credential Cache-Control=%q want=no-store",
			credentialResponse.Header().Get(
				"Cache-Control",
			),
		)
	}

	var credential m6CredentialResponse

	if err := json.Unmarshal(
		credentialResponse.Body.Bytes(),
		&credential,
	); err != nil {
		t.Fatalf(
			"decode M6 credential: %v",
			err,
		)
	}

	if credential.TicketID !=
		confirmed.Tickets[0].ID ||
		credential.CredentialID !=
			confirmed.Tickets[0].
				CredentialID ||
		credential.Status != "ACTIVE" ||
		credential.QRPayload == "" {
		t.Fatalf(
			"invalid M6 credential: %s",
			credentialResponse.Body.String(),
		)
	}

	if !bytes.HasPrefix(
		[]byte(credential.QRPayload),
		[]byte("qr1."),
	) {
		t.Fatalf(
			"M6 credential payload is not an opaque qr1 credential: %q",
			credential.QRPayload,
		)
	}

	secondCredentialResponse :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/tickets/"+
				confirmed.Tickets[0].ID+
				"/credential",
			credentialA,
			"",
			"",
			nil,
			"",
		)

	if secondCredentialResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"M6 credential recovery status=%d body=%s",
			secondCredentialResponse.Code,
			secondCredentialResponse.Body.String(),
		)
	}

	var secondCredential m6CredentialResponse

	if err := json.Unmarshal(
		secondCredentialResponse.Body.Bytes(),
		&secondCredential,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		credential,
		secondCredential,
	) {
		t.Fatalf(
			"M6 credential retrieval was not reproducible: first=%s second=%s",
			credentialResponse.Body.String(),
			secondCredentialResponse.Body.String(),
		)
	}

	crossPartner :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/tickets/"+
				confirmed.Tickets[0].ID+
				"/credential",
			credentialB,
			"",
			"",
			nil,
			"",
		)

	m5RequireErrorCode(
		t,
		crossPartner,
		"RESOURCE_NOT_FOUND",
	)

	reservationID, err := publicid.Parse(
		created.Reservation.ID,
		publicid.Reservation,
	)
	if err != nil {
		t.Fatalf(
			"parse current M6 Reservation ID: %v",
			err,
		)
	}

	var (
		saleCount       int
		ticketCount     int
		credentialCount int
	)

	if err := pool.QueryRow(
		t.Context(),
		`
			SELECT COUNT(*)
			FROM sales s
			WHERE s.reservation_id = $1
			  AND s.partner_order_ref =
			      'ORD-M6-HTTP'
			  AND s.partner_payment_ref =
			      'PAY-M6-HTTP'
		`,
		reservationID,
	).Scan(
		&saleCount,
	); err != nil {
		t.Fatalf(
			"count M6 Sales: %v",
			err,
		)
	}

	if saleCount != 1 {
		t.Fatalf(
			"M6 Sale count=%d want=1",
			saleCount,
		)
	}

	if err := pool.QueryRow(
		t.Context(),
		`
			SELECT COUNT(*)
			FROM ticket_entitlements t
			JOIN sale_items si
			  ON si.id =
			     t.origin_sale_item_id
			JOIN sales s
			  ON s.id = si.sale_id
			WHERE s.reservation_id = $1
			  AND t.status = 'ACTIVE'
		`,
		reservationID,
	).Scan(
		&ticketCount,
	); err != nil {
		t.Fatalf(
			"count M6 Tickets: %v",
			err,
		)
	}

	if ticketCount != 3 {
		t.Fatalf(
			"M6 Ticket count=%d want=3",
			ticketCount,
		)
	}

	if err := pool.QueryRow(
		t.Context(),
		`
			SELECT COUNT(*)
			FROM qr_credentials q
			JOIN ticket_entitlements t
			  ON t.id =
			     q.ticket_entitlement_id
			JOIN sale_items si
			  ON si.id =
			     t.origin_sale_item_id
			JOIN sales s
			  ON s.id = si.sale_id
			WHERE s.reservation_id = $1
			  AND q.status = 'ACTIVE'
		`,
		reservationID,
	).Scan(
		&credentialCount,
	); err != nil {
		t.Fatalf(
			"count M6 credentials: %v",
			err,
		)
	}

	if credentialCount != 3 {
		t.Fatalf(
			"M6 active credential count=%d want=3",
			credentialCount,
		)
	}

	m6AssertPartnerTicketLifecycle(
		t,
		handler,
		pool,
		credentialA,
		credentialB,
		confirmed.Tickets[0].ID,
		confirmed.Tickets[0].CredentialID,
	)
}

func m6AssertPartnerTicketLifecycle(
	t *testing.T,
	handler http.Handler,
	pool *pgxpool.Pool,
	credentialA string,
	credentialB string,
	ticketPublicID string,
	originalCredentialPublicID string,
) {
	t.Helper()

	reissueKey := uuid.NewString()

	reissueResponse :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/credentials/reissue",
			credentialA,
			reissueKey,
			"",
			nil,
			uuid.NewString(),
		)

	if reissueResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"M6 credential reissue status=%d body=%s",
			reissueResponse.Code,
			reissueResponse.Body.String(),
		)
	}

	if bytes.Contains(
		reissueResponse.Body.Bytes(),
		[]byte(`"qr_payload"`),
	) {
		t.Fatalf(
			"credential reissue exposed raw QR payload: %s",
			reissueResponse.Body.String(),
		)
	}

	var reissued struct {
		TicketID     string `json:"ticket_id"`
		CredentialID string `json:"credential_id"`
		Status       string `json:"status"`
	}

	if err := json.Unmarshal(
		reissueResponse.Body.Bytes(),
		&reissued,
	); err != nil {
		t.Fatalf(
			"decode credential reissue: %v",
			err,
		)
	}

	if reissued.TicketID !=
		ticketPublicID ||
		reissued.CredentialID == "" ||
		reissued.CredentialID ==
			originalCredentialPublicID ||
		reissued.Status != "ACTIVE" {
		t.Fatalf(
			"invalid credential reissue: %s",
			reissueResponse.Body.String(),
		)
	}

	reissueReplay :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/credentials/reissue",
			credentialA,
			reissueKey,
			"",
			nil,
			"",
		)

	if reissueReplay.Code !=
		http.StatusOK {
		t.Fatalf(
			"credential reissue replay status=%d body=%s",
			reissueReplay.Code,
			reissueReplay.Body.String(),
		)
	}

	var firstReissue any
	var replayReissue any

	if err := json.Unmarshal(
		reissueResponse.Body.Bytes(),
		&firstReissue,
	); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		reissueReplay.Body.Bytes(),
		&replayReissue,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		firstReissue,
		replayReissue,
	) {
		t.Fatalf(
			"credential reissue replay changed result: first=%s replay=%s",
			reissueResponse.Body.String(),
			reissueReplay.Body.String(),
		)
	}

	crossPartnerReissue :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/credentials/reissue",
			credentialB,
			uuid.NewString(),
			"",
			nil,
			"",
		)

	m5RequireErrorCode(
		t,
		crossPartnerReissue,
		"RESOURCE_NOT_FOUND",
	)

	currentCredential :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/credential",
			credentialA,
			"",
			"",
			nil,
			"",
		)

	if currentCredential.Code !=
		http.StatusOK {
		t.Fatalf(
			"credential after reissue status=%d body=%s",
			currentCredential.Code,
			currentCredential.Body.String(),
		)
	}

	var current m6CredentialResponse

	if err := json.Unmarshal(
		currentCredential.Body.Bytes(),
		&current,
	); err != nil {
		t.Fatal(err)
	}

	if current.CredentialID !=
		reissued.CredentialID ||
		current.QRPayload == "" {
		t.Fatalf(
			"credential recovery did not return replacement: %s",
			currentCredential.Body.String(),
		)
	}

	ticketID, err := publicid.Parse(
		ticketPublicID,
		publicid.Ticket,
	)
	if err != nil {
		t.Fatalf(
			"parse lifecycle Ticket: %v",
			err,
		)
	}

	originalCredentialID, err :=
		publicid.Parse(
			originalCredentialPublicID,
			publicid.Credential,
		)
	if err != nil {
		t.Fatalf(
			"parse original credential: %v",
			err,
		)
	}

	reissuedCredentialID, err :=
		publicid.Parse(
			reissued.CredentialID,
			publicid.Credential,
		)
	if err != nil {
		t.Fatalf(
			"parse reissued credential: %v",
			err,
		)
	}

	var (
		originalState string
		reissuedState string
	)

	if err := pool.QueryRow(
		t.Context(),
		`
			SELECT status
			FROM qr_credentials
			WHERE id = $1
		`,
		originalCredentialID,
	).Scan(
		&originalState,
	); err != nil {
		t.Fatalf(
			"read original credential state: %v",
			err,
		)
	}

	if originalState != "SUPERSEDED" {
		t.Fatalf(
			"original credential state=%s want=SUPERSEDED",
			originalState,
		)
	}

	if err := pool.QueryRow(
		t.Context(),
		`
			SELECT status
			FROM qr_credentials
			WHERE id = $1
		`,
		reissuedCredentialID,
	).Scan(
		&reissuedState,
	); err != nil {
		t.Fatalf(
			"read replacement credential state: %v",
			err,
		)
	}

	if reissuedState != "ACTIVE" {
		t.Fatalf(
			"replacement credential state=%s want=ACTIVE",
			reissuedState,
		)
	}

	crossPartnerVoid :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/void",
			credentialB,
			uuid.NewString(),
			"",
			map[string]any{
				"reason": "not owner",
			},
			"",
		)

	m5RequireErrorCode(
		t,
		crossPartnerVoid,
		"RESOURCE_NOT_FOUND",
	)

	voidKey := uuid.NewString()

	voidBody := map[string]any{
		"reason": "customer cancellation",
	}

	voidResponse :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/void",
			credentialA,
			voidKey,
			"",
			voidBody,
			uuid.NewString(),
		)

	if voidResponse.Code !=
		http.StatusOK {
		t.Fatalf(
			"Ticket void status=%d body=%s",
			voidResponse.Code,
			voidResponse.Body.String(),
		)
	}

	var voided struct {
		TicketID   string `json:"ticket_id"`
		Status     string `json:"status"`
		VoidReason string `json:"void_reason"`
	}

	if err := json.Unmarshal(
		voidResponse.Body.Bytes(),
		&voided,
	); err != nil {
		t.Fatal(err)
	}

	if voided.TicketID !=
		ticketPublicID ||
		voided.Status != "VOIDED" ||
		voided.VoidReason !=
			"customer cancellation" {
		t.Fatalf(
			"invalid Ticket void result: %s",
			voidResponse.Body.String(),
		)
	}

	voidReplay :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/void",
			credentialA,
			voidKey,
			"",
			voidBody,
			"",
		)

	if voidReplay.Code !=
		http.StatusOK {
		t.Fatalf(
			"Ticket void replay status=%d body=%s",
			voidReplay.Code,
			voidReplay.Body.String(),
		)
	}

	var firstVoid any
	var replayVoid any

	if err := json.Unmarshal(
		voidResponse.Body.Bytes(),
		&firstVoid,
	); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		voidReplay.Body.Bytes(),
		&replayVoid,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		firstVoid,
		replayVoid,
	) {
		t.Fatalf(
			"Ticket void replay changed result: first=%s replay=%s",
			voidResponse.Body.String(),
			voidReplay.Body.String(),
		)
	}

	credentialAfterVoid :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodGet,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/credential",
			credentialA,
			"",
			"",
			nil,
			"",
		)

	m5RequireErrorCode(
		t,
		credentialAfterVoid,
		"TICKET_VOID",
	)

	reissueAfterVoid :=
		m5PartnerHTTP(
			t,
			handler,
			http.MethodPost,
			"/api/v1/partner/tickets/"+
				ticketPublicID+
				"/credentials/reissue",
			credentialA,
			uuid.NewString(),
			"",
			nil,
			"",
		)

	m5RequireErrorCode(
		t,
		reissueAfterVoid,
		"TICKET_VOID",
	)

	var (
		ticketState       string
		voidReason        *string
		activeQRCount     int
		supersededQRCount int
		revokedQRCount    int
		saleCount         int
	)

	if err := pool.QueryRow(
		t.Context(),
		`
			SELECT
				status,
				void_reason
			FROM ticket_entitlements
			WHERE id = $1
		`,
		ticketID,
	).Scan(
		&ticketState,
		&voidReason,
	); err != nil {
		t.Fatalf(
			"read voided Ticket: %v",
			err,
		)
	}

	if ticketState != "VOIDED" ||
		voidReason == nil ||
		*voidReason !=
			"customer cancellation" {
		t.Fatalf(
			"persisted Ticket state=%s reason=%v",
			ticketState,
			voidReason,
		)
	}

	if err := pool.QueryRow(
		t.Context(),
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
		ticketID,
	).Scan(
		&activeQRCount,
		&supersededQRCount,
		&revokedQRCount,
	); err != nil {
		t.Fatalf(
			"count Ticket credentials: %v",
			err,
		)
	}

	if activeQRCount != 0 ||
		supersededQRCount != 1 ||
		revokedQRCount != 1 {
		t.Fatalf(
			"Ticket credential history active=%d superseded=%d revoked=%d want=0/1/1",
			activeQRCount,
			supersededQRCount,
			revokedQRCount,
		)
	}

	if err := pool.QueryRow(
		t.Context(),
		`
			SELECT COUNT(*)
			FROM sales s
			JOIN sale_items si
			  ON si.sale_id = s.id
			JOIN ticket_entitlements t
			  ON t.origin_sale_item_id =
			     si.id
			WHERE t.id = $1
		`,
		ticketID,
	).Scan(
		&saleCount,
	); err != nil {
		t.Fatalf(
			"count Sale after Ticket void: %v",
			err,
		)
	}

	if saleCount != 1 {
		t.Fatalf(
			"Ticket void rewrote commercial Sale history; Sale count=%d want=1",
			saleCount,
		)
	}
}
