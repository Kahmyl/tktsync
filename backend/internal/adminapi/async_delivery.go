package adminapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) registerAsyncDeliveryRoutes() {
	if h.webhook == nil {
		return
	}
	h.mux.HandleFunc("POST /api/v1/admin/partners/{partner_id}/webhook-endpoints", h.createWebhookEndpoint)
	h.mux.HandleFunc("GET /api/v1/admin/partners/{partner_id}/webhook-endpoints", h.listWebhookEndpoints)
	h.mux.HandleFunc("POST /api/v1/admin/webhook-endpoints/{endpoint_id}/secret/rotate", h.rotateWebhookSecret)
	h.mux.HandleFunc("POST /api/v1/admin/webhook-endpoints/{endpoint_id}/disable", h.disableWebhookEndpoint)
	h.mux.HandleFunc("PUT /api/v1/admin/webhook-endpoints/{endpoint_id}/subscriptions", h.replaceWebhookSubscriptions)
}

type createWebhookEndpointRequest struct {
	URL           string   `json:"url"`
	Subscriptions []string `json:"subscriptions"`
}

func (h *Handler) createWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	partnerID, err := parsePublicID(r.PathValue("partner_id"), publicid.Partner, "partner_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var request createWebhookEndpointRequest
	if err = decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.URL = strings.TrimSpace(request.URL)
	h.runMutation(w, r, "ADMIN_CREATE_WEBHOOK_ENDPOINT", partnerID.String(), request, platformAdminAuthorization, true, func(ctx context.Context, userID uuid.UUID) (response, error) {
		endpoint, createErr := h.webhook.CreateEndpoint(ctx, userID, partnerID, request.URL, request.Subscriptions)
		if createErr != nil {
			return response{}, createErr
		}
		return jsonResponse(http.StatusCreated, map[string]any{"id": publicid.Encode(publicid.WebhookEndpoint, endpoint.ID), "partner_id": publicid.Encode(publicid.Partner, endpoint.PartnerID), "url": endpoint.URL, "state": endpoint.State, "signing_secret": endpoint.Secret, "subscriptions": endpoint.Subscriptions, "created_at": endpoint.CreatedAt})
	})
}

func (h *Handler) listWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	partnerID, err := parsePublicID(r.PathValue("partner_id"), publicid.Partner, "partner_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if _, err = h.authorizeRead(r, platformAdminAuthorization); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT e.id,e.url,e.state,e.created_at,COALESCE(array_agg(s.event_type ORDER BY s.event_type) FILTER(WHERE s.event_type IS NOT NULL),'{}') FROM partner_webhook_endpoints e LEFT JOIN partner_webhook_subscriptions s ON s.webhook_endpoint_id=e.id WHERE e.partner_id=$1 GROUP BY e.id ORDER BY e.created_at,e.id`, partnerID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var url, state string
		var createdAt time.Time
		var subscriptions []string
		if err = rows.Scan(&id, &url, &state, &createdAt, &subscriptions); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": publicid.Encode(publicid.WebhookEndpoint, id), "partner_id": publicid.Encode(publicid.Partner, partnerID), "url": url, "state": state, "subscriptions": subscriptions, "created_at": createdAt})
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func parseWebhookEndpointID(r *http.Request) (uuid.UUID, error) {
	return parsePublicID(r.PathValue("endpoint_id"), publicid.WebhookEndpoint, "endpoint_id")
}
func (h *Handler) rotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	endpointID, err := parseWebhookEndpointID(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request := struct{}{}
	if err = decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	h.runMutation(w, r, "ADMIN_ROTATE_WEBHOOK_SECRET", endpointID.String(), request, platformAdminAuthorization, true, func(ctx context.Context, userID uuid.UUID) (response, error) {
		secret, activatedAt, rotateErr := h.webhook.RotateSecret(ctx, userID, endpointID)
		if rotateErr != nil {
			return response{}, rotateErr
		}
		return jsonResponse(http.StatusOK, map[string]any{"endpoint_id": publicid.Encode(publicid.WebhookEndpoint, endpointID), "signing_secret": secret, "activated_at": activatedAt})
	})
}

type disableWebhookRequest struct {
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) disableWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := parseWebhookEndpointID(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var request disableWebhookRequest
	if err = decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	h.runMutation(w, r, "ADMIN_DISABLE_WEBHOOK_ENDPOINT", endpointID.String(), request, platformAdminAuthorization, false, func(ctx context.Context, userID uuid.UUID) (response, error) {
		if disableErr := h.webhook.DisableEndpoint(ctx, userID, endpointID, request.Reason); disableErr != nil {
			return response{}, disableErr
		}
		return jsonResponse(http.StatusOK, map[string]any{"endpoint_id": publicid.Encode(publicid.WebhookEndpoint, endpointID), "state": "DISABLED"})
	})
}

type subscriptionsRequest struct {
	Subscriptions []string `json:"subscriptions"`
}

func (h *Handler) replaceWebhookSubscriptions(w http.ResponseWriter, r *http.Request) {
	endpointID, err := parseWebhookEndpointID(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var request subscriptionsRequest
	if err = decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	h.runMutation(w, r, "ADMIN_REPLACE_WEBHOOK_SUBSCRIPTIONS", endpointID.String(), request, platformAdminAuthorization, false, func(ctx context.Context, userID uuid.UUID) (response, error) {
		if replaceErr := h.webhook.ReplaceSubscriptions(ctx, userID, endpointID, request.Subscriptions); replaceErr != nil {
			return response{}, replaceErr
		}
		return jsonResponse(http.StatusOK, map[string]any{"endpoint_id": publicid.Encode(publicid.WebhookEndpoint, endpointID), "subscriptions": request.Subscriptions})
	})
}
