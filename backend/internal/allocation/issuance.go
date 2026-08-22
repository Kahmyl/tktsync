package allocation

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type NonPublicIssuanceInput struct {
	RecipientRef    string
	Reason          string
	ReservedUnitIDs []uuid.UUID
	GATargets       []GATarget
}

type IssuedTicket struct {
	TicketID     uuid.UUID
	CredentialID uuid.UUID
	State        string
}

type NonPublicIssuance struct {
	IssuanceID   uuid.UUID
	EventID      uuid.UUID
	AllocationID uuid.UUID
	IssuedAt     time.Time
	Tickets      []IssuedTicket
}

type reservedIssuanceTarget struct {
	UnitID       uuid.UUID
	MembershipID uuid.UUID
	ClaimID      uuid.UUID
}

type gaIssuanceTarget struct {
	PoolID   uuid.UUID
	BucketID uuid.UUID
	Quantity int
}

func (s *Service) IssueNonPublic(
	ctx context.Context,
	actorID uuid.UUID,
	allocationID uuid.UUID,
	input NonPublicIssuanceInput,
) (NonPublicIssuance, error) {
	if actorID == uuid.Nil ||
		allocationID == uuid.Nil {
		return NonPublicIssuance{},
			validation(
				"actor and Allocation are required",
			)
	}

	if s.qrKeys == nil {
		return NonPublicIssuance{},
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"QR credential authority is not configured",
			)
	}

	input.RecipientRef =
		strings.TrimSpace(
			input.RecipientRef,
		)

	input.Reason =
		strings.TrimSpace(
			input.Reason,
		)

	input.ReservedUnitIDs =
		uniqueUUIDs(
			input.ReservedUnitIDs,
		)

	input.GATargets =
		normalizeGATargets(
			input.GATargets,
		)

	if err := validateTargets(
		input.ReservedUnitIDs,
		input.GATargets,
	); err != nil {
		return NonPublicIssuance{},
			err
	}

	var result NonPublicIssuance

	err := s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			eventID, err :=
				issuanceAllocationEventID(
					ctx,
					tx,
					allocationID,
				)
			if err != nil {
				return err
			}

			if err :=
				lockIssuanceEvent(
					ctx,
					tx,
					eventID,
				); err != nil {
				return err
			}

			if err :=
				lockEligibleNonPublicAllocation(
					ctx,
					tx,
					eventID,
					allocationID,
				); err != nil {
				return err
			}

			if err :=
				lockReservedRows(
					ctx,
					tx,
					eventID,
					input.ReservedUnitIDs,
				); err != nil {
				return err
			}

			if err :=
				lockGAPools(
					ctx,
					tx,
					eventID,
					input.GATargets,
				); err != nil {
				return err
			}

			reservedTargets, err :=
				lockReservedIssuanceTargets(
					ctx,
					tx,
					allocationID,
					input.ReservedUnitIDs,
				)
			if err != nil {
				return err
			}

			gaTargets, err :=
				lockGAIssuanceTargets(
					ctx,
					tx,
					allocationID,
					input.GATargets,
				)
			if err != nil {
				return err
			}

			var issuedAt time.Time

			if err := tx.QueryRow(
				ctx,
				`SELECT clock_timestamp()`,
			).Scan(
				&issuedAt,
			); err != nil {
				return err
			}

			issuanceID := uuid.New()

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO non_public_issuances (
						id,
						event_id,
						allocation_id,
						issued_by_user_id,
						recipient_ref,
						reason,
						issued_at,
						created_at
					)
					VALUES (
						$1,
						$2,
						$3,
						$4,
						NULLIF($5, ''),
						NULLIF($6, ''),
						$7,
						$7
					)
				`,
				issuanceID,
				eventID,
				allocationID,
				actorID,
				input.RecipientRef,
				input.Reason,
				issuedAt,
			); err != nil {
				return err
			}

			tickets := make(
				[]IssuedTicket,
				0,
				len(reservedTargets)+
					totalGAIssuanceQuantity(
						gaTargets,
					),
			)

			for _, target := range reservedTargets {
				issuanceItemID :=
					uuid.New()

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO non_public_issuance_items (
							id,
							issuance_id,
							event_id,
							inventory_kind,
							reserved_inventory_unit_id,
							allocation_reserved_unit_id,
							quantity,
							created_at
						)
						VALUES (
							$1,
							$2,
							$3,
							'RESERVED',
							$4,
							$5,
							1,
							$6
						)
					`,
					issuanceItemID,
					issuanceID,
					eventID,
					target.UnitID,
					target.MembershipID,
					issuedAt,
				); err != nil {
					return err
				}

				commandTag, err :=
					tx.Exec(
						ctx,
						`
							UPDATE reserved_inventory_claims
							SET
								ended_at = $2,
								end_reason =
								    'NON_PUBLIC_ISSUED'
							WHERE id = $1
							  AND claim_type =
							      'ALLOCATION'
							  AND ended_at IS NULL
						`,
						target.ClaimID,
						issuedAt,
					)
				if err != nil {
					return err
				}

				if commandTag.RowsAffected() !=
					1 {
					return apierror.New(
						apierror.CodeInventoryUnavailable,
						"Reserved Allocation inventory is no longer issuable",
					)
				}

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO reserved_inventory_claims (
							id,
							reserved_inventory_unit_id,
							claim_type,
							issuance_item_id,
							activated_at
						)
						VALUES (
							$1,
							$2,
							'ISSUANCE',
							$3,
							$4
						)
					`,
					uuid.New(),
					target.UnitID,
					issuanceItemID,
					issuedAt,
				); err != nil {
					return err
				}

				ticket, err :=
					s.createIssuanceTicket(
						ctx,
						tx,
						eventID,
						issuanceItemID,
						"RESERVED",
						&target.UnitID,
						nil,
						issuedAt,
					)
				if err != nil {
					return err
				}

				tickets = append(
					tickets,
					ticket,
				)
			}

			for _, target := range gaTargets {
				issuanceItemID :=
					uuid.New()

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO non_public_issuance_items (
							id,
							issuance_id,
							event_id,
							inventory_kind,
							ga_pool_id,
							ga_allocation_bucket_id,
							quantity,
							created_at
						)
						VALUES (
							$1,
							$2,
							$3,
							'GA',
							$4,
							$5,
							$6,
							$7
						)
					`,
					issuanceItemID,
					issuanceID,
					eventID,
					target.PoolID,
					target.BucketID,
					target.Quantity,
					issuedAt,
				); err != nil {
					return err
				}

				commandTag, err :=
					tx.Exec(
						ctx,
						`
							UPDATE ga_allocation_buckets
							SET
								available_quantity =
								    available_quantity - $2,
								issued_current_quantity =
								    issued_current_quantity + $2,
								updated_at = $3
							WHERE id = $1
							  AND available_quantity >= $2
						`,
						target.BucketID,
						target.Quantity,
						issuedAt,
					)
				if err != nil {
					return err
				}

				if commandTag.RowsAffected() !=
					1 {
					return apierror.New(
						apierror.CodeInsufficientGAQuantity,
						"Requested NON_PUBLIC GA quantity is no longer available",
					)
				}

				for index := 0; index < target.Quantity; index++ {
					ticket, err :=
						s.createIssuanceTicket(
							ctx,
							tx,
							eventID,
							issuanceItemID,
							"GA",
							nil,
							&target.PoolID,
							issuedAt,
						)
					if err != nil {
						return err
					}

					tickets = append(
						tickets,
						ticket,
					)
				}
			}

			if _, err := s.audit.Append(
				ctx,
				tx,
				audit.Event{
					EventID:     &eventID,
					ActorKind:   audit.ActorUser,
					ActorUserID: &actorID,
					Operation:   "NON_PUBLIC_ISSUED",
					EntityType:  "NON_PUBLIC_ISSUANCE",
					EntityID:    &issuanceID,
				},
			); err != nil {
				return err
			}

			if _, err := s.outbox.Append(
				ctx,
				tx,
				outbox.Fact{
					EventID:       &eventID,
					FactType:      "ticket.non_public_issued",
					AggregateType: "NON_PUBLIC_ISSUANCE",
					AggregateID:   &issuanceID,
				},
			); err != nil {
				return err
			}

			result = NonPublicIssuance{
				IssuanceID:   issuanceID,
				EventID:      eventID,
				AllocationID: allocationID,
				IssuedAt:     issuedAt,
				Tickets:      tickets,
			}

			return nil
		},
	)
	if err != nil {
		return NonPublicIssuance{},
			err
	}

	return result, nil
}

func issuanceAllocationEventID(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
) (uuid.UUID, error) {
	var eventID uuid.UUID

	err := tx.QueryRow(
		ctx,
		`
			SELECT ir.event_id
			FROM inventory_restrictions ir
			JOIN allocations a
			  ON a.restriction_id = ir.id
			WHERE ir.id = $1
			  AND ir.kind = 'ALLOCATION'
		`,
		allocationID,
	).Scan(
		&eventID,
	)

	if errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return uuid.Nil,
			notFound("Allocation")
	}

	if err != nil {
		return uuid.Nil, err
	}

	return eventID, nil
}

func lockIssuanceEvent(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
) error {
	var (
		state     string
		finalized bool
	)

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				e.state,
				EXISTS (
					SELECT 1
					FROM event_layout_snapshots els
					WHERE els.event_id = e.id
					  AND els.finalized_at IS NOT NULL
				)
			FROM events e
			WHERE e.id = $1
			FOR KEY SHARE
		`,
		eventID,
	).Scan(
		&state,
		&finalized,
	)

	if errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return notFound("Event")
	}

	if err != nil {
		return err
	}

	switch state {
	case "DRAFT",
		"ON_SALE",
		"PAUSED",
		"SALES_CLOSED":

	case "CANCELLED":
		return apierror.New(
			apierror.CodeEventCancelled,
			"Cancelled Event cannot create non-public issuance",
		)

	case "COMPLETED":
		return apierror.New(
			apierror.CodeEventSalesClosed,
			"Completed Event cannot create non-public issuance",
		)

	default:
		return validation(
			"Event state does not permit non-public issuance",
		)
	}

	if !finalized {
		return validation(
			"Non-public issuance requires finalized Event inventory",
		)
	}

	return nil
}

func lockEligibleNonPublicAllocation(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	allocationID uuid.UUID,
) error {
	var (
		state string
		mode  string
	)

	err := tx.QueryRow(
		ctx,
		`
			SELECT
				ir.state,
				a.mode
			FROM inventory_restrictions ir
			JOIN allocations a
			  ON a.restriction_id = ir.id
			WHERE ir.id = $1
			  AND ir.event_id = $2
			  AND ir.kind = 'ALLOCATION'
			FOR UPDATE OF ir, a
		`,
		allocationID,
		eventID,
	).Scan(
		&state,
		&mode,
	)

	if errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return notFound("Allocation")
	}

	if err != nil {
		return err
	}

	if state != "ACTIVE" {
		return validation(
			"only ACTIVE Allocations may issue non-public Tickets",
		)
	}

	if mode != "NON_PUBLIC" {
		return validation(
			"Allocation is not an eligible NON_PUBLIC Allocation",
		)
	}

	return nil
}

func lockReservedIssuanceTargets(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
	unitIDs []uuid.UUID,
) ([]reservedIssuanceTarget, error) {
	result := make(
		[]reservedIssuanceTarget,
		0,
		len(unitIDs),
	)

	for _, unitID := range unitIDs {
		var target reservedIssuanceTarget

		err := tx.QueryRow(
			ctx,
			`
				SELECT
					aru.id,
					aru.reserved_inventory_unit_id,
					ric.id
				FROM allocation_reserved_units aru
				JOIN reserved_inventory_claims ric
				  ON ric.allocation_reserved_unit_id =
				     aru.id
				 AND ric.claim_type =
				     'ALLOCATION'
				 AND ric.ended_at IS NULL
				WHERE aru.allocation_id = $1
				  AND aru.reserved_inventory_unit_id =
				      $2
				  AND aru.released_at IS NULL
				FOR UPDATE OF aru, ric
			`,
			allocationID,
			unitID,
		).Scan(
			&target.MembershipID,
			&target.UnitID,
			&target.ClaimID,
		)

		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return nil,
				apierror.New(
					apierror.CodeInventoryUnavailable,
					"Reserved inventory is not currently available from this NON_PUBLIC Allocation",
				)
		}

		if err != nil {
			return nil, err
		}

		result = append(
			result,
			target,
		)
	}

	return result, nil
}

func lockGAIssuanceTargets(
	ctx context.Context,
	tx pgx.Tx,
	allocationID uuid.UUID,
	targets []GATarget,
) ([]gaIssuanceTarget, error) {
	result := make(
		[]gaIssuanceTarget,
		0,
		len(targets),
	)

	for _, requested := range targets {
		var (
			target    gaIssuanceTarget
			available int
		)

		err := tx.QueryRow(
			ctx,
			`
				SELECT
					id,
					ga_pool_id,
					available_quantity
				FROM ga_allocation_buckets
				WHERE allocation_id = $1
				  AND ga_pool_id = $2
				FOR UPDATE
			`,
			allocationID,
			requested.PoolID,
		).Scan(
			&target.BucketID,
			&target.PoolID,
			&available,
		)

		if errors.Is(
			err,
			pgx.ErrNoRows,
		) {
			return nil,
				apierror.New(
					apierror.CodeInventoryUnavailable,
					"GA inventory is not allocated to this NON_PUBLIC Allocation",
				)
		}

		if err != nil {
			return nil, err
		}

		if available <
			requested.Quantity {
			return nil,
				apierror.New(
					apierror.CodeInsufficientGAQuantity,
					"insufficient NON_PUBLIC GA quantity",
				)
		}

		target.Quantity =
			requested.Quantity

		result = append(
			result,
			target,
		)
	}

	return result, nil
}

func totalGAIssuanceQuantity(
	targets []gaIssuanceTarget,
) int {
	total := 0

	for _, target := range targets {
		total += target.Quantity
	}

	return total
}

func (s *Service) createIssuanceTicket(
	ctx context.Context,
	tx pgx.Tx,
	eventID uuid.UUID,
	issuanceItemID uuid.UUID,
	inventoryKind string,
	reservedUnitID *uuid.UUID,
	gaPoolID *uuid.UUID,
	issuedAt time.Time,
) (IssuedTicket, error) {
	ticketID := uuid.New()

	if _, err := tx.Exec(
		ctx,
		`
			INSERT INTO ticket_entitlements (
				id,
				event_id,
				origin_issuance_item_id,
				inventory_kind,
				reserved_inventory_unit_id,
				ga_pool_id,
				status,
				created_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				$5,
				$6,
				'ACTIVE',
				$7
			)
		`,
		ticketID,
		eventID,
		issuanceItemID,
		inventoryKind,
		reservedUnitID,
		gaPoolID,
		issuedAt,
	); err != nil {
		return IssuedTicket{},
			err
	}

	credentialID := uuid.New()
	version := s.qrKeys.ActiveVersion()

	payload, err :=
		buildNonPublicQRPayload(
			s.qrKeys,
			credentialID,
			ticketID,
			eventID,
			version,
		)
	if err != nil {
		return IssuedTicket{},
			err
	}

	hash :=
		auth.TokenHash(
			payload,
		)

	if _, err := tx.Exec(
		ctx,
		`
			INSERT INTO qr_credentials (
				id,
				ticket_entitlement_id,
				token_hash,
				token_key_version,
				status,
				issued_at,
				created_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				'ACTIVE',
				$5,
				$5
			)
		`,
		credentialID,
		ticketID,
		hash[:],
		version,
		issuedAt,
	); err != nil {
		return IssuedTicket{},
			err
	}

	return IssuedTicket{
		TicketID:     ticketID,
		CredentialID: credentialID,
		State:        "ACTIVE",
	}, nil
}

func buildNonPublicQRPayload(
	keys *auth.HMACKeyring,
	credentialID uuid.UUID,
	ticketID uuid.UUID,
	eventID uuid.UUID,
	version int,
) (string, error) {
	mac, err := keys.MAC(
		version,
		auth.Canonical(
			credentialID.String(),
			ticketID.String(),
			eventID.String(),
		),
	)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"qr1.%d.%s.%s",
		version,
		credentialID.String(),
		base64.
			RawURLEncoding.
			EncodeToString(
				mac,
			),
	), nil
}
