package adminapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) registerAllocationRoutes() {
	if h.allocation == nil {
		return
	}

	h.mux.HandleFunc(
		"GET /api/v1/admin/events/{event_id}/restrictions",
		h.listRestrictions,
	)

	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/blocks",
		h.createBlock,
	)

	h.mux.HandleFunc(
		"POST /api/v1/admin/blocks/{block_id}/release",
		h.releaseBlock,
	)

	h.mux.HandleFunc(
		"POST /api/v1/admin/events/{event_id}/allocations",
		h.createAllocation,
	)

	h.mux.HandleFunc(
		"POST /api/v1/admin/allocations/{allocation_id}/release",
		h.releaseAllocation,
	)

	h.mux.HandleFunc(
		"POST /api/v1/admin/allocations/{allocation_id}/reclassify",
		h.reclassifyAllocation,
	)
}

func (h *Handler) listRestrictions(w http.ResponseWriter, r *http.Request) {
	eventID, err := parsePublicID(r.PathValue("event_id"), publicid.Event, "event_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	// Restrictions expose operational channel and partner allocation details,
	// so use the same Event Manager boundary as the corresponding mutations.
	if _, err = h.authorizeRead(r, eventManagerAuthorization(eventID)); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `
		SELECT ir.id,ir.kind,ir.state,ir.purpose,ir.reason,a.mode,a.partner_id,p.name,
		       ir.created_at,ir.released_at,
		       COALESCE((SELECT count(*) FROM block_reserved_units bru WHERE bru.block_id=ir.id),0)
		       + COALESCE((SELECT count(*) FROM allocation_reserved_units aru WHERE aru.allocation_id=ir.id),0),
		       COALESCE((SELECT sum(gbb.assigned_quantity) FROM ga_block_buckets gbb WHERE gbb.block_id=ir.id),0)
		       + COALESCE((SELECT sum(gab.assigned_quantity) FROM ga_allocation_buckets gab WHERE gab.allocation_id=ir.id),0),
		       COALESCE((SELECT jsonb_agg(label ORDER BY label) FROM (
		         SELECT riu.display_label label FROM block_reserved_units bru JOIN reserved_inventory_units riu ON riu.id=bru.reserved_inventory_unit_id WHERE bru.block_id=ir.id
		         UNION ALL SELECT riu.display_label FROM allocation_reserved_units aru JOIN reserved_inventory_units riu ON riu.id=aru.reserved_inventory_unit_id WHERE aru.allocation_id=ir.id
		         UNION ALL SELECT gp.name FROM ga_block_buckets gbb JOIN ga_inventory_pools gp ON gp.id=gbb.ga_pool_id WHERE gbb.block_id=ir.id AND gbb.assigned_quantity>0
		         UNION ALL SELECT gp.name FROM ga_allocation_buckets gab JOIN ga_inventory_pools gp ON gp.id=gab.ga_pool_id WHERE gab.allocation_id=ir.id AND gab.assigned_quantity>0
		       ) labels),'[]'::jsonb)
		FROM inventory_restrictions ir
		LEFT JOIN allocations a ON a.restriction_id=ir.id
		LEFT JOIN partners p ON p.id=a.partner_id
		WHERE ir.event_id=$1 ORDER BY ir.created_at DESC,ir.id DESC
	`, eventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var kind, state, purpose string
		var reason, mode, partnerName *string
		var partnerID *uuid.UUID
		var createdAt time.Time
		var releasedAt *time.Time
		var reservedQuantity, gaQuantity int
		var labels []byte
		if err = rows.Scan(&id, &kind, &state, &purpose, &reason, &mode, &partnerID, &partnerName, &createdAt, &releasedAt, &reservedQuantity, &gaQuantity, &labels); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		var encodedPartner any
		if partnerID != nil {
			encodedPartner = publicid.Encode(publicid.Partner, *partnerID)
		}
		items = append(items, map[string]any{"id": publicid.Encode(func() publicid.Kind {
			if kind == "BLOCK" {
				return publicid.Block
			}
			return publicid.Allocation
		}(), id), "kind": kind, "state": state, "purpose": purpose, "reason": reason, "mode": mode, "partner_id": encodedPartner, "partner_name": partnerName, "reserved_quantity": reservedQuantity, "ga_quantity": gaQuantity, "inventory_labels": rawJSON(labels), "created_at": createdAt, "released_at": releasedAt})
	}
	if err = rows.Err(); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createBlock(
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

	var request createBlockRequest

	if err := decodeJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	reservedIDs, gaTargets, err :=
		parseRestrictionTargets(
			request.ReservedInventoryIDs,
			request.GATargets,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_BLOCK",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			blockID, err :=
				h.allocation.CreateBlock(
					ctx,
					userID,
					eventID,
					allocsvc.BlockInput{
						Purpose:         request.Purpose,
						Reason:          request.Reason,
						ReservedUnitIDs: reservedIDs,
						GATargets:       gaTargets,
					},
				)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Block,
						blockID,
					),
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"state": "ACTIVE",
				},
			)
		},
	)
}

func (h *Handler) releaseBlock(
	w http.ResponseWriter,
	r *http.Request,
) {
	blockID, err := parsePublicID(
		r.PathValue("block_id"),
		publicid.Block,
		"block_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_RELEASE_BLOCK",
		blockID.String(),
		request,
		h.restrictionManagerAuthorization(
			blockID,
			"BLOCK",
		),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.allocation.ReleaseBlock(
				ctx,
				userID,
				blockID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.Block,
						blockID,
					),
					"state": "RELEASED",
				},
			)
		},
	)
}

func (h *Handler) createAllocation(
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

	var request createAllocationRequest

	if err := decodeJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	reservedIDs, gaTargets, err :=
		parseRestrictionTargets(
			request.ReservedInventoryIDs,
			request.GATargets,
		)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var partnerID *uuid.UUID

	if request.PartnerID != "" {
		value, err := parsePublicID(
			request.PartnerID,
			publicid.Partner,
			"partner_id",
		)
		if err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		partnerID = &value
	}

	destinationKind := "SHARED"
	var destinationID *uuid.UUID

	if request.ReleaseDestination != nil {
		destinationKind =
			request.ReleaseDestination.Kind

		if request.ReleaseDestination.
			AllocationID != "" {
			value, err := parsePublicID(
				request.ReleaseDestination.
					AllocationID,
				publicid.Allocation,
				"release_destination.allocation_id",
			)
			if err != nil {
				httpserver.WriteError(
					w,
					r,
					err,
				)
				return
			}

			destinationID = &value
		}
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_ALLOCATION",
		eventID.String(),
		request,
		eventManagerAuthorization(eventID),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			allocationID, err :=
				h.allocation.CreateAllocation(
					ctx,
					userID,
					eventID,
					allocsvc.AllocationInput{
						Mode:                   request.Mode,
						PartnerID:              partnerID,
						Purpose:                request.Purpose,
						Reason:                 request.Reason,
						ReleaseDestinationKind: destinationKind,
						ReleaseDestinationID:   destinationID,
						ReservedUnitIDs:        reservedIDs,
						GATargets:              gaTargets,
					},
				)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Allocation,
						allocationID,
					),
					"event_id": publicid.Encode(
						publicid.Event,
						eventID,
					),
					"mode":  request.Mode,
					"state": "ACTIVE",
				},
			)
		},
	)
}

func (h *Handler) releaseAllocation(
	w http.ResponseWriter,
	r *http.Request,
) {
	allocationID, err := parsePublicID(
		r.PathValue("allocation_id"),
		publicid.Allocation,
		"allocation_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_RELEASE_ALLOCATION",
		allocationID.String(),
		request,
		h.restrictionManagerAuthorization(
			allocationID,
			"ALLOCATION",
		),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err :=
				h.allocation.ReleaseAllocation(
					ctx,
					userID,
					allocationID,
				); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.Allocation,
						allocationID,
					),
					"state": "RELEASED",
				},
			)
		},
	)
}

func (h *Handler) reclassifyAllocation(
	w http.ResponseWriter,
	r *http.Request,
) {
	allocationID, err := parsePublicID(
		r.PathValue("allocation_id"),
		publicid.Allocation,
		"allocation_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var request reclassifyAllocationRequest

	if err := decodeJSON(
		r,
		&request,
	); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	var partnerID *uuid.UUID

	if request.PartnerID != "" {
		value, err := parsePublicID(
			request.PartnerID,
			publicid.Partner,
			"partner_id",
		)
		if err != nil {
			httpserver.WriteError(w, r, err)
			return
		}

		partnerID = &value
	}

	h.runMutation(
		w,
		r,
		"ADMIN_RECLASSIFY_ALLOCATION",
		allocationID.String(),
		request,
		h.restrictionManagerAuthorization(
			allocationID,
			"ALLOCATION",
		),
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err :=
				h.allocation.ReclassifyAllocation(
					ctx,
					userID,
					allocationID,
					request.Mode,
					partnerID,
				); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.Allocation,
						allocationID,
					),
					"mode":  request.Mode,
					"state": "ACTIVE",
				},
			)
		},
	)
}

func (h *Handler) restrictionManagerAuthorization(
	restrictionID uuid.UUID,
	kind string,
) authorizeFunc {
	return func(
		ctx context.Context,
		authorizer *auth.Authorizer,
		userID uuid.UUID,
	) error {
		var eventID uuid.UUID

		err := h.db.QueryRow(
			ctx,
			`
				SELECT event_id
				FROM inventory_restrictions
				WHERE id = $1
				  AND kind = $2
			`,
			restrictionID,
			kind,
		).Scan(&eventID)
		if err != nil {
			if errors.Is(
				err,
				pgx.ErrNoRows,
			) {
				return apierror.New(
					apierror.CodeResourceNotFound,
					"inventory restriction not found",
				)
			}

			return err
		}

		return authorizer.RequireHumanEventRole(
			ctx,
			userID,
			eventID,
			"EVENT_MANAGER",
		)
	}
}

func parseRestrictionTargets(
	reservedValues []string,
	gaValues []gaRestrictionTargetRequest,
) ([]uuid.UUID, []allocsvc.GATarget, error) {
	reserved := make(
		[]uuid.UUID,
		0,
		len(reservedValues),
	)

	for _, value := range reservedValues {
		id, err := parsePublicID(
			value,
			publicid.ReservedInventory,
			"reserved_inventory_id",
		)
		if err != nil {
			return nil, nil, err
		}

		reserved = append(
			reserved,
			id,
		)
	}

	gaTargets := make(
		[]allocsvc.GATarget,
		0,
		len(gaValues),
	)

	for _, item := range gaValues {
		id, err := parsePublicID(
			item.InventoryID,
			publicid.GAPool,
			"ga inventory_id",
		)
		if err != nil {
			return nil, nil, err
		}

		if item.Quantity <= 0 {
			return nil, nil, apierror.New(
				apierror.CodeValidation,
				"GA quantity must be positive",
			)
		}

		gaTargets = append(
			gaTargets,
			allocsvc.GATarget{
				PoolID:   id,
				Quantity: item.Quantity,
			},
		)
	}

	return reserved, gaTargets, nil
}
