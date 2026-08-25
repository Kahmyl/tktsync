//go:build integration

package reservation_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/admission"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/realtimeapi"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func TestHoldVersusBlockHasOneAuthoritativeWinner(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := make(chan struct{})
	var hold *reservation.Created
	var holdErr, blockErr error
	var blockID uuid.UUID
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		created, err := f.reservation.Create(ctx, reservation.CreateInput{
			EventID: f.eventID, PartnerID: f.partnerID,
			Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A1"], Quantity: 1, SourceKind: reservation.SourceShared}},
		})
		holdErr = err
		if err == nil {
			hold = &created
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		blockID, blockErr = f.allocation.CreateBlock(ctx, f.userID, f.eventID, allocsvc.BlockInput{
			Purpose: "OPERATIONS", Reason: "Release contention", ReservedUnitIDs: []uuid.UUID{f.seatIDs["A1"]},
		})
	}()
	close(start)
	wg.Wait()

	if (holdErr == nil) == (blockErr == nil) {
		t.Fatalf("hold/block outcomes must contain exactly one success: hold=%v block=%v", holdErr, blockErr)
	}
	var claimType string
	if err := f.pool.QueryRow(ctx, `SELECT claim_type FROM reserved_inventory_claims WHERE reserved_inventory_unit_id=$1 AND ended_at IS NULL`, f.seatIDs["A1"]).Scan(&claimType); err != nil {
		t.Fatal(err)
	}
	if holdErr == nil {
		if claimType != "RESERVATION" || hold == nil {
			t.Fatalf("hold won but claim=%s hold=%v", claimType, hold)
		}
		assertCode(t, blockErr, apierror.CodeInventoryUnavailable)
	} else {
		if claimType != "BLOCK" || blockID == uuid.Nil {
			t.Fatalf("block won but claim=%s block=%s", claimType, blockID)
		}
		assertCode(t, holdErr, apierror.CodeInventoryUnavailable)
	}
	var activeClaims int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM reserved_inventory_claims WHERE reserved_inventory_unit_id=$1 AND ended_at IS NULL`, f.seatIDs["A1"]).Scan(&activeClaims); err != nil {
		t.Fatal(err)
	}
	if activeClaims != 1 {
		t.Fatalf("active claims=%d want=1", activeClaims)
	}
}

func TestPartnerDisableVersusAcquisitionUsesPartnerGate(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := make(chan struct{})
	var created reservation.Created
	var holdErr, disableErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		created, holdErr = f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A1"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	}()
	go func() {
		defer wg.Done()
		<-start
		disableErr = partnersvc.NewService(f.runner).SetEnabled(ctx, f.userID, f.partnerID, false)
	}()
	close(start)
	wg.Wait()
	if disableErr != nil {
		t.Fatalf("disable Partner: %v", disableErr)
	}
	if holdErr != nil {
		assertCode(t, holdErr, apierror.CodePartnerDisabled)
	} else if created.ReservationID == uuid.Nil {
		t.Fatal("successful pre-disable hold has no identity")
	}
	_, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A2"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	assertCode(t, err, apierror.CodePartnerDisabled)
}

func TestConfirmationVersusCancellationOrdering(t *testing.T) {
	t.Run("confirmation-before-cancellation-remains-history", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		confirmedAdmissionTicket(t, ctx, f, "A1")
		if err := eventsvc.NewService(f.runner).CancelEvent(ctx, f.userID, f.eventID, "Release ordered cancellation"); err != nil {
			t.Fatal(err)
		}
		assertEventSaleCounts(t, ctx, f, "CANCELLED", 1)
	})

	t.Run("cancellation-before-confirmation-rejects-sale", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		created, checkout := checkoutRelease(t, ctx, f, "A1")
		if err := eventsvc.NewService(f.runner).CancelEvent(ctx, f.userID, f.eventID, "Release cancellation first"); err != nil {
			t.Fatal(err)
		}
		_, err := f.reservation.Confirm(ctx, f.partnerID, created.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID})
		assertCode(t, err, apierror.CodeEventCancelled)
		assertEventSaleCounts(t, ctx, f, "CANCELLED", 0)
	})

	t.Run("concurrent-commands-serialize-on-event-gate", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		created, checkout := checkoutRelease(t, ctx, f, "A1")
		start := make(chan struct{})
		var confirmErr, cancelErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, confirmErr = f.reservation.Confirm(ctx, f.partnerID, created.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID})
		}()
		go func() {
			defer wg.Done()
			<-start
			cancelErr = eventsvc.NewService(f.runner).CancelEvent(ctx, f.userID, f.eventID, "Release concurrent cancellation")
		}()
		close(start)
		wg.Wait()
		if cancelErr != nil {
			t.Fatalf("cancellation: %v", cancelErr)
		}
		var saleCount int
		if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM sales WHERE event_id=$1`, f.eventID).Scan(&saleCount); err != nil {
			t.Fatal(err)
		}
		if confirmErr == nil && saleCount != 1 {
			t.Fatalf("successful confirmation sale count=%d", saleCount)
		}
		if confirmErr != nil {
			assertCode(t, confirmErr, apierror.CodeEventCancelled)
			if saleCount != 0 {
				t.Fatalf("rejected confirmation sale count=%d", saleCount)
			}
		}
		assertEventSaleCounts(t, ctx, f, "CANCELLED", saleCount)
	})
}

func TestConfirmationExpiryGuardUsesPostLockDatabaseTime(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	created, checkout := checkoutRelease(t, ctx, f, "A1")
	if _, err := f.reservation.MarkPaymentUncertain(ctx, f.partnerID, created.Token, "release-payment", "TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE reservations SET reconciliation_expires_at=clock_timestamp()+interval '200 milliseconds' WHERE id=$1`, created.ReservationID); err != nil {
		t.Fatal(err)
	}
	lockTx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lockTx.Exec(ctx, `SELECT id FROM reservations WHERE id=$1 FOR UPDATE`, created.ReservationID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, confirmErr := f.reservation.Confirm(ctx, f.partnerID, created.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID})
		done <- confirmErr
	}()
	time.Sleep(300 * time.Millisecond)
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertCode(t, <-done, apierror.CodeReconciliationExpired)
	var state string
	var sales int
	if err := f.pool.QueryRow(ctx, `SELECT state,(SELECT COUNT(*) FROM sales WHERE reservation_id=$1) FROM reservations WHERE id=$1`, created.ReservationID).Scan(&state, &sales); err != nil {
		t.Fatal(err)
	}
	if state != "EXPIRED" || sales != 0 {
		t.Fatalf("state/sales=%s/%d want EXPIRED/0", state, sales)
	}
}

func TestCancellationCleanupIsBoundedAndStateAware(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	held := holdRelease(t, ctx, f, "A1")
	retry, _ := checkoutRelease(t, ctx, f, "A2")
	if _, err := f.reservation.PaymentFailed(ctx, f.partnerID, retry.Token, "release-declined", "DECLINED"); err != nil {
		t.Fatal(err)
	}
	committing, _ := checkoutRelease(t, ctx, f, "A3")
	reconciling, _ := checkoutRelease(t, ctx, f, "A4")
	if _, err := f.reservation.MarkPaymentUncertain(ctx, f.partnerID, reconciling.Token, "release-uncertain", "TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	confirmedTicket, _ := confirmedAdmissionTicket(t, ctx, f, "A5")
	if err := eventsvc.NewService(f.runner).CancelEvent(ctx, f.userID, f.eventID, "Release cleanup"); err != nil {
		t.Fatal(err)
	}
	materializer := reservation.NewMaterializer(f.pool, f.reservation, 10000)
	for range 3 {
		if err := materializer.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{held.ReservationID, retry.ReservationID} {
		var state, reason string
		if err := f.pool.QueryRow(ctx, `SELECT state,COALESCE(terminal_reason,'') FROM reservations WHERE id=$1`, id).Scan(&state, &reason); err != nil {
			t.Fatal(err)
		}
		if state != "EXPIRED" || reason != "EVENT_CANCELLED" {
			t.Fatalf("cancelled Reservation %s state/reason=%s/%s", id, state, reason)
		}
	}
	var committingState, committingOutcome string
	if err := f.pool.QueryRow(ctx, `SELECT r.state,COALESCE(ca.partner_outcome_code,'') FROM reservations r JOIN checkout_attempts ca ON ca.reservation_id=r.id WHERE r.id=$1 ORDER BY ca.attempt_number DESC LIMIT 1`, committing.ReservationID).Scan(&committingState, &committingOutcome); err != nil {
		t.Fatal(err)
	}
	if committingState != "RECONCILING" || committingOutcome != "EVENT_CANCELLED" {
		t.Fatalf("committing cancellation state/outcome=%s/%s", committingState, committingOutcome)
	}
	var reconcilingState string
	if err := f.pool.QueryRow(ctx, `SELECT state FROM reservations WHERE id=$1`, reconciling.ReservationID).Scan(&reconcilingState); err != nil {
		t.Fatal(err)
	}
	if reconcilingState != "RECONCILING" {
		t.Fatalf("existing reconciliation state=%s", reconcilingState)
	}
	var ticketState string
	var saleCount int
	if err := f.pool.QueryRow(ctx, `SELECT status,(SELECT COUNT(*) FROM sales WHERE event_id=$2) FROM ticket_entitlements WHERE id=$1`, confirmedTicket, f.eventID).Scan(&ticketState, &saleCount); err != nil {
		t.Fatal(err)
	}
	if ticketState != "ACTIVE" || saleCount != 1 {
		t.Fatalf("confirmed history ticket/sales=%s/%d", ticketState, saleCount)
	}
}

func TestVoidAndReissueVersusScanOrdering(t *testing.T) {
	t.Run("void-versus-scan", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ticketID, payload := confirmedAdmissionTicket(t, ctx, f, "A1")
		scanner := admission.NewService(f.runner, admissionKeyring(t))
		start := make(chan struct{})
		var scan admission.ScanResult
		var scanErr, voidErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			scan, scanErr = scanner.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: payload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperationConcurrent(t, ctx, f, uuid.NewString())})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, voidErr = f.reservation.VoidPartnerTicket(ctx, f.partnerID, ticketID, "Release void race")
		}()
		close(start)
		wg.Wait()
		if scanErr != nil || voidErr != nil {
			t.Fatalf("scan/void errors=%v/%v", scanErr, voidErr)
		}
		if scan.Result != "ADMITTED" && scan.Result != "TICKET_VOID" {
			t.Fatalf("scan result=%s", scan.Result)
		}
		var ticketState string
		var sales, admissions int
		if err := f.pool.QueryRow(ctx, `SELECT status,(SELECT COUNT(*) FROM sales WHERE event_id=$2),(SELECT COUNT(*) FROM admissions WHERE ticket_entitlement_id=$1 AND status='ACTIVE') FROM ticket_entitlements WHERE id=$1`, ticketID, f.eventID).Scan(&ticketState, &sales, &admissions); err != nil {
			t.Fatal(err)
		}
		if ticketState != "VOIDED" || sales != 1 || (scan.Result == "ADMITTED") != (admissions == 1) {
			t.Fatalf("result/state/sales/admissions=%s/%s/%d/%d", scan.Result, ticketState, sales, admissions)
		}
	})

	t.Run("reissue-versus-old-credential-scan", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ticketID, oldPayload := confirmedAdmissionTicket(t, ctx, f, "A1")
		scanner := admission.NewService(f.runner, admissionKeyring(t))
		start := make(chan struct{})
		var scan admission.ScanResult
		var scanErr, reissueErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			scan, scanErr = scanner.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: oldPayload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperationConcurrent(t, ctx, f, uuid.NewString())})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, reissueErr = f.reservation.ReissuePartnerCredential(ctx, f.partnerID, ticketID)
		}()
		close(start)
		wg.Wait()
		if scanErr != nil || reissueErr != nil {
			t.Fatalf("scan/reissue errors=%v/%v", scanErr, reissueErr)
		}
		if scan.Result != "ADMITTED" && scan.Result != "CREDENTIAL_SUPERSEDED" {
			t.Fatalf("old credential scan result=%s", scan.Result)
		}
		active, err := f.reservation.RecoverActiveCredentialForPartner(ctx, f.partnerID, ticketID)
		if err != nil {
			t.Fatal(err)
		}
		second, err := scanner.ValidateAndAdmit(ctx, admission.ScanInput{EventID: f.eventID, Credential: active.QRPayload, ScannerUserID: f.userID, IdempotencyOperationID: admissionOperation(t, ctx, f, uuid.NewString())})
		if err != nil {
			t.Fatal(err)
		}
		expected := "ADMITTED"
		if scan.Result == "ADMITTED" {
			expected = "TICKET_ALREADY_ADMITTED"
		}
		if second.Result != expected {
			t.Fatalf("old/new results=%s/%s want second=%s", scan.Result, second.Result, expected)
		}
		var activeAdmissions int
		if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admissions WHERE ticket_entitlement_id=$1 AND status='ACTIVE'`, ticketID).Scan(&activeAdmissions); err != nil {
			t.Fatal(err)
		}
		if activeAdmissions != 1 {
			t.Fatalf("active admissions=%d want=1", activeAdmissions)
		}
	})
}

func TestRealtimeConcurrentConnectionsRemainAdvisory(t *testing.T) {
	f := newFixture(t)
	handler := realtimeapi.New(f.pool, func(context.Context, string) (auth.HumanPrincipal, error) {
		return auth.HumanPrincipal{Provider: "reservation", Subject: f.userSubject}, nil
	}, nil, realtimeapi.NewHub(32))
	server := httptest.NewServer(handler)
	defer server.Close()
	const connections = 50
	start := time.Now()
	errs := make(chan error, connections)
	var wg sync.WaitGroup
	wg.Add(connections)
	for i := 0; i < connections; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/realtime/stream?audience=admin&event_id="+publicid.Encode(publicid.Event, f.eventID), nil)
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set("Authorization", "Bearer release-realtime")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				errs <- apierror.New(apierror.CodeInternal, response.Status)
				return
			}
			scanner := bufio.NewScanner(response.Body)
			for scanner.Scan() {
				if scanner.Text() == "event: resync" {
					errs <- nil
					return
				}
			}
			if err := scanner.Err(); err != nil {
				errs <- err
			} else {
				errs <- io.ErrUnexpectedEOF
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var reservationCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM reservations WHERE event_id=$1`, f.eventID).Scan(&reservationCount); err != nil {
		t.Fatal(err)
	}
	if reservationCount != 0 {
		t.Fatalf("realtime connections mutated Reservations: %d", reservationCount)
	}
	t.Logf("RELEASE_LOAD_MEASUREMENT realtime_connections=%d elapsed=%s", connections, time.Since(start))
}

func checkoutRelease(t *testing.T, ctx context.Context, f fixture, seat string) (reservation.Created, reservation.Checkout) {
	t.Helper()
	created, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs[seat], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := f.reservation.BeginCheckout(ctx, f.partnerID, created.Token)
	if err != nil {
		t.Fatal(err)
	}
	return created, checkout
}

func holdRelease(t *testing.T, ctx context.Context, f fixture, seat string) reservation.Created {
	t.Helper()
	created, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs[seat], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func assertEventSaleCounts(t *testing.T, ctx context.Context, f fixture, wantState string, wantSales int) {
	t.Helper()
	var state string
	var sales int
	if err := f.pool.QueryRow(ctx, `SELECT state,(SELECT COUNT(*) FROM sales WHERE event_id=$1) FROM events WHERE id=$1`, f.eventID).Scan(&state, &sales); err != nil {
		t.Fatal(err)
	}
	if state != wantState || sales != wantSales {
		t.Fatalf("event state/sales=%s/%d want=%s/%d", state, sales, wantState, wantSales)
	}
}

func assertCode(t *testing.T, err error, want apierror.Code) {
	t.Helper()
	apiErr, ok := apierror.As(err)
	if !ok || apiErr.Code != want {
		t.Fatalf("error=%v want code=%s", err, want)
	}
}
