package reporting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

type EventContext struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	State    string     `json:"state"`
	StartsAt *time.Time `json:"starts_at"`
}

type InventoryDimensions struct {
	Capacity         int64 `json:"capacity"`
	Available        int64 `json:"available"`
	Held             int64 `json:"held"`
	Committing       int64 `json:"committing"`
	PaymentRetry     int64 `json:"payment_retry"`
	Reconciling      int64 `json:"reconciling"`
	Blocked          int64 `json:"blocked"`
	Allocated        int64 `json:"allocated"`
	SoldCurrent      int64 `json:"sold_current"`
	IssuedCurrent    int64 `json:"issued_current"`
	VoidedTickets    int64 `json:"voided_tickets"`
	CapacityConsumed int64 `json:"capacity_consumed"`
	HistoricalSold   int64 `json:"historical_sold"`
	HistoricalIssued int64 `json:"historical_issued"`
}

func (d InventoryDimensions) add(other InventoryDimensions) InventoryDimensions {
	d.Capacity += other.Capacity
	d.Available += other.Available
	d.Held += other.Held
	d.Committing += other.Committing
	d.PaymentRetry += other.PaymentRetry
	d.Reconciling += other.Reconciling
	d.Blocked += other.Blocked
	d.Allocated += other.Allocated
	d.SoldCurrent += other.SoldCurrent
	d.IssuedCurrent += other.IssuedCurrent
	d.VoidedTickets += other.VoidedTickets
	d.CapacityConsumed += other.CapacityConsumed
	d.HistoricalSold += other.HistoricalSold
	d.HistoricalIssued += other.HistoricalIssued
	return d
}

type InventoryReport struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Event       EventContext        `json:"event"`
	Reserved    InventoryDimensions `json:"reserved_seating"`
	GA          InventoryDimensions `json:"general_admission"`
	Total       InventoryDimensions `json:"total"`
}

type SalesReport struct {
	GeneratedAt           time.Time    `json:"generated_at"`
	Event                 EventContext `json:"event"`
	SaleCount             int64        `json:"sale_count"`
	HistoricalQuantity    int64        `json:"historical_sale_quantity"`
	HistoricalAmountMinor int64        `json:"historical_amount_minor"`
	ActiveSoldTickets     int64        `json:"active_sold_tickets"`
	VoidedSoldTickets     int64        `json:"voided_sold_tickets"`
	CurrentSoldCapacity   int64        `json:"current_sold_capacity"`
	Currency              *string      `json:"currency"`
}

type AdmissionReport struct {
	GeneratedAt        time.Time        `json:"generated_at"`
	Event              EventContext     `json:"event"`
	ActiveAdmissions   int64            `json:"active_admissions"`
	ReversedAdmissions int64            `json:"reversed_admissions"`
	ScanOutcomes       map[string]int64 `json:"scan_outcomes"`
}

func (s *Service) readSnapshot(ctx context.Context, work func(pgx.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin reporting snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit reporting snapshot: %w", err)
	}
	return nil
}

func eventContext(ctx context.Context, q pgx.Tx, eventID uuid.UUID) (EventContext, time.Time, error) {
	var event EventContext
	var id uuid.UUID
	var generatedAt time.Time
	err := q.QueryRow(ctx, `SELECT id, name, state, starts_at, clock_timestamp() FROM events WHERE id=$1`, eventID).
		Scan(&id, &event.Name, &event.State, &event.StartsAt, &generatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return event, generatedAt, apierror.New(apierror.CodeResourceNotFound, "Event not found")
	}
	if err != nil {
		return event, generatedAt, err
	}
	event.ID = publicid.Encode(publicid.Event, id)
	return event, generatedAt, nil
}

func (s *Service) Inventory(ctx context.Context, eventID uuid.UUID, partnerID *uuid.UUID) (InventoryReport, error) {
	var report InventoryReport
	err := s.readSnapshot(ctx, func(tx pgx.Tx) error {
		var err error
		report.Event, report.GeneratedAt, err = eventContext(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if err := scanReservedDimensions(ctx, tx, eventID, partnerID, &report.Reserved); err != nil {
			return err
		}
		if err := scanGADimensions(ctx, tx, eventID, partnerID, &report.GA); err != nil {
			return err
		}
		report.Total = report.Reserved.add(report.GA)
		return nil
	})
	return report, err
}

func scanReservedDimensions(ctx context.Context, q pgx.Tx, eventID uuid.UUID, partnerID *uuid.UUID, d *InventoryDimensions) error {
	return q.QueryRow(ctx, `
		WITH current_claim AS (
			SELECT ric.reserved_inventory_unit_id, ric.claim_type, r.state reservation_state, r.partner_id reservation_partner_id,
			       a.partner_id allocation_partner_id, s.partner_id sale_partner_id
			FROM reserved_inventory_claims ric
			LEFT JOIN reservation_items ri ON ri.id=ric.reservation_item_id
			LEFT JOIN reservations r ON r.id=ri.reservation_id
			LEFT JOIN allocation_reserved_units aru ON aru.id=ric.allocation_reserved_unit_id
			LEFT JOIN allocations a ON a.restriction_id=aru.allocation_id
			LEFT JOIN sale_items si ON si.id=ric.sale_item_id
			LEFT JOIN sales s ON s.id=si.sale_id
			WHERE ric.ended_at IS NULL
		), base AS (
			SELECT riu.id, cc.claim_type, cc.reservation_state, cc.reservation_partner_id, cc.allocation_partner_id, cc.sale_partner_id
			FROM reserved_inventory_units riu LEFT JOIN current_claim cc ON cc.reserved_inventory_unit_id=riu.id
			WHERE riu.event_id=$1
		), tickets AS (
			SELECT te.status, te.origin_sale_item_id, te.origin_issuance_item_id, s.partner_id
			FROM ticket_entitlements te
			LEFT JOIN sale_items si ON si.id=te.origin_sale_item_id
			LEFT JOIN sales s ON s.id=si.sale_id
			WHERE te.event_id=$1 AND te.inventory_kind='RESERVED'
		), history AS (
			SELECT
				COALESCE((SELECT SUM(si.quantity) FROM sale_items si JOIN sales s ON s.id=si.sale_id WHERE si.event_id=$1 AND si.inventory_kind='RESERVED' AND ($2::uuid IS NULL OR s.partner_id=$2)),0) sold,
				COALESCE((SELECT SUM(nii.quantity) FROM non_public_issuance_items nii WHERE nii.event_id=$1 AND nii.inventory_kind='RESERVED' AND $2::uuid IS NULL),0) issued
		)
		SELECT COUNT(base.id) FILTER (WHERE $2::uuid IS NULL OR claim_type IS NULL OR allocation_partner_id=$2),
		       COUNT(base.id) FILTER (WHERE claim_type IS NULL),
		       COUNT(base.id) FILTER (WHERE claim_type='RESERVATION' AND reservation_state='HELD' AND ($2::uuid IS NULL OR reservation_partner_id=$2)),
		       COUNT(base.id) FILTER (WHERE claim_type='RESERVATION' AND reservation_state='COMMITTING' AND ($2::uuid IS NULL OR reservation_partner_id=$2)),
		       COUNT(base.id) FILTER (WHERE claim_type='RESERVATION' AND reservation_state='PAYMENT_RETRY' AND ($2::uuid IS NULL OR reservation_partner_id=$2)),
		       COUNT(base.id) FILTER (WHERE claim_type='RESERVATION' AND reservation_state='RECONCILING' AND ($2::uuid IS NULL OR reservation_partner_id=$2)),
		       COUNT(base.id) FILTER (WHERE claim_type='BLOCK' AND $2::uuid IS NULL),
		       COUNT(base.id) FILTER (WHERE claim_type='ALLOCATION' AND ($2::uuid IS NULL OR allocation_partner_id=$2)),
		       COUNT(base.id) FILTER (WHERE claim_type='SALE' AND ($2::uuid IS NULL OR sale_partner_id=$2)),
		       COUNT(base.id) FILTER (WHERE claim_type='ISSUANCE' AND $2::uuid IS NULL),
		       (SELECT COUNT(*) FROM tickets WHERE status='VOIDED' AND ($2::uuid IS NULL OR partner_id=$2)),
		       COUNT(base.id) FILTER (WHERE claim_type IN ('SALE','ISSUANCE') AND ($2::uuid IS NULL OR sale_partner_id=$2)),
		       history.sold, history.issued
		FROM history LEFT JOIN base ON true
		GROUP BY history.sold, history.issued
	`, eventID, partnerID).Scan(&d.Capacity, &d.Available, &d.Held, &d.Committing, &d.PaymentRetry, &d.Reconciling, &d.Blocked, &d.Allocated, &d.SoldCurrent, &d.IssuedCurrent, &d.VoidedTickets, &d.CapacityConsumed, &d.HistoricalSold, &d.HistoricalIssued)
}

func scanGADimensions(ctx context.Context, q pgx.Tx, eventID uuid.UUID, partnerID *uuid.UUID, d *InventoryDimensions) error {
	return q.QueryRow(ctx, `
		WITH pools AS (SELECT id, capacity FROM ga_inventory_pools WHERE event_id=$1),
		active_reservations AS (
			SELECT r.state, COALESCE(SUM(ri.quantity),0) quantity
			FROM reservation_items ri JOIN reservations r ON r.id=ri.reservation_id
			WHERE ri.event_id=$1 AND ri.inventory_kind='GA' AND ri.removed_at IS NULL
			  AND r.state IN ('HELD','COMMITTING','PAYMENT_RETRY','RECONCILING')
			  AND ($2::uuid IS NULL OR r.partner_id=$2) GROUP BY r.state
		), alloc AS (
			SELECT COALESCE(SUM(gab.available_quantity),0) available,
			       COALESCE(SUM(gab.sold_current_quantity),0) sold,
			       COALESCE(SUM(gab.issued_current_quantity),0) issued
			FROM ga_allocation_buckets gab JOIN allocations a ON a.restriction_id=gab.allocation_id
			JOIN inventory_restrictions ir ON ir.id=a.restriction_id
			JOIN pools p ON p.id=gab.ga_pool_id
			WHERE ir.state='ACTIVE' AND ($2::uuid IS NULL OR a.partner_id=$2)
		), hist AS (
			SELECT COALESCE((SELECT SUM(si.quantity) FROM sale_items si JOIN sales s ON s.id=si.sale_id WHERE si.event_id=$1 AND si.inventory_kind='GA' AND ($2::uuid IS NULL OR s.partner_id=$2)),0) sold,
			       COALESCE((SELECT SUM(nii.quantity) FROM non_public_issuance_items nii WHERE nii.event_id=$1 AND nii.inventory_kind='GA' AND $2::uuid IS NULL),0) issued
		), partner_current AS (
			SELECT COUNT(*) active
			FROM ticket_entitlements te JOIN sale_items si ON si.id=te.origin_sale_item_id JOIN sales s ON s.id=si.sale_id
			WHERE te.event_id=$1 AND te.inventory_kind='GA' AND te.inventory_released_at IS NULL AND s.partner_id=$2
		)
		SELECT COALESCE(SUM(p.capacity),0),
		       CASE WHEN $2::uuid IS NULL THEN COALESCE((SELECT SUM(gsi.available_quantity) FROM ga_shared_inventory gsi JOIN pools p2 ON p2.id=gsi.ga_pool_id),0) ELSE COALESCE((SELECT SUM(gsi.available_quantity) FROM ga_shared_inventory gsi JOIN pools p2 ON p2.id=gsi.ga_pool_id),0) END,
		       COALESCE((SELECT quantity FROM active_reservations WHERE state='HELD'),0),
		       COALESCE((SELECT quantity FROM active_reservations WHERE state='COMMITTING'),0),
		       COALESCE((SELECT quantity FROM active_reservations WHERE state='PAYMENT_RETRY'),0),
		       COALESCE((SELECT quantity FROM active_reservations WHERE state='RECONCILING'),0),
		       CASE WHEN $2::uuid IS NULL THEN COALESCE((SELECT SUM(gbb.blocked_quantity) FROM ga_block_buckets gbb JOIN pools p3 ON p3.id=gbb.ga_pool_id JOIN inventory_restrictions ir ON ir.id=gbb.block_id WHERE ir.state='ACTIVE'),0) ELSE 0 END,
		       alloc.available,
		       CASE WHEN $2::uuid IS NULL THEN COALESCE((SELECT SUM(gsi.sold_current_quantity) FROM ga_shared_inventory gsi JOIN pools p4 ON p4.id=gsi.ga_pool_id),0)+alloc.sold ELSE partner_current.active END,
		       CASE WHEN $2::uuid IS NULL THEN alloc.issued ELSE 0 END,
		       COALESCE((SELECT COUNT(*) FROM ticket_entitlements te LEFT JOIN sale_items si ON si.id=te.origin_sale_item_id LEFT JOIN sales s ON s.id=si.sale_id WHERE te.event_id=$1 AND te.inventory_kind='GA' AND te.status='VOIDED' AND ($2::uuid IS NULL OR s.partner_id=$2)),0),
		       CASE WHEN $2::uuid IS NULL THEN COALESCE((SELECT SUM(gsi.sold_current_quantity) FROM ga_shared_inventory gsi JOIN pools p5 ON p5.id=gsi.ga_pool_id),0)+alloc.sold+alloc.issued ELSE partner_current.active END,
		       hist.sold, hist.issued
		FROM alloc CROSS JOIN hist CROSS JOIN partner_current LEFT JOIN pools p ON true GROUP BY alloc.available,alloc.sold,alloc.issued,hist.sold,hist.issued,partner_current.active
	`, eventID, partnerID).Scan(&d.Capacity, &d.Available, &d.Held, &d.Committing, &d.PaymentRetry, &d.Reconciling, &d.Blocked, &d.Allocated, &d.SoldCurrent, &d.IssuedCurrent, &d.VoidedTickets, &d.CapacityConsumed, &d.HistoricalSold, &d.HistoricalIssued)
}

func (s *Service) Sales(ctx context.Context, eventID uuid.UUID, partnerID *uuid.UUID) (SalesReport, error) {
	var report SalesReport
	err := s.readSnapshot(ctx, func(tx pgx.Tx) error {
		var err error
		report.Event, report.GeneratedAt, err = eventContext(ctx, tx, eventID)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			WITH scoped_sales AS (
				SELECT * FROM sales WHERE event_id=$1 AND ($2::uuid IS NULL OR partner_id=$2)
			), history AS (
				SELECT COUNT(DISTINCT s.id) sale_count, COALESCE(SUM(si.quantity),0) quantity,
				       COALESCE(SUM(si.quantity*si.unit_amount_minor),0) amount,
				       CASE WHEN COUNT(DISTINCT s.currency)=1 THEN MIN(s.currency) END currency
				FROM scoped_sales s LEFT JOIN sale_items si ON si.sale_id=s.id
			), tickets AS (
				SELECT COUNT(te.id) FILTER (WHERE te.status='ACTIVE') active,
				       COUNT(te.id) FILTER (WHERE te.status='VOIDED') voided,
				       COUNT(te.id) FILTER (WHERE te.inventory_released_at IS NULL) current_capacity
				FROM scoped_sales s JOIN sale_items si ON si.sale_id=s.id
				JOIN ticket_entitlements te ON te.origin_sale_item_id=si.id
			)
			SELECT history.sale_count,history.quantity,history.amount,tickets.active,tickets.voided,tickets.current_capacity,history.currency
			FROM history CROSS JOIN tickets
		`, eventID, partnerID).Scan(&report.SaleCount, &report.HistoricalQuantity, &report.HistoricalAmountMinor, &report.ActiveSoldTickets, &report.VoidedSoldTickets, &report.CurrentSoldCapacity, &report.Currency)
	})
	return report, err
}

func (s *Service) Admissions(ctx context.Context, eventID uuid.UUID) (AdmissionReport, error) {
	var report AdmissionReport
	report.ScanOutcomes = map[string]int64{}
	err := s.readSnapshot(ctx, func(tx pgx.Tx) error {
		var err error
		report.Event, report.GeneratedAt, err = eventContext(ctx, tx, eventID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE status='ACTIVE'), COUNT(*) FILTER (WHERE status='REVERSED') FROM admissions WHERE event_id=$1`, eventID).Scan(&report.ActiveAdmissions, &report.ReversedAdmissions); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT result, COUNT(*) FROM scan_attempts WHERE event_id=$1 GROUP BY result ORDER BY result`, eventID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var result string
			var count int64
			if err := rows.Scan(&result, &count); err != nil {
				return err
			}
			report.ScanOutcomes[result] = count
		}
		return rows.Err()
	})
	return report, err
}
