//go:build integration

package reservation_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func TestM6ConfirmationCreatesSaleTicketsAndQRCredentials(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	var soldBefore int

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT sold_current_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		f.gaMainID,
	).Scan(
		&soldBefore,
	); err != nil {
		t.Fatalf(
			"read GA sold before confirmation: %v",
			err,
		)
	}

	created, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:         f.eventID,
				PartnerID:       f.partnerID,
				PartnerOrderRef: "cart-before-confirm",
				Items: []reservation.ItemInput{
					{
						InventoryKind: reservation.InventoryReserved,
						InventoryID:   f.seatIDs["A1"],
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
					{
						InventoryKind: reservation.InventoryGA,
						InventoryID:   f.gaMainID,
						Quantity:      2,
						SourceKind:    reservation.SourceShared,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create confirmation Reservation: %v",
			err,
		)
	}

	checkout, err :=
		f.reservation.BeginCheckout(
			ctx,
			f.partnerID,
			created.Token,
		)
	if err != nil {
		t.Fatalf(
			"begin confirmation checkout: %v",
			err,
		)
	}

	confirmed, err :=
		f.reservation.Confirm(
			ctx,
			f.partnerID,
			created.Token,
			reservation.ConfirmInput{
				CheckoutAttemptID: checkout.
					CheckoutAttemptID,
				PartnerOrderRef:   "ORD-M6-1",
				PartnerPaymentRef: "PAY-M6-1",
			},
		)
	if err != nil {
		t.Fatalf(
			"confirm Reservation: %v",
			err,
		)
	}

	if confirmed.State != "CONFIRMED" ||
		confirmed.SaleID == uuid.Nil ||
		confirmed.ReservationID !=
			created.ReservationID {
		t.Fatalf(
			"invalid confirmation result: %+v",
			confirmed,
		)
	}

	if len(confirmed.Tickets) != 3 {
		t.Fatalf(
			"tickets=%d want=3",
			len(confirmed.Tickets),
		)
	}

	var reservationState string

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM reservations
			WHERE id = $1
		`,
		created.ReservationID,
	).Scan(
		&reservationState,
	); err != nil {
		t.Fatalf(
			"read confirmed Reservation: %v",
			err,
		)
	}

	if reservationState != "CONFIRMED" {
		t.Fatalf(
			"Reservation state=%s want=CONFIRMED",
			reservationState,
		)
	}

	var (
		saleCount        int
		saleOrderRef     *string
		salePaymentRef   *string
		saleCurrency     string
		saleConfirmedAt  time.Time
		saleItemCount    int
		saleItemQuantity int
		ticketCount      int
		credentialCount  int
		checkoutState    string
		gaActiveReserved int
		gaSold           int
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*),
				MIN(partner_order_ref),
				MIN(partner_payment_ref),
				MIN(currency),
				MIN(confirmed_at)
			FROM sales
			WHERE reservation_id = $1
		`,
		created.ReservationID,
	).Scan(
		&saleCount,
		&saleOrderRef,
		&salePaymentRef,
		&saleCurrency,
		&saleConfirmedAt,
	); err != nil {
		t.Fatalf(
			"read Sale: %v",
			err,
		)
	}

	if saleCount != 1 ||
		saleOrderRef == nil ||
		*saleOrderRef != "ORD-M6-1" ||
		salePaymentRef == nil ||
		*salePaymentRef != "PAY-M6-1" ||
		saleCurrency != created.Currency ||
		saleConfirmedAt.IsZero() {
		t.Fatalf(
			"invalid Sale count=%d order=%v payment=%v currency=%s confirmed=%s",
			saleCount,
			saleOrderRef,
			salePaymentRef,
			saleCurrency,
			saleConfirmedAt,
		)
	}

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*),
				COALESCE(SUM(quantity), 0)
			FROM sale_items
			WHERE sale_id = $1
		`,
		confirmed.SaleID,
	).Scan(
		&saleItemCount,
		&saleItemQuantity,
	); err != nil {
		t.Fatalf(
			"read SaleItems: %v",
			err,
		)
	}

	if saleItemCount != 2 ||
		saleItemQuantity != 3 {
		t.Fatalf(
			"SaleItems count=%d quantity=%d want=2/3",
			saleItemCount,
			saleItemQuantity,
		)
	}

	assertClaimType(
		t,
		ctx,
		f.pool,
		f.seatIDs["A1"],
		"SALE",
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				active_reserved_quantity,
				sold_current_quantity
			FROM ga_shared_inventory
			WHERE ga_pool_id = $1
		`,
		f.gaMainID,
	).Scan(
		&gaActiveReserved,
		&gaSold,
	); err != nil {
		t.Fatalf(
			"read confirmed GA accounting: %v",
			err,
		)
	}

	if gaActiveReserved != 0 ||
		gaSold != soldBefore+2 {
		t.Fatalf(
			"GA active_reserved=%d sold=%d want=0/%d",
			gaActiveReserved,
			gaSold,
			soldBefore+2,
		)
	}

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM ticket_entitlements
			WHERE origin_sale_item_id IN (
				SELECT id
				FROM sale_items
				WHERE sale_id = $1
			)
			  AND replaces_ticket_entitlement_id IS NULL
			  AND status = 'ACTIVE'
		`,
		confirmed.SaleID,
	).Scan(
		&ticketCount,
	); err != nil {
		t.Fatalf(
			"count Tickets: %v",
			err,
		)
	}

	if ticketCount != 3 {
		t.Fatalf(
			"Ticket count=%d want=3",
			ticketCount,
		)
	}

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM qr_credentials q
			JOIN ticket_entitlements t
			  ON t.id = q.ticket_entitlement_id
			JOIN sale_items si
			  ON si.id = t.origin_sale_item_id
			WHERE si.sale_id = $1
			  AND q.status = 'ACTIVE'
		`,
		confirmed.SaleID,
	).Scan(
		&credentialCount,
	); err != nil {
		t.Fatalf(
			"count QR credentials: %v",
			err,
		)
	}

	if credentialCount != 3 {
		t.Fatalf(
			"QR credential count=%d want=3",
			credentialCount,
		)
	}

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM checkout_attempts
			WHERE id = $1
		`,
		checkout.CheckoutAttemptID,
	).Scan(
		&checkoutState,
	); err != nil {
		t.Fatalf(
			"read confirmed CheckoutAttempt: %v",
			err,
		)
	}

	if checkoutState != "CONFIRMED" {
		t.Fatalf(
			"CheckoutAttempt state=%s want=CONFIRMED",
			checkoutState,
		)
	}

	var (
		credentialID uuid.UUID
		ticketID     uuid.UUID
		eventID      uuid.UUID
		keyVersion   int
		tokenHash    []byte
	)

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT
				q.id,
				q.ticket_entitlement_id,
				t.event_id,
				q.token_key_version,
				q.token_hash
			FROM qr_credentials q
			JOIN ticket_entitlements t
			  ON t.id = q.ticket_entitlement_id
			JOIN sale_items si
			  ON si.id = t.origin_sale_item_id
			WHERE si.sale_id = $1
			ORDER BY q.created_at, q.id
			LIMIT 1
		`,
		confirmed.SaleID,
	).Scan(
		&credentialID,
		&ticketID,
		&eventID,
		&keyVersion,
		&tokenHash,
	); err != nil {
		t.Fatalf(
			"read QR credential: %v",
			err,
		)
	}

	keyBytes := make(
		[]byte,
		32,
	)

	for i := range keyBytes {
		keyBytes[i] =
			byte(i + 1)
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
			"recreate QR keyring: %v",
			err,
		)
	}

	mac, err := keyring.MAC(
		keyVersion,
		auth.Canonical(
			credentialID.String(),
			ticketID.String(),
			eventID.String(),
		),
	)
	if err != nil {
		t.Fatalf(
			"recreate QR MAC: %v",
			err,
		)
	}

	payload := fmt.Sprintf(
		"qr1.%d.%s.%s",
		keyVersion,
		credentialID.String(),
		base64.
			RawURLEncoding.
			EncodeToString(
				mac,
			),
	)

	expectedHash :=
		auth.TokenHash(
			payload,
		)

	if !bytes.Equal(
		tokenHash,
		expectedHash[:],
	) {
		t.Fatal(
			"persisted QR token hash does not match deterministic qr1 payload",
		)
	}

	_, err =
		f.reservation.Confirm(
			ctx,
			f.partnerID,
			created.Token,
			reservation.ConfirmInput{
				CheckoutAttemptID: checkout.
					CheckoutAttemptID,
				PartnerOrderRef:   "ORD-DIFFERENT",
				PartnerPaymentRef: "PAY-DIFFERENT",
			},
		)

	apiErr, ok := apierror.As(err)
	if !ok ||
		apiErr.Code !=
			apierror.CodeAlreadyConfirmed {
		t.Fatalf(
			"second confirmation error=%v want=ALREADY_CONFIRMED",
			err,
		)
	}

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM sales
			WHERE reservation_id = $1
		`,
		created.ReservationID,
	).Scan(
		&saleCount,
	); err != nil {
		t.Fatalf(
			"recount Sale: %v",
			err,
		)
	}

	if saleCount != 1 {
		t.Fatalf(
			"Sale count after second confirmation=%d want=1",
			saleCount,
		)
	}
}

func TestM6DelayedConfirmationDuringReconciliation(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	created, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind: reservation.InventoryReserved,
						InventoryID:   f.seatIDs["A2"],
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create reconciliation confirmation Reservation: %v",
			err,
		)
	}

	checkout, err :=
		f.reservation.BeginCheckout(
			ctx,
			f.partnerID,
			created.Token,
		)
	if err != nil {
		t.Fatalf(
			"begin reconciliation confirmation checkout: %v",
			err,
		)
	}

	if _, err :=
		f.reservation.MarkPaymentUncertain(
			ctx,
			f.partnerID,
			created.Token,
			"PAY-RECON-M6",
			"PSP_TIMEOUT",
		); err != nil {
		t.Fatalf(
			"mark payment uncertain: %v",
			err,
		)
	}

	confirmed, err :=
		f.reservation.Confirm(
			ctx,
			f.partnerID,
			created.Token,
			reservation.ConfirmInput{
				CheckoutAttemptID: checkout.
					CheckoutAttemptID,
				PartnerOrderRef:   "ORD-RECON-M6",
				PartnerPaymentRef: "PAY-RECON-M6",
			},
		)
	if err != nil {
		t.Fatalf(
			"delayed reconciliation confirmation: %v",
			err,
		)
	}

	if confirmed.State != "CONFIRMED" ||
		len(confirmed.Tickets) != 1 {
		t.Fatalf(
			"invalid reconciliation confirmation: %+v",
			confirmed,
		)
	}

	assertClaimType(
		t,
		ctx,
		f.pool,
		f.seatIDs["A2"],
		"SALE",
	)
}

func TestM6ConcurrentConfirmationHasOneSale(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	created, err :=
		f.reservation.Create(
			ctx,
			reservation.CreateInput{
				EventID:   f.eventID,
				PartnerID: f.partnerID,
				Items: []reservation.ItemInput{
					{
						InventoryKind: reservation.InventoryReserved,
						InventoryID:   f.seatIDs["A3"],
						Quantity:      1,
						SourceKind:    reservation.SourceShared,
					},
				},
			},
		)
	if err != nil {
		t.Fatalf(
			"create concurrent confirmation Reservation: %v",
			err,
		)
	}

	checkout, err :=
		f.reservation.BeginCheckout(
			ctx,
			f.partnerID,
			created.Token,
		)
	if err != nil {
		t.Fatalf(
			"begin concurrent confirmation checkout: %v",
			err,
		)
	}

	input := reservation.ConfirmInput{
		CheckoutAttemptID: checkout.
			CheckoutAttemptID,
		PartnerOrderRef:   "ORD-CONCURRENT-M6",
		PartnerPaymentRef: "PAY-CONCURRENT-M6",
	}

	errs := make(
		[]error,
		2,
	)

	var wg sync.WaitGroup
	wg.Add(2)

	for index := range errs {
		go func(index int) {
			defer wg.Done()

			_, errs[index] =
				f.reservation.Confirm(
					ctx,
					f.partnerID,
					created.Token,
					input,
				)
		}(index)
	}

	wg.Wait()

	successes := 0
	alreadyConfirmed := 0

	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}

		apiErr, ok :=
			apierror.As(err)
		if ok &&
			apiErr.Code ==
				apierror.CodeAlreadyConfirmed {
			alreadyConfirmed++
			continue
		}

		t.Fatalf(
			"unexpected concurrent confirmation error: %v",
			err,
		)
	}

	if successes != 1 ||
		alreadyConfirmed != 1 {
		t.Fatalf(
			"concurrent results successes=%d already_confirmed=%d want=1/1",
			successes,
			alreadyConfirmed,
		)
	}

	var saleCount int

	if err := f.pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM sales
			WHERE reservation_id = $1
		`,
		created.ReservationID,
	).Scan(
		&saleCount,
	); err != nil {
		t.Fatalf(
			"count concurrent Sales: %v",
			err,
		)
	}

	if saleCount != 1 {
		t.Fatalf(
			"concurrent Sale count=%d want=1",
			saleCount,
		)
	}
}
