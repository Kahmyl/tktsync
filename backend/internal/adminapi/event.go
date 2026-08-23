package adminapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) createEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request createEventRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	venueID, err := parsePublicID(
		request.VenueID,
		publicid.Venue,
		"venue_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_EVENT",
		venueID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.event.Create(
				ctx,
				userID,
				eventsvc.CreateInput{
					VenueID:          venueID,
					Name:             request.Name,
					StartsAt:         request.StartsAt,
					EndsAt:           request.EndsAt,
					SalesOpenAt:      request.SalesOpenAt,
					SalesCloseAt:     request.SalesCloseAt,
					AdmissionOpenAt:  request.AdmissionOpenAt,
					AdmissionCloseAt: request.AdmissionCloseAt,
					TimezoneName:     request.TimezoneName,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Event,
						id,
					),
					"state": "DRAFT",
				},
			)
		},
	)
}

func (h *Handler) getEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if _, err := h.authorizeRead(
		r,
		eventReadAuthorization(eventID),
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	result, err := h.eventResponse(
		r.Context(),
		eventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		result,
	)
}

func (h *Handler) updateEvent(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request updateEventRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_UPDATE_EVENT_CONFIGURATION",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.event.UpdateConfiguration(
				ctx,
				userID,
				eventID,
				eventsvc.UpdateConfigurationInput{
					Name:             request.Name,
					StartsAt:         request.StartsAt,
					EndsAt:           request.EndsAt,
					SalesOpenAt:      request.SalesOpenAt,
					SalesCloseAt:     request.SalesCloseAt,
					AdmissionOpenAt:  request.AdmissionOpenAt,
					AdmissionCloseAt: request.AdmissionCloseAt,
					TimezoneName:     request.TimezoneName,
				},
			); err != nil {
				return response{}, err
			}

			value, err := h.eventResponse(
				ctx,
				eventID,
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				value,
			)
		},
	)
}

func (h *Handler) materializeLayout(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request materializeLayoutRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	layoutID, err := parsePublicID(
		request.LayoutID,
		publicid.VenueLayout,
		"layout_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_MATERIALIZE_EVENT_LAYOUT",
		eventID.String()+":"+layoutID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.event.MaterializeLayout(
				ctx,
				userID,
				eventID,
				layoutID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"layout_id": publicid.Encode(
						publicid.VenueLayout,
						layoutID,
					),
					"materialized": true,
				},
			)
		},
	)
}

func (h *Handler) getEventInventory(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	if _, err := h.authorizeRead(
		r,
		eventReadAuthorization(eventID),
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	rows, err := h.db.Query(
		r.Context(),
		`
			SELECT
				'RESERVED'::text,
				riu.id,
				riu.snapshot_object_key,
				riu.display_label,
				1::integer,
				COALESCE(
					riu.price_tier_override_id,
					es.default_price_tier_id
				)
			FROM reserved_inventory_units riu
			JOIN event_sections es
			  ON es.id = riu.event_section_id
			WHERE riu.event_id = $1

			UNION ALL

			SELECT
				'GA'::text,
				gp.id,
				gp.snapshot_object_key,
				gp.name,
				gp.capacity,
				gp.price_tier_id
			FROM ga_inventory_pools gp
			WHERE gp.event_id = $1

			ORDER BY 1, 3
		`,
		eventID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)

	for rows.Next() {
		var (
			kind        string
			id          uuid.UUID
			objectKey   string
			label       string
			quantity    int
			priceTierID *uuid.UUID
		)

		if err := rows.Scan(
			&kind,
			&id,
			&objectKey,
			&label,
			&quantity,
			&priceTierID,
		); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		idKind := publicid.ReservedInventory
		if kind == "GA" {
			idKind = publicid.GAPool
		}

		var encodedPriceTier any
		if priceTierID != nil {
			encodedPriceTier = publicid.Encode(
				publicid.PriceTier,
				*priceTierID,
			)
		}

		items = append(
			items,
			map[string]any{
				"kind":                kind,
				"id":                  publicid.Encode(idKind, id),
				"snapshot_object_key": objectKey,
				"label":               label,
				"quantity":            quantity,
				"price_tier_id":       encodedPriceTier,
			},
		)
	}

	httpserver.WriteJSON(
		w,
		http.StatusOK,
		map[string]any{
			"event_id": publicid.Encode(
				publicid.Event,
				eventID,
			),
			"inventory": items,
		},
	)
}

func (h *Handler) configureTransactionPolicy(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request transactionPolicyRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CONFIGURE_EVENT_TRANSACTION_POLICY",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.event.ConfigureTransactionPolicy(
				ctx,
				userID,
				eventID,
				eventsvc.TransactionPolicyInput{
					HoldDurationSeconds:                  request.HoldDurationSeconds,
					CheckoutProtectionSeconds:            request.CheckoutProtectionSeconds,
					PaymentRetrySeconds:                  request.PaymentRetrySeconds,
					ReconciliationSeconds:                request.ReconciliationSeconds,
					MaxReservationLifetimeSeconds:        request.MaxReservationLifetimeSeconds,
					MaxHoldQuantity:                      request.MaxHoldQuantity,
					MaxActiveReservationsPerPartner:      request.MaxActiveReservationsPerPartner,
					MaxActiveReservationsPerBuyerSession: request.MaxActiveReservationsPerBuyerSession,
					AllowVoidedInventoryRerelease:        request.AllowVoidedInventoryRerelease,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"configured": true,
				},
			)
		},
	)
}

func (h *Handler) createPriceTier(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request createPriceTierRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_EVENT_PRICE_TIER",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.event.CreatePriceTier(
				ctx,
				userID,
				eventID,
				eventsvc.PriceTierInput{
					Code:        request.Code,
					Name:        request.Name,
					AmountMinor: request.AmountMinor,
					Currency:    request.Currency,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.PriceTier,
						id,
					),
				},
			)
		},
	)
}

func (h *Handler) updatePriceTier(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	priceTierID, err := parsePublicID(
		r.PathValue("price_tier_id"),
		publicid.PriceTier,
		"price_tier_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request updatePriceTierRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_UPDATE_EVENT_PRICE_TIER",
		eventID.String()+":"+priceTierID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.event.UpdatePriceTier(
				ctx,
				userID,
				eventID,
				priceTierID,
				eventsvc.UpdatePriceTierInput{
					Name:        request.Name,
					AmountMinor: request.AmountMinor,
					State:       request.State,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.PriceTier,
						priceTierID,
					),
					"updated": true,
				},
			)
		},
	)
}

func (h *Handler) assignPricing(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request pricingAssignmentRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	priceTierID, err := parsePublicID(
		request.PriceTierID,
		publicid.PriceTier,
		"price_tier_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_ASSIGN_EVENT_PRICING",
		eventID.String()+":"+priceTierID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			err := h.event.AssignPricing(
				ctx,
				userID,
				eventID,
				eventsvc.PricingAssignmentInput{
					PriceTierID:        priceTierID,
					SectionObjectKeys:  request.SectionObjectKeys,
					ReservedObjectKeys: request.ReservedObjectKeys,
					GAPoolObjectKeys:   request.GAPoolObjectKeys,
				},
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"assigned": true,
				},
			)
		},
	)
}

func (h *Handler) openSales(
	w http.ResponseWriter,
	r *http.Request,
) {
	eventID, err := parsePublicID(
		r.PathValue("event_id"),
		publicid.Event,
		"event_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_OPEN_EVENT_SALES",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.event.OpenSales(
				ctx,
				userID,
				eventID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"state": "ON_SALE",
				},
			)
		},
	)
}

func (h *Handler) pauseSales(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_PAUSE_EVENT_SALES", "PAUSED", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.PauseSales(ctx, actorID, eventID)
	})
}

func (h *Handler) resumeSales(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_RESUME_EVENT_SALES", "ON_SALE", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.ResumeSales(ctx, actorID, eventID)
	})
}

func (h *Handler) closeSales(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_CLOSE_EVENT_SALES", "SALES_CLOSED", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.CloseSales(ctx, actorID, eventID)
	})
}

func (h *Handler) completeEvent(w http.ResponseWriter, r *http.Request) {
	h.runEventLifecycleCommand(w, r, "ADMIN_COMPLETE_EVENT", "COMPLETED", nil, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.CompleteEvent(ctx, actorID, eventID)
	})
}

func (h *Handler) cancelEvent(w http.ResponseWriter, r *http.Request) {
	var request cancelEventRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "reason is required"))
		return
	}
	h.runEventLifecycleCommand(w, r, "ADMIN_CANCEL_EVENT", "CANCELLED", request, func(ctx context.Context, actorID, eventID uuid.UUID) error {
		return h.event.CancelEvent(ctx, actorID, eventID, request.Reason)
	})
}

func (h *Handler) runEventLifecycleCommand(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	nextState string,
	request any,
	command func(context.Context, uuid.UUID, uuid.UUID) error,
) {
	eventID, err := parsePublicID(r.PathValue("event_id"), publicid.Event, "event_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if request == nil {
		request = struct{}{}
	}
	h.runMutation(w, r, operation, eventID.String(), request, eventManagerAuthorization(eventID), false, func(ctx context.Context, userID uuid.UUID) (response, error) {
		if err := command(ctx, userID, eventID); err != nil {
			return response{}, err
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"event_id": publicid.Encode(publicid.Event, eventID),
			"state":    nextState,
		})
	})
}
