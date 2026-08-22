//go:build integration

package reservation_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func TestM6ExplicitInventoryReleaseIsSourceSafeAndOneTime(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := f.pool.Exec(ctx, `UPDATE event_transaction_policies SET allow_voided_inventory_rerelease=true WHERE event_id=$1`, f.eventID); err != nil {
		t.Fatalf("enable inventory re-release policy: %v", err)
	}
	created, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{
		{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A4"], Quantity: 1, SourceKind: reservation.SourceShared},
		{InventoryKind: reservation.InventoryGA, InventoryID: f.gaMainID, Quantity: 1, SourceKind: reservation.SourceShared},
	}})
	if err != nil {
		t.Fatalf("create Reservation: %v", err)
	}
	checkout, err := f.reservation.BeginCheckout(ctx, f.partnerID, created.Token)
	if err != nil {
		t.Fatalf("begin checkout: %v", err)
	}
	confirmed, err := f.reservation.Confirm(ctx, f.partnerID, created.Token, reservation.ConfirmInput{CheckoutAttemptID: checkout.CheckoutAttemptID})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if len(confirmed.Tickets) != 2 {
		t.Fatalf("tickets=%d want=2", len(confirmed.Tickets))
	}

	for _, ticket := range confirmed.Tickets {
		if _, err = f.reservation.VoidPartnerTicket(ctx, f.partnerID, ticket.TicketID, "customer refund"); err != nil {
			t.Fatalf("void Ticket: %v", err)
		}
	}

	var beforeAvailable, beforeSold int
	if err = f.pool.QueryRow(ctx, `SELECT available_quantity,sold_current_quantity FROM ga_shared_inventory WHERE ga_pool_id=$1`, f.gaMainID).Scan(&beforeAvailable, &beforeSold); err != nil {
		t.Fatalf("read GA counters: %v", err)
	}
	for _, ticket := range confirmed.Tickets {
		if _, err = f.reservation.ReReleasePartnerTicketInventory(ctx, f.partnerID, ticket.TicketID, reservation.InventoryReleaseInput{DestinationKind: reservation.SourceShared, Reason: "refund completed"}); err != nil {
			t.Fatalf("release Ticket inventory: %v", err)
		}
	}
	assertNoActiveClaim(t, ctx, f.pool, f.seatIDs["A4"])
	var afterAvailable, afterSold int
	if err = f.pool.QueryRow(ctx, `SELECT available_quantity,sold_current_quantity FROM ga_shared_inventory WHERE ga_pool_id=$1`, f.gaMainID).Scan(&afterAvailable, &afterSold); err != nil {
		t.Fatalf("read released GA counters: %v", err)
	}
	if afterAvailable != beforeAvailable+1 || afterSold != beforeSold-1 {
		t.Fatalf("GA after release available/sold=%d/%d want=%d/%d", afterAvailable, afterSold, beforeAvailable+1, beforeSold-1)
	}

	_, err = f.reservation.ReReleasePartnerTicketInventory(ctx, f.partnerID, confirmed.Tickets[0].TicketID, reservation.InventoryReleaseInput{DestinationKind: reservation.SourceShared, Reason: "duplicate"})
	if apiErr, ok := apierror.As(err); !ok || apiErr.Code != apierror.CodeInventoryUnavailable {
		t.Fatalf("duplicate release error=%v want INVENTORY_UNAVAILABLE", err)
	}
}

func TestM6ConcurrentInventoryReleaseHasOneWinner(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := f.pool.Exec(ctx, `UPDATE event_transaction_policies SET allow_voided_inventory_rerelease=true WHERE event_id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	created, err := f.reservation.Create(ctx, reservation.CreateInput{EventID: f.eventID, PartnerID: f.partnerID, Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: f.seatIDs["A5"], Quantity: 1, SourceKind: reservation.SourceShared}}})
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
	if _, err = f.reservation.VoidPartnerTicket(ctx, f.partnerID, ticketID, "refund"); err != nil {
		t.Fatal(err)
	}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := range errs {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.reservation.ReReleasePartnerTicketInventory(ctx, f.partnerID, ticketID, reservation.InventoryReleaseInput{DestinationKind: reservation.SourceShared, Reason: "refund completed"})
		}(i)
	}
	wg.Wait()
	successes, conflicts := 0, 0
	for _, releaseErr := range errs {
		if releaseErr == nil {
			successes++
			continue
		}
		if apiErr, ok := apierror.As(releaseErr); ok && apiErr.Code == apierror.CodeInventoryUnavailable {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent release error: %v", releaseErr)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes/conflicts=%d/%d want=1/1", successes, conflicts)
	}
}
