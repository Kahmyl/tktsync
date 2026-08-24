package admissionapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/admission"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

type HumanAuthenticator func(context.Context, string) (auth.HumanPrincipal, error)

type Dependencies struct {
	Database     *pgxpool.Pool
	Transactions *database.Runner
	HumanAuth    HumanAuthenticator
	Admission    *admission.Service
}

type Handler struct {
	db           *pgxpool.Pool
	transactions *database.Runner
	humanAuth    HumanAuthenticator
	admission    *admission.Service
	idempotency  idempotency.Store
	mux          *http.ServeMux
}

func New(deps Dependencies) (*Handler, error) {
	if deps.Database == nil || deps.Transactions == nil || deps.Admission == nil {
		return nil, errors.New("admission API dependencies are incomplete")
	}
	h := &Handler{db: deps.Database, transactions: deps.Transactions, humanAuth: deps.HumanAuth, admission: deps.Admission, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/admission/events", h.events)
	h.mux.HandleFunc("POST /api/v1/admission/scans", h.scan)
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

type scanRequest struct {
	EventID       string `json:"event_id"`
	Credential    string `json:"credential"`
	GateReference string `json:"gate_reference,omitempty"`
}

func (h *Handler) scan(w http.ResponseWriter, r *http.Request) {
	principal, authErr := h.authenticateHuman(r)
	if authErr != nil {
		httpserver.WriteError(w, r, authErr)
		return
	}
	var request scanRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "request body is invalid"))
		return
	}
	request.EventID = strings.TrimSpace(request.EventID)
	request.Credential = strings.TrimSpace(request.Credential)
	request.GateReference = strings.TrimSpace(request.GateReference)
	eventID, err := publicid.Parse(request.EventID, publicid.Event)
	if err != nil {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "event_id is invalid"))
		return
	}
	request.EventID = publicid.Encode(publicid.Event, eventID)
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "Idempotency-Key header is required"))
		return
	}
	canonical, err := json.Marshal(request)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var body json.RawMessage
	err = h.transactions.Run(r.Context(), func(tx pgx.Tx) error {
		authorizer := auth.NewAuthorizer(tx)
		user, err := authorizer.ResolveHuman(r.Context(), principal)
		if err != nil {
			return err
		}
		if err = authorizer.RequireHumanEventRole(r.Context(), user.ID, eventID, "SCANNER", "GATE_SUPERVISOR", "EVENT_MANAGER"); err != nil {
			return err
		}
		claim, err := h.idempotency.Claim(r.Context(), tx, idempotency.Scope{Kind: idempotency.ScopeUser, ID: user.ID}, "ADMISSION_SCAN", idempotencyKey, idempotency.Fingerprint(canonical))
		if err != nil {
			return err
		}
		if !claim.Owner {
			if claim.Replay == nil {
				return errors.New("idempotency replay result is missing")
			}
			body = append(json.RawMessage(nil), claim.Replay.Payload...)
			return nil
		}
		result, err := h.admission.ValidateAndAdmit(database.WithTransaction(r.Context(), tx), admission.ScanInput{EventID: eventID, Credential: request.Credential, GateReference: request.GateReference, ScannerUserID: user.ID, IdempotencyOperationID: claim.ID})
		if err != nil {
			return err
		}
		response := map[string]any{"result": result.Result, "scan_attempt_id": publicid.Encode(publicid.ScanAttempt, result.ScanAttemptID)}
		if result.TicketID != nil {
			response["ticket"] = map[string]any{"id": publicid.Encode(publicid.Ticket, *result.TicketID), "display": result.TicketDisplay}
		}
		if result.AdmissionID != nil {
			response["admission_id"] = publicid.Encode(publicid.Admission, *result.AdmissionID)
		}
		if result.AdmittedAt != nil {
			response["admitted_at"] = result.AdmittedAt
		}
		if result.PreviousAdmittedAt != nil {
			response["previous_admission"] = map[string]any{"admitted_at": result.PreviousAdmittedAt, "gate_reference": result.PreviousGate}
		}
		body, err = json.Marshal(response)
		if err != nil {
			return err
		}
		return h.idempotency.CompleteSuccess(r.Context(), tx, claim.ID, result.Result, "SCAN_ATTEMPT", &result.ScanAttemptID, response)
	})
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpserver.WriteJSON(w, http.StatusOK, json.RawMessage(body))
}
