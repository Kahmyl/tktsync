package adminapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) createPartner(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request createPartnerRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_PARTNER",
		"",
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			id, err := h.partner.Create(
				ctx,
				userID,
				request.Name,
			)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.Partner,
						id,
					),
					"state": "ACTIVE",
				},
			)
		},
	)
}

func (h *Handler) createPartnerCredential(
	w http.ResponseWriter,
	r *http.Request,
) {
	partnerID, err := parsePublicID(
		r.PathValue("partner_id"),
		publicid.Partner,
		"partner_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_CREATE_PARTNER_CREDENTIAL",
		partnerID.String(),
		request,
		platformAdminAuthorization,
		true,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if h.replayProtector == nil {
				return response{}, apierror.New(
					apierror.
						CodeAuthorityTemporarilyUnavailable,
					"credential replay protection is not configured",
				)
			}

			credentialID, rawCredential, err :=
				h.partner.CreateCredential(
					ctx,
					userID,
					partnerID,
				)
			if err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusCreated,
				map[string]any{
					"id": publicid.Encode(
						publicid.PartnerCredential,
						credentialID,
					),
					"partner_id": publicid.Encode(
						publicid.Partner,
						partnerID,
					),
					"credential": rawCredential,
				},
			)
		},
	)
}

func (h *Handler) revokePartnerCredential(
	w http.ResponseWriter,
	r *http.Request,
) {
	credentialID, err := parsePublicID(
		r.PathValue("credential_id"),
		publicid.PartnerCredential,
		"credential_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		"ADMIN_REVOKE_PARTNER_CREDENTIAL",
		credentialID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.partner.RevokeCredential(
				ctx,
				userID,
				credentialID,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.PartnerCredential,
						credentialID,
					),
					"state": "REVOKED",
				},
			)
		},
	)
}

func (h *Handler) disablePartner(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEnabled(w, r, false)
}

func (h *Handler) enablePartner(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEnabled(w, r, true)
}

func (h *Handler) setPartnerEnabled(
	w http.ResponseWriter,
	r *http.Request,
	enabled bool,
) {
	partnerID, err := parsePublicID(
		r.PathValue("partner_id"),
		publicid.Partner,
		"partner_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	operation := "ADMIN_DISABLE_PARTNER"
	state := "DISABLED"

	if enabled {
		operation = "ADMIN_ENABLE_PARTNER"
		state = "ACTIVE"
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		operation,
		partnerID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			if err := h.partner.SetEnabled(
				ctx,
				userID,
				partnerID,
				enabled,
			); err != nil {
				return response{}, err
			}

			return jsonResponse(
				http.StatusOK,
				map[string]any{
					"id": publicid.Encode(
						publicid.Partner,
						partnerID,
					),
					"state": state,
				},
			)
		},
	)
}

func (h *Handler) enablePartnerEventAccess(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEventAccess(w, r, true)
}

func (h *Handler) disablePartnerEventAccess(
	w http.ResponseWriter,
	r *http.Request,
) {
	h.setPartnerEventAccess(w, r, false)
}

func (h *Handler) setPartnerEventAccess(
	w http.ResponseWriter,
	r *http.Request,
	enabled bool,
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

	partnerID, err := parsePublicID(
		r.PathValue("partner_id"),
		publicid.Partner,
		"partner_id",
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	operation := "ADMIN_DISABLE_PARTNER_EVENT_ACCESS"
	state := "DISABLED"

	if enabled {
		operation = "ADMIN_ENABLE_PARTNER_EVENT_ACCESS"
		state = "ACTIVE"
	}

	request := struct{}{}

	h.runMutation(
		w,
		r,
		operation,
		eventID.String()+":"+partnerID.String(),
		request,
		platformAdminAuthorization,
		false,
		func(
			ctx context.Context,
			userID uuid.UUID,
		) (response, error) {
			var err error

			if enabled {
				err = h.partner.GrantEventAccess(
					ctx,
					userID,
					eventID,
					partnerID,
				)
			} else {
				err = h.partner.DisableEventAccess(
					ctx,
					userID,
					eventID,
					partnerID,
				)
			}

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
					"partner_id": publicid.Encode(
						publicid.Partner,
						partnerID,
					),
					"state": state,
				},
			)
		},
	)
}
