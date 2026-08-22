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
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func TestM7AdmissionHTTPIdempotencyReplayAndConflict(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, payload := confirmedM7Ticket(t, ctx, f, "A7")
	service := admission.NewService(f.runner, m7Keyring(t))
	handler, err := admissionapi.New(admissionapi.Dependencies{Database: f.pool, Transactions: f.runner, HumanAuth: func(context.Context, string) (auth.HumanPrincipal, error) {
		return auth.HumanPrincipal{Provider: "m5", Subject: f.userSubject}, nil
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

func publicEventID(id uuid.UUID) string { return publicid.Encode(publicid.Event, id) }

func confirmedM7Ticket(t *testing.T, ctx context.Context, f fixture, seat string) (uuid.UUID, string) {
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
	credential, err := f.reservation.RecoverActiveCredential(ctx, f.partnerID, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	return ticketID, credential.QRPayload
}

func m7Operation(t *testing.T, ctx context.Context, f fixture, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.pool.Exec(ctx, `INSERT INTO idempotency_operations(id,scope_kind,app_user_id,operation_type,idempotency_key,request_hash,execution_state) VALUES($1,'USER',$2,'ADMISSION_SCAN',$3,$4,'IN_PROGRESS')`, id, f.userID, key, []byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestM7AdmissionLifecycleAndCredentialStates(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	keyring := m7Keyring(t)
	service := admission.NewService(f.runner, keyring)
	ticketID, payload := confirmedM7Ticket(t, ctx, f, "A1")
	first, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, GateReference: "gate-1", ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "first")})
	if err != nil {
		t.Fatal(err)
	}
	if first.Result != "ADMITTED" || first.AdmissionID == nil {
		t.Fatalf("first=%+v", first)
	}
	second, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, GateReference: "gate-2", ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "second")})
	if err != nil {
		t.Fatal(err)
	}
	if second.Result != "TICKET_ALREADY_ADMITTED" || second.PreviousAdmittedAt == nil {
		t.Fatalf("second=%+v", second)
	}
	reversed, err := service.Reverse(ctx, f.userID, *first.AdmissionID, "operator correction")
	if err != nil {
		t.Fatal(err)
	}
	if reversed.TicketID != ticketID {
		t.Fatalf("reversal=%+v", reversed)
	}
	third, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, GateReference: "gate-3", ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "third")})
	if err != nil {
		t.Fatal(err)
	}
	if third.Result != "ADMITTED" {
		t.Fatalf("scan after reversal=%+v", third)
	}

	malformed, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: "not-a-credential", ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "malformed")})
	if err != nil {
		t.Fatal(err)
	}
	if malformed.Result != "TICKET_INVALID" {
		t.Fatalf("malformed=%+v", malformed)
	}

	other := newFixture(t)
	wrong, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: other.eventID, Credential: payload, ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, other, "wrong-event")})
	if err != nil {
		t.Fatal(err)
	}
	if wrong.Result != "WRONG_EVENT" {
		t.Fatalf("wrong=%+v", wrong)
	}

	supersededTicket, oldPayload := confirmedM7Ticket(t, ctx, f, "A2")
	if _, err = f.reservation.ReissuePartnerCredential(ctx, f.partnerID, supersededTicket); err != nil {
		t.Fatal(err)
	}
	superseded, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: oldPayload, ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "superseded")})
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Result != "CREDENTIAL_SUPERSEDED" {
		t.Fatalf("superseded=%+v", superseded)
	}

	revokedTicket, revokedPayload := confirmedM7Ticket(t, ctx, f, "A4")
	if _, err = f.pool.Exec(ctx, `UPDATE qr_credentials SET status='REVOKED',revoked_at=clock_timestamp() WHERE ticket_entitlement_id=$1 AND status='ACTIVE'`, revokedTicket); err != nil {
		t.Fatal(err)
	}
	revoked, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: revokedPayload, ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "revoked")})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Result != "CREDENTIAL_REVOKED" {
		t.Fatalf("revoked=%+v", revoked)
	}

	voidTicket, voidPayload := confirmedM7Ticket(t, ctx, f, "A5")
	if _, err = f.reservation.VoidPartnerTicket(ctx, f.partnerID, voidTicket, "fraud"); err != nil {
		t.Fatal(err)
	}
	voided, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: voidPayload, ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "void")})
	if err != nil {
		t.Fatal(err)
	}
	if voided.Result != "TICKET_VOID" {
		t.Fatalf("voided=%+v", voided)
	}

	_, closedPayload := confirmedM7Ticket(t, ctx, f, "A6")
	if _, err = f.pool.Exec(ctx, `UPDATE events SET admission_open_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	closed, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: closedPayload, ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "not-open")})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Result != "ADMISSION_NOT_OPEN" {
		t.Fatalf("closed=%+v", closed)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE events SET admission_open_at=NULL WHERE id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	manualTicket, _ := confirmedM7Ticket(t, ctx, f, "A7")
	manualOperation := m7Operation(t, ctx, f, "manual")
	manual, err := service.ManualOverride(idempotency.WithOperationID(ctx, manualOperation), f.userID, admission.ManualOverrideInput{EventID: f.eventID, TicketID: manualTicket, GateReference: "supervisor", Reason: "damaged printed credential"})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Result != "MANUAL_OVERRIDE_ADMITTED" || manual.AdmissionID == nil {
		t.Fatalf("manual=%+v", manual)
	}

	unauthorizedUser := uuid.New()
	if _, err = f.pool.Exec(ctx, `INSERT INTO app_users(id,auth_provider,auth_subject,display_name,state,created_at,updated_at) VALUES($1,'m7',$2,'Unauthorized','ACTIVE',clock_timestamp(),clock_timestamp())`, unauthorizedUser, uuid.NewString()); err != nil {
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
	cancelled, err := service.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, ScannerUserID: f.userID, IdempotencyOperationID: m7Operation(t, ctx, f, "cancelled")})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Result != "EVENT_CANCELLED" {
		t.Fatalf("cancelled=%+v", cancelled)
	}
}

func TestM7ConcurrentDistinctScansHaveExactlyOneWinner(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	service := admission.NewService(f.runner, m7Keyring(t))
	_, payload := confirmedM7Ticket(t, ctx, f, "A3")
	const count = 100
	results := make([]string, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(i int) {
			defer wg.Done()
			op := m7OperationConcurrent(t, ctx, f, uuid.NewString())
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

func m7OperationConcurrent(t *testing.T, ctx context.Context, f fixture, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO idempotency_operations(id,scope_kind,app_user_id,operation_type,idempotency_key,request_hash,execution_state) VALUES($1,'USER',$2,'ADMISSION_SCAN',$3,$4,'IN_PROGRESS')`, id, f.userID, key, []byte(key)); err != nil {
		t.Errorf("operation: %v", err)
	}
	return id
}

func m7Keyring(t *testing.T) *auth.HMACKeyring {
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
