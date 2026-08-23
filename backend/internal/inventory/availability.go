package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(
	db *pgxpool.Pool,
) *Service {
	return &Service{
		db: db,
	}
}

type Price struct {
	PriceTierID uuid.UUID
	AmountMinor int64
	Currency    string
}

type Offer struct {
	OfferID           string
	AvailableQuantity int
	Price             Price
	SourceKind        OfferSourceKind
	SourceID          uuid.UUID
}

type ReservedAvailability struct {
	InventoryID uuid.UUID
	SectionID   uuid.UUID
	Row         string
	Seat        string
	Sellability string
	Offer       *Offer
}

type GAPoolAvailability struct {
	InventoryID uuid.UUID
	Name        string
	Offers      []Offer
}

type Availability struct {
	EventID       uuid.UUID
	AsOf          time.Time
	ServerTime    time.Time
	ReservedUnits []ReservedAvailability
	GAPools       []GAPoolAvailability
}

func (s *Service) PartnerAvailability(
	ctx context.Context,
	partnerID uuid.UUID,
	eventID uuid.UUID,
) (Availability, error) {
	if partnerID == uuid.Nil ||
		eventID == uuid.Nil {
		return Availability{}, apierror.New(
			apierror.CodeValidation,
			"Partner and Event are required",
		)
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Availability{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var (
		partnerState string
		accessState  *string
		eventState   string
		asOf         time.Time
	)

	err = tx.QueryRow(
		ctx,
		`
			SELECT
				p.state,
				pea.state,
				e.state,
				clock_timestamp()
			FROM partners p
			CROSS JOIN events e
			LEFT JOIN partner_event_access pea
			  ON pea.partner_id = p.id
			 AND pea.event_id = e.id
			WHERE p.id = $1
			  AND e.id = $2
		`,
		partnerID,
		eventID,
	).Scan(
		&partnerState,
		&accessState,
		&eventState,
		&asOf,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Availability{}, apierror.New(
				apierror.CodeNotAuthorized,
				"Partner is not authorized for this Event",
			)
		}
		return Availability{}, err
	}

	if partnerState != "ACTIVE" {
		return Availability{}, apierror.New(
			apierror.CodePartnerDisabled,
			"Partner is disabled",
		)
	}

	if accessState == nil {
		return Availability{}, apierror.New(
			apierror.CodeNotAuthorized,
			"Partner is not authorized for this Event",
		)
	}

	if *accessState != "ACTIVE" {
		return Availability{}, apierror.New(
			apierror.CodePartnerEventAccessDisabled,
			"Partner Event access is disabled",
		)
	}

	switch eventState {
	case "ON_SALE":
	case "PAUSED":
		return Availability{}, apierror.New(
			apierror.CodeEventPaused,
			"Event sales are paused",
		)
	case "SALES_CLOSED":
		return Availability{}, apierror.New(
			apierror.CodeEventSalesClosed,
			"Event sales are closed",
		)
	case "CANCELLED":
		return Availability{}, apierror.New(
			apierror.CodeEventCancelled,
			"Event is cancelled",
		)
	case "COMPLETED":
		return Availability{}, apierror.New(
			apierror.CodeEventSalesClosed,
			"Event sales are closed",
		)
	default:
		return Availability{}, apierror.New(
			apierror.CodeEventNotOnSale,
			"Event is not on sale",
		)
	}

	reserved, err := s.reservedAvailability(
		ctx,
		tx,
		partnerID,
		eventID,
	)
	if err != nil {
		return Availability{}, err
	}

	gaPools, err := s.gaAvailability(
		ctx,
		tx,
		partnerID,
		eventID,
	)
	if err != nil {
		return Availability{}, err
	}

	var serverTime time.Time

	if err := tx.QueryRow(
		ctx,
		`SELECT clock_timestamp()`,
	).Scan(&serverTime); err != nil {
		return Availability{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Availability{}, err
	}

	return Availability{
		EventID:       eventID,
		AsOf:          asOf,
		ServerTime:    serverTime,
		ReservedUnits: reserved,
		GAPools:       gaPools,
	}, nil
}

func (s *Service) reservedAvailability(
	ctx context.Context,
	q pgx.Tx,
	partnerID uuid.UUID,
	eventID uuid.UUID,
) ([]ReservedAvailability, error) {
	rows, err := q.Query(
		ctx,
		`
			SELECT
				riu.id,
				riu.event_section_id,
				COALESCE(riu.row_label, ''),
				riu.seat_label,
				pt.id,
				pt.amount_minor,
				pt.currency,
				ric.claim_type,
				aru.id,
				a.mode,
				ir.state,
				a.partner_id
			FROM reserved_inventory_units riu
			JOIN event_sections es
			  ON es.id = riu.event_section_id
			JOIN event_price_tiers pt
			  ON pt.id = COALESCE(
			      riu.price_tier_override_id,
			      es.default_price_tier_id
			  )
			 AND pt.event_id = riu.event_id
			LEFT JOIN reserved_inventory_claims ric
			  ON ric.reserved_inventory_unit_id = riu.id
			 AND ric.ended_at IS NULL
			LEFT JOIN allocation_reserved_units aru
			  ON aru.id = ric.allocation_reserved_unit_id
			LEFT JOIN allocations a
			  ON a.restriction_id = aru.allocation_id
			LEFT JOIN inventory_restrictions ir
			  ON ir.id = a.restriction_id
			WHERE riu.event_id = $1
			ORDER BY es.sort_order, riu.snapshot_object_key, riu.id
		`,
		eventID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(
		[]ReservedAvailability,
		0,
	)

	for rows.Next() {
		var (
			inventoryID         uuid.UUID
			sectionID           uuid.UUID
			rowLabel            string
			seatLabel           string
			priceTierID         uuid.UUID
			amountMinor         int64
			currency            string
			claimType           *string
			membershipID        *uuid.UUID
			mode                *string
			restrictionState    *string
			allocationPartnerID *uuid.UUID
		)

		if err := rows.Scan(
			&inventoryID,
			&sectionID,
			&rowLabel,
			&seatLabel,
			&priceTierID,
			&amountMinor,
			&currency,
			&claimType,
			&membershipID,
			&mode,
			&restrictionState,
			&allocationPartnerID,
		); err != nil {
			return nil, err
		}

		entry := ReservedAvailability{
			InventoryID: inventoryID,
			SectionID:   sectionID,
			Row:         rowLabel,
			Seat:        seatLabel,
			Sellability: "UNAVAILABLE",
		}

		price := Price{
			PriceTierID: priceTierID,
			AmountMinor: amountMinor,
			Currency:    currency,
		}

		if claimType == nil {
			offerID := OfferID(
				eventID,
				partnerID,
				"RESERVED",
				inventoryID,
				OfferSourceShared,
				uuid.Nil,
				priceTierID,
				amountMinor,
				currency,
			)

			entry.Sellability = "AVAILABLE"
			entry.Offer = &Offer{
				OfferID:           offerID,
				AvailableQuantity: 1,
				Price:             price,
				SourceKind:        OfferSourceShared,
				SourceID:          uuid.Nil,
			}
		} else if *claimType == "ALLOCATION" &&
			membershipID != nil &&
			mode != nil &&
			*mode == "CHANNEL" &&
			restrictionState != nil &&
			*restrictionState == "ACTIVE" &&
			allocationPartnerID != nil &&
			*allocationPartnerID == partnerID {
			offerID := OfferID(
				eventID,
				partnerID,
				"RESERVED",
				inventoryID,
				OfferSourceAllocation,
				*membershipID,
				priceTierID,
				amountMinor,
				currency,
			)

			entry.Sellability = "AVAILABLE"
			entry.Offer = &Offer{
				OfferID:           offerID,
				AvailableQuantity: 1,
				Price:             price,
				SourceKind:        OfferSourceAllocation,
				SourceID:          *membershipID,
			}
		}

		result = append(
			result,
			entry,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Service) gaAvailability(
	ctx context.Context,
	q pgx.Tx,
	partnerID uuid.UUID,
	eventID uuid.UUID,
) ([]GAPoolAvailability, error) {
	rows, err := q.Query(
		ctx,
		`
			SELECT
				gp.id,
				gp.name,
				pt.id,
				pt.amount_minor,
				pt.currency,
				gsi.available_quantity,
				gab.id,
				gab.available_quantity
			FROM ga_inventory_pools gp
			JOIN event_price_tiers pt
			  ON pt.id = gp.price_tier_id
			 AND pt.event_id = gp.event_id
			JOIN ga_shared_inventory gsi
			  ON gsi.ga_pool_id = gp.id
			LEFT JOIN ga_allocation_buckets gab
			  ON gab.ga_pool_id = gp.id
			 AND gab.available_quantity > 0
			 AND EXISTS (
			      SELECT 1
			      FROM allocations a
			      JOIN inventory_restrictions ir
			        ON ir.id = a.restriction_id
			      WHERE a.restriction_id = gab.allocation_id
			        AND a.mode = 'CHANNEL'
			        AND a.partner_id = $2
			        AND ir.event_id = gp.event_id
			        AND ir.state = 'ACTIVE'
			 )
			WHERE gp.event_id = $1
			ORDER BY gp.snapshot_object_key, gp.id, gab.id
		`,
		eventID,
		partnerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type accumulator struct {
		index int
	}

	indexes := map[uuid.UUID]accumulator{}
	result := make(
		[]GAPoolAvailability,
		0,
	)

	for rows.Next() {
		var (
			poolID              uuid.UUID
			name                string
			priceTierID         uuid.UUID
			amountMinor         int64
			currency            string
			sharedAvailable     int
			allocationBucketID  *uuid.UUID
			allocationAvailable *int
		)

		if err := rows.Scan(
			&poolID,
			&name,
			&priceTierID,
			&amountMinor,
			&currency,
			&sharedAvailable,
			&allocationBucketID,
			&allocationAvailable,
		); err != nil {
			return nil, err
		}

		acc, exists := indexes[poolID]

		if !exists {
			offers := make(
				[]Offer,
				0,
				2,
			)

			if sharedAvailable > 0 {
				offers = append(
					offers,
					Offer{
						OfferID: OfferID(
							eventID,
							partnerID,
							"GA",
							poolID,
							OfferSourceShared,
							uuid.Nil,
							priceTierID,
							amountMinor,
							currency,
						),
						AvailableQuantity: sharedAvailable,
						Price: Price{
							PriceTierID: priceTierID,
							AmountMinor: amountMinor,
							Currency:    currency,
						},
						SourceKind: OfferSourceShared,
						SourceID:   uuid.Nil,
					},
				)
			}

			result = append(
				result,
				GAPoolAvailability{
					InventoryID: poolID,
					Name:        name,
					Offers:      offers,
				},
			)

			acc = accumulator{
				index: len(result) - 1,
			}

			indexes[poolID] = acc
		}

		if allocationBucketID != nil &&
			allocationAvailable != nil &&
			*allocationAvailable > 0 {
			result[acc.index].Offers = append(
				result[acc.index].Offers,
				Offer{
					OfferID: OfferID(
						eventID,
						partnerID,
						"GA",
						poolID,
						OfferSourceAllocation,
						*allocationBucketID,
						priceTierID,
						amountMinor,
						currency,
					),
					AvailableQuantity: *allocationAvailable,
					Price: Price{
						PriceTierID: priceTierID,
						AmountMinor: amountMinor,
						Currency:    currency,
					},
					SourceKind: OfferSourceAllocation,
					SourceID:   *allocationBucketID,
				},
			)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
