//go:build integration

package reservation_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/adminapi"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reporting"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func TestReportingPreservesCommercialIssuanceAndCapacitySemantics(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	service := reporting.NewService(f.pool)
	issuanceService := allocsvc.NewService(f.runner, reportingKeyring(t))

	held, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A2"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	committing, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A3"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.reservation.BeginCheckout(ctx, f.partnerID, committing.Token); err != nil {
		t.Fatal(err)
	}
	reconciling, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A4"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE reservations SET state='RECONCILING',reconciliation_expires_at=clock_timestamp()+interval '5 minutes' WHERE id=$1`, reconciling.ReservationID); err != nil {
		t.Fatal(err)
	}
	paymentRetry, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A6"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE reservations SET state='PAYMENT_RETRY',payment_retry_expires_at=clock_timestamp()+interval '5 minutes' WHERE id=$1`, paymentRetry.ReservationID); err != nil {
		t.Fatal(err)
	}

	commercial, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A1"], Quantity: 1, SourceKind: reservation.SourceShared}, {InventoryKind: reservation.InventoryGA, InventoryID: f.gaMainID, Quantity: 2, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := f.reservation.BeginCheckout(ctx, f.partnerID, commercial.Token)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.reservation.Confirm(ctx, f.partnerID, commercial.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID, PartnerOrderRef: "Reporting-ORDER", PartnerPaymentRef: "Reporting-PAY"})
	if err != nil {
		t.Fatal(err)
	}

	nonPublicAllocation, err := f.allocation.CreateAllocation(ctx, f.userID, f.eventID, allocsvc.AllocationInput{Mode: "NON_PUBLIC", Purpose: "VIP,\nPress", ReleaseDestinationKind: "SHARED", ReservedUnitIDs: []uuid.UUID{f.seatIDs["A5"]}, GATargets: []allocsvc.GATarget{{PoolID: f.gaMainID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	issuance, err := issuanceService.IssueNonPublic(ctx, f.userID, nonPublicAllocation, allocsvc.NonPublicIssuanceInput{RecipientRef: "private-recipient", Reason: "Accreditation", ReservedUnitIDs: []uuid.UUID{f.seatIDs["A5"]}, GATargets: []allocsvc.GATarget{{PoolID: f.gaMainID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}

	inventory, err := service.Inventory(ctx, f.eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Total.HistoricalSold != 3 || inventory.Total.HistoricalIssued != 2 {
		t.Fatalf("historical sold/issued=%d/%d want=3/2", inventory.Total.HistoricalSold, inventory.Total.HistoricalIssued)
	}
	if inventory.Total.Held != 1 || inventory.Total.Committing != 1 || inventory.Total.PaymentRetry != 1 || inventory.Total.Reconciling != 1 {
		t.Fatalf("reservation dimensions=%+v", inventory.Total)
	}
	if inventory.Total.HistoricalSold == inventory.Total.HistoricalIssued {
		t.Fatal("commercial Sales and non-public issuance were conflated")
	}

	var gaTicket uuid.UUID
	for _, ticket := range confirmed.Tickets {
		var kind string
		if err := f.pool.QueryRow(ctx, `SELECT inventory_kind FROM ticket_entitlements WHERE id=$1`, ticket.TicketID).Scan(&kind); err != nil {
			t.Fatal(err)
		}
		if kind == "GA" {
			gaTicket = ticket.TicketID
			break
		}
	}
	if gaTicket == uuid.Nil {
		t.Fatal("GA Ticket not found")
	}
	if _, err = f.pool.Exec(ctx, `UPDATE event_transaction_policies SET allow_voided_inventory_rerelease=true WHERE event_id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.reservation.VoidPartnerTicket(ctx, f.partnerID, gaTicket, "refund"); err != nil {
		t.Fatal(err)
	}
	voidedBeforeRelease, err := service.Sales(ctx, f.eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if voidedBeforeRelease.VoidedSoldTickets != 1 || voidedBeforeRelease.CurrentSoldCapacity != 3 || voidedBeforeRelease.HistoricalQuantity != 3 {
		t.Fatalf("void incorrectly released/reclassified Sale capacity: %+v", voidedBeforeRelease)
	}
	if _, err = f.reservation.ReReleasePartnerTicketInventory(ctx, f.partnerID, gaTicket, reservation.InventoryReleaseInput{DestinationKind: reservation.SourceShared, Reason: "refund completed"}); err != nil {
		t.Fatal(err)
	}

	after, err := service.Inventory(ctx, f.eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	sales, err := service.Sales(ctx, f.eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after.Total.HistoricalSold != 3 || sales.HistoricalQuantity != 3 {
		t.Fatalf("historical Sale changed after void/re-release: inventory=%d sales=%d", after.Total.HistoricalSold, sales.HistoricalQuantity)
	}
	if sales.VoidedSoldTickets != 1 || sales.CurrentSoldCapacity != 2 {
		t.Fatalf("void/current capacity=%d/%d want=1/2", sales.VoidedSoldTickets, sales.CurrentSoldCapacity)
	}
	if after.Total.CapacityConsumed >= inventory.Total.CapacityConsumed {
		t.Fatalf("current consumption did not decrease: before=%d after=%d", inventory.Total.CapacityConsumed, after.Total.CapacityConsumed)
	}

	partnerSales, err := service.Sales(ctx, f.eventID, &f.otherPartnerID)
	if err != nil {
		t.Fatal(err)
	}
	if partnerSales.SaleCount != 0 || partnerSales.HistoricalQuantity != 0 {
		t.Fatalf("Partner isolation failed: %+v", partnerSales)
	}
	otherInventory, err := service.Inventory(ctx, f.eventID, &f.otherPartnerID)
	if err != nil {
		t.Fatal(err)
	}
	if otherInventory.Total.Held != 0 || otherInventory.Total.Committing != 0 || otherInventory.Total.Reconciling != 0 || otherInventory.Total.HistoricalSold != 0 {
		t.Fatalf("Partner inferred another Partner's obligations: %+v", otherInventory.Total)
	}
	_ = held
	_ = issuance
}

func TestInventoryReportReturnsZerosWithoutMaterializedInventory(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var venueID uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT venue_id FROM events WHERE id=$1`, f.eventID).Scan(&venueID); err != nil {
		t.Fatal(err)
	}
	emptyEventID, err := eventsvc.NewService(f.runner).Create(ctx, f.userID, eventsvc.CreateInput{
		VenueID:      venueID,
		Name:         "Unconfigured reporting event " + uuid.NewString(),
		TimezoneName: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := reporting.NewService(f.pool).Inventory(ctx, emptyEventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Event.ID != publicid.Encode(publicid.Event, emptyEventID) || report.Event.State != "DRAFT" {
		t.Fatalf("unexpected Event context: %+v", report.Event)
	}
	if report.Reserved != (reporting.InventoryDimensions{}) || report.GA != (reporting.InventoryDimensions{}) || report.Total != (reporting.InventoryDimensions{}) {
		t.Fatalf("empty Event inventory was not all zero: %+v", report)
	}
}

func TestReportingAuditPaginationAccreditationCSVAndCancellationHistory(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	service := reporting.NewService(f.pool)
	issuanceService := allocsvc.NewService(f.runner, reportingKeyring(t))
	commercial, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A1"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := f.reservation.BeginCheckout(ctx, f.partnerID, commercial.Token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.reservation.Confirm(ctx, f.partnerID, commercial.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID}); err != nil {
		t.Fatal(err)
	}
	allocationID, err := f.allocation.CreateAllocation(ctx, f.userID, f.eventID, allocsvc.AllocationInput{Mode: "NON_PUBLIC", Purpose: "VIP,\n\"Press\"", ReleaseDestinationKind: "SHARED", ReservedUnitIDs: []uuid.UUID{f.seatIDs["A6"]}})
	if err != nil {
		t.Fatal(err)
	}
	issuance, err := issuanceService.IssueNonPublic(ctx, f.userID, allocationID, allocsvc.NonPublicIssuanceInput{Reason: "Reporting export", ReservedUnitIDs: []uuid.UUID{f.seatIDs["A6"]}})
	if err != nil {
		t.Fatal(err)
	}
	ticketID := issuance.Tickets[0].TicketID
	if _, err = f.pool.Exec(ctx, `INSERT INTO ticket_attendee_details(ticket_entitlement_id,partner_attendee_ref,display_name,accreditation_data) VALUES($1,$2,$3,$4)`, ticketID, "attendee,1", "Ada \"A\"\nLovelace", json.RawMessage(`{"badge":"VIP,Press","secret":"must-not-export"}`)); err != nil {
		t.Fatal(err)
	}
	insertReportingAdmissions(t, ctx, f, ticketID)
	admissions, err := service.Admissions(ctx, f.eventID)
	if err != nil {
		t.Fatal(err)
	}
	if admissions.ActiveAdmissions != 1 || admissions.ReversedAdmissions != 1 || admissions.ScanOutcomes["ADMITTED"] != 1 || admissions.ScanOutcomes["MANUAL_OVERRIDE_ADMITTED"] != 1 {
		t.Fatalf("admission distinctions lost: %+v", admissions)
	}

	page1, err := service.Audit(ctx, f.eventID, reporting.AuditFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || page1.NextCursor == nil {
		t.Fatalf("first audit page=%+v", page1)
	}
	page2, err := service.Audit(ctx, f.eventID, reporting.AuditFilter{Limit: 2, Cursor: *page1.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) == 0 {
		t.Fatal("second audit page is empty")
	}
	if page1.Items[1].ID == page2.Items[0].ID {
		t.Fatal("stable pagination repeated boundary item")
	}
	partnerPage, err := service.Audit(ctx, f.eventID, reporting.AuditFilter{Limit: 100, PartnerID: &f.otherPartnerID})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range partnerPage.Items {
		if strings.Contains(item.Operation, "ISSUANCE") || strings.Contains(item.Operation, "ALLOCATION_CREATED") {
			t.Fatalf("other Partner saw private allocation/issuance activity: %+v", item)
		}
	}
	unauthorizedID := uuid.New()
	unauthorizedSubject := uuid.NewString()
	if _, err = f.pool.Exec(ctx, `INSERT INTO app_users(id,auth_provider,auth_subject,display_name,state) VALUES($1,'reporting',$2,'Unauthorized Reporting Viewer','ACTIVE')`, unauthorizedID, unauthorizedSubject); err != nil {
		t.Fatal(err)
	}
	adminHandler, err := adminapi.New(adminapi.Dependencies{Database: f.pool, Transactions: f.runner, HumanAuth: func(context.Context, string) (auth.HumanPrincipal, error) {
		return auth.HumanPrincipal{Provider: "reporting", Subject: unauthorizedSubject}, nil
	}, VenueService: venuesvc.NewService(f.runner), EventService: eventsvc.NewService(f.runner), PartnerService: partnersvc.NewService(f.runner), ReportingService: service})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/events/"+publicid.Encode(publicid.Event, f.eventID)+"/audit", nil)
	request.Header.Set("Authorization", "Bearer test")
	response := httptest.NewRecorder()
	adminHandler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthorized audit status=%d body=%s", response.Code, response.Body.String())
	}

	var output bytes.Buffer
	var businessRowsBefore int64
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM ticket_entitlements WHERE event_id=$1)+(SELECT COUNT(*) FROM admissions WHERE event_id=$1)+(SELECT COUNT(*) FROM audit_events WHERE event_id=$1)`, f.eventID).Scan(&businessRowsBefore); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.WriteAccreditationCSV(ctx, f.eventID, &output)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GeneratedAt.IsZero() || snapshot.Event.ID == "" {
		t.Fatalf("invalid snapshot context: %+v", snapshot)
	}
	records, err := csv.NewReader(strings.NewReader(output.String())).ReadAll()
	if err != nil {
		t.Fatalf("CSV escaping failed: %v\n%s", err, output.String())
	}
	if len(records) < 2 {
		t.Fatalf("CSV rows=%d", len(records))
	}
	if strings.Contains(strings.ToLower(output.String()), "must-not-export") || strings.Contains(strings.ToLower(output.String()), "qr_") {
		t.Fatalf("secret/QR material leaked: %s", output.String())
	}
	if !strings.Contains(output.String(), "Ada \"\"A\"\"") {
		t.Fatalf("quoted attendee value was not escaped: %s", output.String())
	}
	var businessRowsAfter int64
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM ticket_entitlements WHERE event_id=$1)+(SELECT COUNT(*) FROM admissions WHERE event_id=$1)+(SELECT COUNT(*) FROM audit_events WHERE event_id=$1)`, f.eventID).Scan(&businessRowsAfter); err != nil {
		t.Fatal(err)
	}
	if businessRowsAfter != businessRowsBefore {
		t.Fatalf("export mutated business history: before=%d after=%d", businessRowsBefore, businessRowsAfter)
	}

	before, err := service.Inventory(ctx, f.eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE events SET state='CANCELLED',cancelled_at=clock_timestamp() WHERE id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	after, err := service.Inventory(ctx, f.eventID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after.Event.State != "CANCELLED" || after.Total.HistoricalIssued != before.Total.HistoricalIssued || after.Total.HistoricalSold != before.Total.HistoricalSold || after.Total.HistoricalSold != 1 {
		t.Fatalf("cancellation erased history: before=%+v after=%+v", before.Total, after.Total)
	}
	afterCancellationAudit, err := service.Audit(ctx, f.eventID, reporting.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	foundHistoricalAudit := false
	for _, item := range afterCancellationAudit.Items {
		if item.ID == page1.Items[0].ID {
			foundHistoricalAudit = true
		}
	}
	if !foundHistoricalAudit {
		t.Fatal("cancellation erased pre-existing audit history")
	}
}

func TestReportingMetricsAlertWithoutBecomingReservationAuthority(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	service := reporting.NewService(f.pool)
	created, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A7"], Quantity: 1, SourceKind: reservation.SourceShared}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE reservations SET state='RECONCILING',reconciliation_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, created.ReservationID); err != nil {
		t.Fatal(err)
	}
	metrics, err := service.Metrics(ctx, f.eventID, reporting.NewObserver())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.OverdueReconciliations != 1 || metrics.ConfirmedSales != 0 || metrics.ReservationStates["RECONCILING"] != 1 {
		t.Fatalf("unexpected operational metrics: %+v", metrics)
	}
	found := false
	for _, alert := range metrics.Alerts {
		if alert.Code == "RECONCILIATION_OVERDUE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing reconciliation alert: %+v", metrics.Alerts)
	}
	var state string
	if err := f.pool.QueryRow(ctx, `SELECT state FROM reservations WHERE id=$1`, created.ReservationID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RECONCILING" {
		t.Fatalf("metrics mutated authoritative state to %s", state)
	}
}

func insertReportingAdmissions(t *testing.T, ctx context.Context, f fixture, ticketID uuid.UUID) {
	t.Helper()
	for index, status := range []string{"REVERSED", "ACTIVE"} {
		opID, scanID, admissionID := uuid.New(), uuid.New(), uuid.New()
		key := uuid.NewString()
		if _, err := f.pool.Exec(ctx, `INSERT INTO idempotency_operations(id,scope_kind,app_user_id,operation_type,idempotency_key,request_hash,execution_state,created_at,completed_at) VALUES($1,'USER',$2,'REPORTING_TEST_SCAN',$3,$4,'SUCCEEDED',clock_timestamp(),clock_timestamp())`, opID, f.userID, key, []byte(key)); err != nil {
			t.Fatal(err)
		}
		result := "ADMITTED"
		if index == 1 {
			result = "MANUAL_OVERRIDE_ADMITTED"
		}
		if _, err := f.pool.Exec(ctx, `INSERT INTO scan_attempts(id,event_id,scanner_user_id,ticket_entitlement_id,idempotency_operation_id,result,occurred_at) VALUES($1,$2,$3,$4,$5,$6,clock_timestamp())`, scanID, f.eventID, f.userID, ticketID, opID, result); err != nil {
			t.Fatal(err)
		}
		if status == "REVERSED" {
			if _, err := f.pool.Exec(ctx, `INSERT INTO admissions(id,event_id,ticket_entitlement_id,scan_attempt_id,status,admitted_at,reversed_at,reversal_reason,reversed_by_user_id) VALUES($1,$2,$3,$4,'REVERSED',clock_timestamp(),clock_timestamp(),'correction',$5)`, admissionID, f.eventID, ticketID, scanID, f.userID); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := f.pool.Exec(ctx, `INSERT INTO admissions(id,event_id,ticket_entitlement_id,scan_attempt_id,status,admitted_at) VALUES($1,$2,$3,$4,'ACTIVE',clock_timestamp())`, admissionID, f.eventID, ticketID, scanID); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func reportingKeyring(t *testing.T) *auth.HMACKeyring {
	t.Helper()
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index + 31)
	}
	keyring, err := auth.ParseHMACKeyring(1, "1:"+base64.RawURLEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return keyring
}
