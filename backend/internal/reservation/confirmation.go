package reservation

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (s *Service) Confirm(
	ctx context.Context,
	partnerID uuid.UUID,
	rawToken string,
	input ConfirmInput,
) (Confirmation, error) {
	if input.CheckoutAttemptID == uuid.Nil {
		return Confirmation{}, apierror.New(
			apierror.CodeValidation,
			"checkout_attempt_id is required",
		)
	}

	if s.qrKeys == nil {
		return Confirmation{}, apierror.New(
			apierror.CodeAuthorityTemporarilyUnavailable,
			"QR credential authority is not configured",
		)
	}

	input.PartnerOrderRef = strings.TrimSpace(
		input.PartnerOrderRef,
	)
	input.PartnerPaymentRef = strings.TrimSpace(
		input.PartnerPaymentRef,
	)

	parts, err := parseReservationToken(
		rawToken,
	)
	if err != nil {
		return Confirmation{}, err
	}

	var (
		result      Confirmation
		businessErr error
	)

	err = s.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			businessErr = nil

			eventID, err :=
				reservationEventID(
					ctx,
					tx,
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			gate, err := lockEventGate(
				ctx,
				tx,
				eventID,
			)
			if err != nil {
				return err
			}

			record, err :=
				lockReservation(
					ctx,
					tx,
					parts.ReservationID,
				)
			if err != nil {
				return err
			}

			if err :=
				s.verifyLockedToken(
					rawToken,
					parts,
					record,
					partnerID,
				); err != nil {
				return err
			}

			switch gate.State {
			case "ON_SALE",
				"PAUSED",
				"SALES_CLOSED":

			case "CANCELLED":
				return apierror.New(
					apierror.CodeEventCancelled,
					"Cancelled Event cannot accept a new Sale confirmation",
				)

			case "COMPLETED":
				return apierror.New(
					apierror.CodeEventSalesClosed,
					"Completed Event cannot accept a new Sale confirmation",
				)

			default:
				return apierror.New(
					apierror.CodeEventNotOnSale,
					"Event cannot accept this Sale confirmation",
				)
			}

			if record.State == "CONFIRMED" {
				return apierror.New(
					apierror.CodeAlreadyConfirmed,
					"Reservation is already confirmed",
				)
			}

			if record.State != "COMMITTING" &&
				record.State != "RECONCILING" {
				return apierror.New(
					apierror.CodeReservationNotModifiable,
					"Reservation is not in protected checkout or reconciliation",
				)
			}

			items, err := loadActiveItems(
				ctx,
				tx,
				record.ID,
			)
			if err != nil {
				return err
			}

			if len(items) == 0 {
				return apierror.New(
					apierror.CodeInternal,
					"Reservation has no active items to confirm",
				)
			}

			allocations, err :=
				s.lockMutationResources(
					ctx,
					tx,
					record.EventID,
					items,
					nil,
				)
			if err != nil {
				return err
			}

			now, err := clockTimestamp(
				ctx,
				tx,
			)
			if err != nil {
				return err
			}

			switch record.State {
			case "COMMITTING":
				attempt, err :=
					lockCheckoutAttemptByState(
						ctx,
						tx,
						record.ID,
						input.CheckoutAttemptID,
						"ACTIVE",
					)
				if err != nil {
					return err
				}

				if !attempt.
					ProtectionExpiresAt.
					After(now) {
					reconciliation, err :=
						s.transitionToReconciliation(
							ctx,
							tx,
							record,
							gate.Policy,
							attempt,
							now,
							input.PartnerPaymentRef,
							"CHECKOUT_PROTECTION_EXPIRED",
							false,
						)
					if err != nil {
						return err
					}

					if !reconciliation.
						ReconciliationExpiresAt.
						After(now) {
						reconRecord := record
						reconRecord.State =
							"RECONCILING"
						reconRecord.
							ReconciliationExpiresAt =
							&reconciliation.
								ReconciliationExpiresAt

						if err :=
							expireReservationLocked(
								ctx,
								tx,
								s,
								reconRecord,
								items,
								allocations,
								now,
								"RECONCILIATION_EXPIRED",
								false,
							); err != nil {
							return err
						}

						businessErr =
							apierror.New(
								apierror.CodeReconciliationExpired,
								"Reconciliation window has expired",
							)

						return nil
					}

					record.State = "RECONCILING"
					record.ReconciliationExpiresAt =
						&reconciliation.
							ReconciliationExpiresAt
				}

			case "RECONCILING":
				if record.
					ReconciliationExpiresAt ==
					nil ||
					!record.
						ReconciliationExpiresAt.
						After(now) {
					if err :=
						expireReservationLocked(
							ctx,
							tx,
							s,
							record,
							items,
							allocations,
							now,
							"RECONCILIATION_EXPIRED",
							false,
						); err != nil {
						return err
					}

					businessErr =
						apierror.New(
							apierror.CodeReconciliationExpired,
							"Reconciliation window has expired",
						)

					return nil
				}

				if _, err :=
					lockCheckoutAttemptByState(
						ctx,
						tx,
						record.ID,
						input.CheckoutAttemptID,
						"UNCERTAIN",
					); err != nil {
					return err
				}
			}

			var (
				storedOrderRef   *string
				storedPaymentRef *string
			)

			if err := tx.QueryRow(
				ctx,
				`
					SELECT
						r.partner_order_ref,
						ca.partner_payment_ref
					FROM reservations r
					JOIN checkout_attempts ca
					  ON ca.reservation_id = r.id
					 AND ca.id = $2
					WHERE r.id = $1
				`,
				record.ID,
				input.CheckoutAttemptID,
			).Scan(
				&storedOrderRef,
				&storedPaymentRef,
			); err != nil {
				return err
			}

			partnerOrderRef :=
				input.PartnerOrderRef
			if partnerOrderRef == "" &&
				storedOrderRef != nil {
				partnerOrderRef =
					*storedOrderRef
			}

			partnerPaymentRef :=
				input.PartnerPaymentRef
			if partnerPaymentRef == "" &&
				storedPaymentRef != nil {
				partnerPaymentRef =
					*storedPaymentRef
			}

			saleID := uuid.New()

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO sales (
						id,
						reservation_id,
						event_id,
						partner_id,
						partner_order_ref,
						partner_payment_ref,
						currency,
						confirmed_at,
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
						$8,
						$8
					)
				`,
				saleID,
				record.ID,
				record.EventID,
				record.PartnerID,
				partnerOrderRef,
				partnerPaymentRef,
				record.Currency,
				now,
			); err != nil {
				return err
			}

			tickets := make(
				[]ConfirmedTicket,
				0,
				totalActiveItemQuantity(items),
			)

			for _, item := range items {
				saleItemID := uuid.New()

				if _, err := tx.Exec(
					ctx,
					`
						INSERT INTO sale_items (
							id,
							sale_id,
							reservation_item_id,
							event_id,
							inventory_kind,
							reserved_inventory_unit_id,
							ga_pool_id,
							quantity,
							source_allocation_id,
							unit_amount_minor,
							currency,
							created_at
						)
						VALUES (
							$1,
							$2,
							$3,
							$4,
							$5,
							$6,
							$7,
							$8,
							$9,
							$10,
							$11,
							$12
						)
					`,
					saleItemID,
					saleID,
					item.ID,
					record.EventID,
					item.InventoryKind,
					item.ReservedInventoryUnitID,
					item.GAPoolID,
					item.Quantity,
					item.SourceAllocationID,
					item.UnitAmountMinor,
					item.Currency,
					now,
				); err != nil {
					return err
				}

				if err :=
					consumeConfirmedInventory(
						ctx,
						tx,
						item,
						saleItemID,
						now,
					); err != nil {
					return err
				}

				for index := 0; index < item.Quantity; index++ {
					ticketID := uuid.New()

					if _, err := tx.Exec(
						ctx,
						`
							INSERT INTO ticket_entitlements (
								id,
								event_id,
								origin_sale_item_id,
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
						record.EventID,
						saleItemID,
						item.InventoryKind,
						item.ReservedInventoryUnitID,
						item.GAPoolID,
						now,
					); err != nil {
						return err
					}

					credentialID :=
						uuid.New()
					version :=
						s.qrKeys.
							ActiveVersion()

					payload, err :=
						s.buildQRPayload(
							credentialID,
							ticketID,
							record.EventID,
							version,
						)
					if err != nil {
						return err
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
						now,
					); err != nil {
						return err
					}

					tickets = append(
						tickets,
						ConfirmedTicket{
							TicketID:     ticketID,
							CredentialID: credentialID,
							State:        "ACTIVE",
						},
					)
				}
			}

			commandTag, err := tx.Exec(
				ctx,
				`
					UPDATE checkout_attempts
					SET
						state = 'CONFIRMED',
						partner_payment_ref =
						    COALESCE(
						        NULLIF($3, ''),
						        partner_payment_ref
						    ),
						partner_outcome_code =
						    'CONFIRMED',
						completed_at =
						    COALESCE(
						        completed_at,
						        $4
						    )
					WHERE id = $1
					  AND reservation_id = $2
					  AND state IN (
					      'ACTIVE',
					      'UNCERTAIN'
					  )
				`,
				input.CheckoutAttemptID,
				record.ID,
				partnerPaymentRef,
				now,
			)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeValidation,
					"checkout_attempt_id is not the accepted transaction attempt",
				)
			}

			commandTag, err = tx.Exec(
				ctx,
				`
					UPDATE reservations
					SET
						state = 'CONFIRMED',
						confirmed_at = $2,
						payment_retry_expires_at = NULL,
						reconciliation_expires_at = NULL,
						terminal_reason = NULL,
						updated_at = $2
					WHERE id = $1
					  AND state IN (
					      'COMMITTING',
					      'RECONCILING'
					  )
				`,
				record.ID,
				now,
			)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeInternal,
					"Reservation changed during confirmation",
				)
			}

			if err :=
				s.appendConfirmationFact(
					ctx,
					tx,
					record,
					saleID,
					now,
					input.CheckoutAttemptID,
				); err != nil {
				return err
			}

			result = Confirmation{
				ReservationID:     record.ID,
				State:             "CONFIRMED",
				SaleID:            saleID,
				ConfirmedAt:       now,
				PartnerOrderRef:   partnerOrderRef,
				PartnerPaymentRef: partnerPaymentRef,
				Tickets:           tickets,
			}

			return nil
		},
	)
	if err != nil {
		return Confirmation{}, err
	}

	if businessErr != nil {
		return Confirmation{}, businessErr
	}

	return result, nil
}

func totalActiveItemQuantity(
	items []activeItem,
) int {
	total := 0

	for _, item := range items {
		total += item.Quantity
	}

	return total
}

func consumeConfirmedInventory(
	ctx context.Context,
	tx pgx.Tx,
	item activeItem,
	saleItemID uuid.UUID,
	now time.Time,
) error {
	switch item.InventoryKind {
	case InventoryReserved:
		if item.ReservedInventoryUnitID ==
			nil {
			return apierror.New(
				apierror.CodeInternal,
				"Reserved ReservationItem has no inventory identity",
			)
		}

		commandTag, err := tx.Exec(
			ctx,
			`
				UPDATE reserved_inventory_claims
				SET
					ended_at = $3,
					end_reason = 'CONFIRMED_SALE'
				WHERE reserved_inventory_unit_id = $1
				  AND reservation_item_id = $2
				  AND claim_type = 'RESERVATION'
				  AND ended_at IS NULL
			`,
			*item.ReservedInventoryUnitID,
			item.ID,
			now,
		)
		if err != nil {
			return err
		}

		if commandTag.RowsAffected() != 1 {
			return apierror.New(
				apierror.CodeInternal,
				"Reserved Reservation claim is missing during confirmation",
			)
		}

		_, err = tx.Exec(
			ctx,
			`
				INSERT INTO reserved_inventory_claims (
					id,
					reserved_inventory_unit_id,
					claim_type,
					sale_item_id,
					activated_at
				)
				VALUES (
					$1,
					$2,
					'SALE',
					$3,
					$4
				)
			`,
			uuid.New(),
			*item.ReservedInventoryUnitID,
			saleItemID,
			now,
		)

		return err

	case InventoryGA:
		if item.GAPoolID == nil {
			return apierror.New(
				apierror.CodeInternal,
				"GA ReservationItem has no pool identity",
			)
		}

		switch item.SourceKind {
		case SourceShared:
			commandTag, err := tx.Exec(
				ctx,
				`
					UPDATE ga_shared_inventory
					SET
						active_reserved_quantity =
						    active_reserved_quantity - $2,
						sold_current_quantity =
						    sold_current_quantity + $2,
						updated_at = $3
					WHERE ga_pool_id = $1
					  AND active_reserved_quantity >= $2
				`,
				*item.GAPoolID,
				item.Quantity,
				now,
			)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeInternal,
					"Shared GA Reservation accounting is inconsistent during confirmation",
				)
			}

			return nil

		case SourceAllocation:
			if item.
				SourceGAAllocationBucketID ==
				nil {
				return apierror.New(
					apierror.CodeInternal,
					"Allocation GA ReservationItem has no source bucket",
				)
			}

			commandTag, err := tx.Exec(
				ctx,
				`
					UPDATE ga_allocation_buckets
					SET
						active_reserved_quantity =
						    active_reserved_quantity - $3,
						sold_current_quantity =
						    sold_current_quantity + $3,
						updated_at = $4
					WHERE id = $1
					  AND ga_pool_id = $2
					  AND active_reserved_quantity >= $3
				`,
				*item.
					SourceGAAllocationBucketID,
				*item.GAPoolID,
				item.Quantity,
				now,
			)
			if err != nil {
				return err
			}

			if commandTag.RowsAffected() != 1 {
				return apierror.New(
					apierror.CodeInternal,
					"Allocation GA Reservation accounting is inconsistent during confirmation",
				)
			}

			return nil
		}

		return apierror.New(
			apierror.CodeInternal,
			"GA ReservationItem has an unsupported source",
		)
	}

	return apierror.New(
		apierror.CodeInternal,
		"ReservationItem has an unsupported inventory kind",
	)
}

func (s *Service) appendConfirmationFact(
	ctx context.Context,
	tx pgx.Tx,
	record reservationRecord,
	saleID uuid.UUID,
	confirmedAt time.Time,
	checkoutAttemptID uuid.UUID,
) error {
	if _, err := s.audit.Append(
		ctx,
		tx,
		audit.Event{
			EventID:        &record.EventID,
			PartnerID:      &record.PartnerID,
			ActorKind:      "PARTNER",
			ActorPartnerID: &record.PartnerID,
			ReservationID:  &record.ID,
			SaleID:         &saleID,
			Operation:      "RESERVATION_CONFIRMED",
			EntityType:     "SALE",
			EntityID:       &saleID,
			PreviousState: map[string]any{
				"state": record.State,
			},
			NewState: map[string]any{
				"state":               "CONFIRMED",
				"sale_id":             saleID,
				"checkout_attempt_id": checkoutAttemptID,
				"confirmed_at":        confirmedAt,
			},
		},
	); err != nil {
		return err
	}

	_, err := s.outbox.Append(
		ctx,
		tx,
		outbox.Fact{
			EventID:       &record.EventID,
			FactType:      "reservation.confirmed",
			AggregateType: "RESERVATION",
			AggregateID:   &record.ID,
		},
	)

	return err
}
