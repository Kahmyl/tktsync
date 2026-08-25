//go:build integration

package reservation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/admission"
	"github.com/tktsync/tktsync/backend/internal/admissionapi"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func TestAdmissionCredentialRecoveryWrappersShareStateSemantics(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ticketID, _ := confirmedAdmissionTicket(t, ctx, f, "A7")

	partnerCredential, err := f.reservation.RecoverActiveCredentialForPartner(
		ctx,
		f.partnerID,
		ticketID,
	)
	if err != nil {
		t.Fatal(err)
	}
	adminCredential, err := f.reservation.RecoverActiveCredentialAdmin(ctx, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := f.reservation.TicketQRPresentationCapability(
		ticketID,
		partnerCredential.CredentialID,
	)
	if err != nil {
		t.Fatal(err)
	}
	presentationCredential, err := f.reservation.RecoverActiveCredentialForPresentation(
		ctx,
		capability,
	)
	if err != nil {
		t.Fatal(err)
	}
	for name, credential := range map[string]reservation.ActiveCredential{
		"admin":        adminCredential,
		"presentation": presentationCredential,
	} {
		if credential != partnerCredential {
			t.Fatalf("%s recovery=%+v want=%+v", name, credential, partnerCredential)
		}
	}

	if _, err = f.reservation.ReissuePartnerCredential(ctx, f.partnerID, ticketID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.reservation.RecoverActiveCredentialForPresentation(ctx, capability); err == nil {
		t.Fatal("superseded presentation capability remained valid")
	} else if apiErr, ok := apierror.As(err); !ok || apiErr.Code != apierror.CodeResourceNotFound {
		t.Fatalf("superseded presentation error=%v want safe not found", err)
	}

	current, err := f.reservation.RecoverActiveCredentialForPartner(ctx, f.partnerID, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	currentCapability, err := f.reservation.TicketQRPresentationCapability(
		ticketID,
		current.CredentialID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = eventsvc.NewService(f.runner).CancelEvent(
		ctx,
		f.userID,
		f.eventID,
		"Credential recovery state test",
	); err != nil {
		t.Fatal(err)
	}

	_, partnerErr := f.reservation.RecoverActiveCredentialForPartner(
		ctx,
		f.partnerID,
		ticketID,
	)
	_, adminErr := f.reservation.RecoverActiveCredentialAdmin(ctx, ticketID)
	_, presentationErr := f.reservation.RecoverActiveCredentialForPresentation(
		ctx,
		currentCapability,
	)
	for name, recoveryErr := range map[string]error{
		"partner":      partnerErr,
		"admin":        adminErr,
		"presentation": presentationErr,
	} {
		apiErr, ok := apierror.As(recoveryErr)
		if !ok || apiErr.Code != apierror.CodeEventCancelled {
			t.Fatalf("%s recovery error=%v want EVENT_CANCELLED", name, recoveryErr)
		}
	}
}

func TestAdmissionHTTPIdempotencyReplayAndConflict(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, payload := confirmedAdmissionTicket(t, ctx, f, "A7")
	service := admission.NewService(f.runner, admissionKeyring(t))
	handler, err := admissionapi.New(admissionapi.Dependencies{Database: f.pool, Transactions: f.runner, HumanAuth: func(context.Context, string) (auth.HumanPrincipal, error) {
		return auth.HumanPrincipal{Provider: "reservation", Subject: f.userSubject}, nil
	}, Admission: service})
	if err != nil {
		t.Fatal(err)
	}
	call := func(key, gate string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"event_id": publicEventID(f.eventID), "credential": payload, "gate_reference": gate})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admission/scans", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer scanner-session")
		request.Header.Set("Idempotency-Key", key)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := call("stable-key", "gate-1")
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	replay := call("stable-key", "gate-1")
	var firstBody, replayBody map[string]any
	if replay.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &firstBody) != nil || json.Unmarshal(replay.Body.Bytes(), &replayBody) != nil || firstBody["scan_attempt_id"] != replayBody["scan_attempt_id"] || firstBody["admission_id"] != replayBody["admission_id"] || firstBody["result"] != replayBody["result"] {
		t.Fatalf("replay status/body=%d %s want logical result %s", replay.Code, replay.Body.String(), first.Body.String())
	}
	conflict := call("stable-key", "gate-2")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	var attempts int
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM scan_attempts WHERE event_id=$1 AND scanner_user_id=$2`, f.eventID, f.userID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d want=1", attempts)
	}
}

func TestAdmissionHTTPListsOnlyAuthenticatedOperatorEvents(t *testing.T) {
	f := newFixture(t)
	handler, err := admissionapi.New(admissionapi.Dependencies{
		Database: f.pool, Transactions: f.runner,
		HumanAuth: func(context.Context, string) (auth.HumanPrincipal, error) {
			return auth.HumanPrincipal{Provider: "reservation", Subject: f.userSubject}, nil
		},
		Admission: admission.NewService(f.runner, admissionKeyring(t)),
	})
	if err != nil {
		t.Fatal(err)
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/admission/events", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admission/events", nil)
	request.Header.Set("Authorization", "Bearer operator-session")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q", response.Header().Get("Cache-Control"))
	}
	var body struct {
		Items []struct {
			ID        string  `json:"id"`
			Name      string  `json:"name"`
			VenueName string  `json:"venue_name"`
			StartsAt  *string `json:"starts_at"`
		} `json:"items"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	wantID := publicEventID(f.eventID)
	for _, item := range body.Items {
		if item.ID == wantID {
			if item.Name == "" || item.VenueName == "" {
				t.Fatalf("event missing buyer-facing fields: %+v", item)
			}
			return
		}
	}
	t.Fatalf("authorized event %s missing from response: %s", wantID, response.Body.String())
}

func publicEventID(id uuid.UUID) string { return publicid.Encode(publicid.Event, id) }

func confirmedAdmissionTicket(t *testing.T, ctx context.Context, f fixture, seat string) (uuid.UUID, string) {
	t.Helper()
	created, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs[seat], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := f.reservation.BeginCheckout(ctx, f.partnerID, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.reservation.Confirm(ctx, f.partnerID, created.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID})
	if err != nil {
		t.Fatal(err)
	}
	ticketID := confirmed.Tickets[0].TicketID
	credential, err := f.reservation.RecoverActiveCredentialForPartner(ctx, f.partnerID, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	return ticketID, credential.QRPayload
}

func confirmedAdmissionGATicket(t *testing.T, ctx context.Context, f fixture) (uuid.UUID, string) {
	t.Helper()
	created, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryGA, InventoryID: f.gaMainID, Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := f.reservation.BeginCheckout(ctx, f.partnerID, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.reservation.Confirm(ctx, f.partnerID, created.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID})
	if err != nil {
		t.Fatal(err)
	}
	ticketID := confirmed.Tickets[0].TicketID
	credential, err := f.reservation.RecoverActiveCredentialForPartner(ctx, f.partnerID, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	return ticketID, credential.QRPayload
}

func admissionOperation(t *testing.T, ctx context.Context, f fixture, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(ctx, `INSERT INTO idempotency_operations(id,scope_kind,app_user_id,operation_type,idempotency_key,request_hash,execution_state) VALUES($1,'USER',$2,'ADMISSION_SCAN',$3,$4,'IN_PROGRESS')`, id, f.userID, key, []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAdmissionLifecycleAndCredentialStates(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	keyring := admissionKeyring(t)
	service := admission.NewService(f.runner, keyring)
	ticketID, payload := confirmedAdmissionTicket(t, ctx, f, "A1")
	first, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, GateReference: "gate-1", ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "first")})
	if err != nil {
		t.Fatal(err)
	}
	if first.Result != "ADMITTED" || first.AdmissionID == nil {
		t.Fatalf("first=%+v", first)
	}
	second, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, GateReference: "gate-2", ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "second")})
	if err != nil {
		t.Fatal(err)
	}
	if second.Result != "TICKET_ALREADY_ADMITTED" || second.PreviousAdmittedAt == nil {
		t.Fatalf("second=%+v", second)
	}

	_, gaPayload := confirmedAdmissionGATicket(t, ctx, f)
	gaAdmission, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: gaPayload, GateReference: "gate-ga", ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "ga-display")})
	if err != nil {
		t.Fatal(err)
	}
	if gaAdmission.Result != "ADMITTED" || gaAdmission.TicketDisplay.Section != "GA Main" || gaAdmission.TicketDisplay.Row != "" || gaAdmission.TicketDisplay.Seat != "" {
		t.Fatalf("GA admission display=%+v", gaAdmission)
	}
	reversed, err := service.Reverse(ctx, f.userID, *first.AdmissionID, "operator correction")
	if err != nil {
		t.Fatal(err)
	}
	if reversed.TicketID != ticketID {
		t.Fatalf("reversal=%+v", reversed)
	}
	third, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, GateReference: "gate-3", ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "third")})
	if err != nil {
		t.Fatal(err)
	}
	if third.Result != "ADMITTED" {
		t.Fatalf("scan after reversal=%+v", third)
	}

	malformed, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: "not-a-credential", ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "malformed")})
	if err != nil {
		t.Fatal(err)
	}
	if malformed.Result != "TICKET_INVALID" {
		t.Fatalf("malformed=%+v", malformed)
	}

	other := newFixture(t)
	wrong, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: other.eventID, Credential: payload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, other, "wrong-event")})
	if err != nil {
		t.Fatal(err)
	}
	if wrong.Result != "WRONG_EVENT" {
		t.Fatalf("wrong=%+v", wrong)
	}

	supersededTicket, oldPayload := confirmedAdmissionTicket(t, ctx, f, "A2")
	if _, err = f.reservation.ReissuePartnerCredential(ctx, f.partnerID, supersededTicket); err != nil {
		t.Fatal(err)
	}
	superseded, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: oldPayload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "superseded")})
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Result != "CREDENTIAL_SUPERSEDED" {
		t.Fatalf("superseded=%+v", superseded)
	}

	revokedTicket, revokedPayload := confirmedAdmissionTicket(t, ctx, f, "A4")
	if _, err = f.pool.Exec(ctx, `UPDATE qr_credentials SET status='REVOKED',revoked_at=clock_timestamp() WHERE ticket_entitlement_id=$1 AND status='ACTIVE'`, revokedTicket); err != nil {
		t.Fatal(err)
	}
	revoked, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: revokedPayload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "revoked")})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Result != "CREDENTIAL_REVOKED" {
		t.Fatalf("revoked=%+v", revoked)
	}

	voidTicket, voidPayload := confirmedAdmissionTicket(t, ctx, f, "A5")
	if _, err = f.reservation.VoidPartnerTicket(ctx, f.partnerID, voidTicket, "fraud"); err != nil {
		t.Fatal(err)
	}
	voided, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: voidPayload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "void")})
	if err != nil {
		t.Fatal(err)
	}
	if voided.Result != "TICKET_VOID" {
		t.Fatalf("voided=%+v", voided)
	}

	_, closedPayload := confirmedAdmissionTicket(t, ctx, f, "A6")
	if _, err = f.pool.Exec(ctx, `UPDATE events SET admission_open_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	closed, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: closedPayload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "not-open")})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Result != "ADMISSION_NOT_OPEN" {
		t.Fatalf("closed=%+v", closed)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE events SET admission_open_at=NULL WHERE id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	manualTicket, _ := confirmedAdmissionTicket(t, ctx, f, "A7")
	manualOperation := admissionOperation(t, ctx, f, "manual")
	manual, err := service.ManualOverride(idempotency.WithOperationID(ctx, manualOperation), f.userID, admission.ManualOverrideInput{EventID: f.eventID, TicketID: manualTicket, GateReference: "supervisor", Reason: "damaged printed credential"})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Result != "MANUAL_OVERRIDE_ADMITTED" || manual.AdmissionID == nil {
		t.Fatalf("manual=%+v", manual)
	}

	unauthorizedUser := uuid.New()
	if _, err = f.pool.Exec(ctx, `INSERT INTO app_users(id,auth_provider,auth_subject,display_name,state,created_at,updated_at) VALUES($1,'admission',$2,'Unauthorized','ACTIVE',clock_timestamp(),clock_timestamp())`, unauthorizedUser, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Reverse(ctx, unauthorizedUser, *manual.AdmissionID, "not allowed"); err == nil {
		t.Fatal("unauthorized reversal succeeded")
	} else if apiErr, ok := apierror.As(err); !ok || apiErr.Code != apierror.CodeNotAuthorized {
		t.Fatalf("unauthorized reversal=%v", err)
	}

	if _, err = f.pool.Exec(ctx, `UPDATE events SET state='CANCELLED',cancelled_at=clock_timestamp() WHERE id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, "cancelled")})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Result != "EVENT_CANCELLED" {
		t.Fatalf("cancelled=%+v", cancelled)
	}
}

func TestAdmissionConcurrentDistinctScansHaveExactlyOneWinner(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	service := admission.NewService(f.runner, admissionKeyring(t))
	_, payload := confirmedAdmissionTicket(t, ctx, f, "A3")
	const count = 100
	results := make([]string, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(i int) {
			defer wg.Done()
			op := admissionOperationConcurrent(t, ctx, f, uuid.NewString())
			result, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, ScannerUserID: f.userID, IdempotencyOperationID: op})
			errs[i] = err
			results[i] = result.Result
		}(i)
	}
	wg.Wait()
	winners, duplicates := 0, 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("scan %d: %v", i, err)
		}
		if results[i] == "ADMITTED" {
			winners++
		} else if results[i] == "TICKET_ALREADY_ADMITTED" {
			duplicates++
		} else {
			t.Fatalf("scan %d result=%s", i, results[i])
		}
	}
	if winners != 1 || duplicates != 99 {
		t.Fatalf("winners/duplicates=%d/%d", winners, duplicates)
	}
	var active int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admissions WHERE event_id=$1 AND status='ACTIVE'`, f.eventID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active admissions=%d", active)
	}
}

func admissionOperationConcurrent(t *testing.T, ctx context.Context, f fixture, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO idempotency_operations(id,scope_kind,app_user_id,operation_type,idempotency_key,request_hash,execution_state) VALUES($1,'USER',$2,'ADMISSION_SCAN',$3,$4,'IN_PROGRESS')`, id, f.userID, key, []byte(key)); err != nil {
		t.Errorf("operation: %v", err)
	}
	return id
}

func admissionKeyring(t *testing.T) *auth.HMACKeyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	encoded := "1:AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA"
	ring, err := auth.ParseHMACKeyring(1, encoded)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func TestAdmissionManualOverrideRecordsValidationStateAndPreservesHardGuards(t *testing.T) {
	f := newFixture(t)

	ctx, cancel :=
		context.WithTimeout(
			context.Background(),
			60*time.Second,
		)
	defer cancel()

	service :=
		admission.NewService(
			f.runner,
			admissionKeyring(t),
		)

	windowTicket, _ :=
		confirmedAdmissionTicket(
			t,
			ctx,
			f,
			"A1",
		)

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE events
			SET admission_open_at =
			    clock_timestamp() +
			    interval '1 hour'
			WHERE id = $1
		`,
		f.eventID,
	); err != nil {
		t.Fatal(err)
	}

	windowOperation :=
		admissionOperation(
			t,
			ctx,
			f,
			"manual-provenance-window",
		)

	windowResult, err :=
		service.ManualOverride(
			idempotency.WithOperationID(
				ctx,
				windowOperation,
			),
			f.userID,
			admission.ManualOverrideInput{
				EventID:       f.eventID,
				TicketID:      windowTicket,
				GateReference: "gate-supervisor",
				Reason:        "manual identity verification",
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	if windowResult.Result !=
		"MANUAL_OVERRIDE_ADMITTED" {
		t.Fatalf(
			"manual result=%s",
			windowResult.Result,
		)
	}

	var (
		scanEventState      string
		scanTicketState     string
		scanCredentialState string
		scanAdmissionWindow string
		scanCredentialCheck string
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				metadata #>> ARRAY[
					'validation_state',
					'event_state'
				],
				metadata #>> ARRAY[
					'validation_state',
					'ticket_state'
				],
				metadata #>> ARRAY[
					'validation_state',
					'credential_state'
				],
				metadata #>> ARRAY[
					'validation_state',
					'admission_window'
				],
				metadata #>> ARRAY[
					'validation_state',
					'credential_validation'
				]
			FROM scan_attempts
			WHERE id = $1
		`,
		windowResult.ScanAttemptID,
	).Scan(
		&scanEventState,
		&scanTicketState,
		&scanCredentialState,
		&scanAdmissionWindow,
		&scanCredentialCheck,
	); err != nil {
		t.Fatal(err)
	}

	if scanEventState == "" ||
		scanTicketState != "ACTIVE" ||
		scanCredentialState != "ACTIVE" ||
		scanAdmissionWindow !=
			"ADMISSION_NOT_OPEN" ||
		scanCredentialCheck !=
			"NOT_PERFORMED_MANUAL_LOOKUP" {
		t.Fatalf(
			"manual ScanAttempt validation state event=%q ticket=%q credential=%q window=%q credential_check=%q",
			scanEventState,
			scanTicketState,
			scanCredentialState,
			scanAdmissionWindow,
			scanCredentialCheck,
		)
	}

	var (
		auditWindow          string
		auditCredential      string
		auditCredentialCheck string
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				metadata #>> ARRAY[
					'validation_state',
					'admission_window'
				],
				metadata #>> ARRAY[
					'validation_state',
					'credential_state'
				],
				metadata #>> ARRAY[
					'validation_state',
					'credential_validation'
				]
			FROM audit_events
			WHERE entity_id = $1
			  AND operation =
			      'ADMISSION_MANUAL_OVERRIDE'
			ORDER BY occurred_at DESC
			LIMIT 1
		`,
		*windowResult.AdmissionID,
	).Scan(
		&auditWindow,
		&auditCredential,
		&auditCredentialCheck,
	); err != nil {
		t.Fatal(err)
	}

	if auditWindow != "ADMISSION_NOT_OPEN" ||
		auditCredential != "ACTIVE" ||
		auditCredentialCheck !=
			"NOT_PERFORMED_MANUAL_LOOKUP" {
		t.Fatalf(
			"manual audit validation state window=%q credential=%q credential_check=%q",
			auditWindow,
			auditCredential,
			auditCredentialCheck,
		)
	}

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE events
			SET admission_open_at = NULL
			WHERE id = $1
		`,
		f.eventID,
	); err != nil {
		t.Fatal(err)
	}

	revokedTicket, _ :=
		confirmedAdmissionTicket(
			t,
			ctx,
			f,
			"A2",
		)

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE qr_credentials
			SET
				status = 'REVOKED',
				revoked_at =
				    clock_timestamp(),
				superseded_at = NULL
			WHERE ticket_entitlement_id = $1
			  AND status = 'ACTIVE'
		`,
		revokedTicket,
	); err != nil {
		t.Fatal(err)
	}

	_, err =
		service.ManualOverride(
			idempotency.WithOperationID(
				ctx,
				admissionOperation(
					t,
					ctx,
					f,
					"manual-revoked-guard",
				),
			),
			f.userID,
			admission.ManualOverrideInput{
				EventID:       f.eventID,
				TicketID:      revokedTicket,
				GateReference: "gate-supervisor",
				Reason:        "attempt revoked credential override",
			},
		)

	if apiErr, ok := apierror.As(err); !ok ||
		apiErr.Code !=
			apierror.CodeCredentialRevoked {
		t.Fatalf(
			"revoked manual override error=%v want CREDENTIAL_REVOKED",
			err,
		)
	}

	voidTicket, _ :=
		confirmedAdmissionTicket(
			t,
			ctx,
			f,
			"A3",
		)

	if _, err = f.reservation.VoidPartnerTicket(
		ctx,
		f.partnerID,
		voidTicket,
		"void before manual admission",
	); err != nil {
		t.Fatal(err)
	}

	_, err =
		service.ManualOverride(
			idempotency.WithOperationID(
				ctx,
				admissionOperation(
					t,
					ctx,
					f,
					"manual-void-guard",
				),
			),
			f.userID,
			admission.ManualOverrideInput{
				EventID:       f.eventID,
				TicketID:      voidTicket,
				GateReference: "gate-supervisor",
				Reason:        "attempt void Ticket override",
			},
		)

	if apiErr, ok := apierror.As(err); !ok ||
		apiErr.Code !=
			apierror.CodeTicketVoid {
		t.Fatalf(
			"void manual override error=%v want TICKET_VOID",
			err,
		)
	}

	cancelledTicket, _ :=
		confirmedAdmissionTicket(
			t,
			ctx,
			f,
			"A4",
		)

	if _, err = f.pool.Exec(
		ctx,
		`
			UPDATE events
			SET
				state = 'CANCELLED',
				cancelled_at =
				    clock_timestamp()
			WHERE id = $1
		`,
		f.eventID,
	); err != nil {
		t.Fatal(err)
	}

	_, err =
		service.ManualOverride(
			idempotency.WithOperationID(
				ctx,
				admissionOperation(
					t,
					ctx,
					f,
					"manual-cancelled-guard",
				),
			),
			f.userID,
			admission.ManualOverrideInput{
				EventID:       f.eventID,
				TicketID:      cancelledTicket,
				GateReference: "gate-supervisor",
				Reason:        "attempt cancelled Event override",
			},
		)

	if apiErr, ok := apierror.As(err); !ok ||
		apiErr.Code !=
			apierror.CodeEventCancelled {
		t.Fatalf(
			"cancelled manual override error=%v want EVENT_CANCELLED",
			err,
		)
	}
}
