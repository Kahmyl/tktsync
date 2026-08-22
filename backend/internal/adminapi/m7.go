package adminapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/admission"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) registerM7Routes() {
	if h.admission == nil {
		return
	}
	h.mux.HandleFunc("POST /api/v1/admin/admissions/{admission_id}/reverse", h.reverseAdmission)
	h.mux.HandleFunc("POST /api/v1/admin/admissions/manual-override", h.manualAdmissionOverride)
}

type reverseAdmissionRequest struct {
	Reason string `json:"reason"`
}

func (h *Handler) reverseAdmission(w http.ResponseWriter, r *http.Request) {
	admissionID, err := parsePublicID(r.PathValue("admission_id"), publicid.Admission, "admission_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var request reverseAdmissionRequest
	if err = decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	h.runMutation(w, r, "ADMIN_REVERSE_ADMISSION", admissionID.String(), request, nil, false, func(ctx context.Context, userID uuid.UUID) (response, error) {
		result, reverseErr := h.admission.Reverse(ctx, userID, admissionID, request.Reason)
		if reverseErr != nil {
			return response{}, reverseErr
		}
		return jsonResponse(http.StatusOK, map[string]any{"admission_id": publicid.Encode(publicid.Admission, result.AdmissionID), "ticket_id": publicid.Encode(publicid.Ticket, result.TicketID), "status": "REVERSED", "reversed_at": result.ReversedAt, "reason": result.Reason})
	})
}

type manualAdmissionOverrideRequest struct {
	EventID       string `json:"event_id"`
	TicketID      string `json:"ticket_id"`
	GateReference string `json:"gate_reference,omitempty"`
	Reason        string `json:"reason"`
}

func (h *Handler) manualAdmissionOverride(w http.ResponseWriter, r *http.Request) {
	var request manualAdmissionOverrideRequest
	if err := decodeJSON(r, &request); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	eventID, err := parsePublicID(strings.TrimSpace(request.EventID), publicid.Event, "event_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	ticketID, err := parsePublicID(strings.TrimSpace(request.TicketID), publicid.Ticket, "ticket_id")
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	request.EventID = publicid.Encode(publicid.Event, eventID)
	request.TicketID = publicid.Encode(publicid.Ticket, ticketID)
	request.GateReference = strings.TrimSpace(request.GateReference)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "reason is required"))
		return
	}
	h.runMutation(w, r, "ADMIN_MANUAL_ADMISSION_OVERRIDE", eventID.String()+":"+ticketID.String(), request, nil, false, func(ctx context.Context, userID uuid.UUID) (response, error) {
		result, overrideErr := h.admission.ManualOverride(ctx, userID, admission.ManualOverrideInput{EventID: eventID, TicketID: ticketID, GateReference: request.GateReference, Reason: request.Reason})
		if overrideErr != nil {
			return response{}, overrideErr
		}
		return jsonResponse(http.StatusOK, map[string]any{"result": result.Result, "scan_attempt_id": publicid.Encode(publicid.ScanAttempt, result.ScanAttemptID), "admission_id": publicid.Encode(publicid.Admission, *result.AdmissionID), "ticket": map[string]any{"id": publicid.Encode(publicid.Ticket, *result.TicketID), "display": result.TicketDisplay}, "admitted_at": result.AdmittedAt})
	})
}
