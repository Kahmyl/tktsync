package selectionapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/selection"
)

type Dependencies struct {
	Database     *pgxpool.Pool
	Transactions *database.Runner
	Selection    *selection.Service
	Reservation  *reservation.Service
	Availability *inventory.Service
}

type Handler struct {
	db           *pgxpool.Pool
	transactions *database.Runner
	selection    *selection.Service
	reservation  *reservation.Service
	availability *inventory.Service
	mux          *http.ServeMux
}

func New(deps Dependencies) (*Handler, error) {
	if deps.Database == nil || deps.Transactions == nil || deps.Selection == nil || deps.Reservation == nil || deps.Availability == nil {
		return nil, errors.New("Selection API dependencies are incomplete")
	}
	h := &Handler{db: deps.Database, transactions: deps.Transactions, selection: deps.Selection, reservation: deps.Reservation, availability: deps.Availability, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /api/v1/selection/session", h.getSession)
	h.mux.HandleFunc("GET /api/v1/selection/event", h.getEvent)
	h.mux.HandleFunc("GET /api/v1/selection/layout", h.getLayout)
	h.mux.HandleFunc("GET /api/v1/selection/availability", h.getAvailability)
	h.mux.HandleFunc("POST /api/v1/selection/reservations", h.createReservation)
	h.mux.HandleFunc("GET /api/v1/selection/reservations/{reservation_id}", h.getReservation)
	h.mux.HandleFunc("PATCH /api/v1/selection/reservations/{reservation_id}", h.modifyReservation)
	h.mux.HandleFunc("POST /api/v1/selection/reservations/{reservation_id}/release", h.releaseReservation)
	return h, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) authenticate(r *http.Request) (selection.Session, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(header, "Bearer ") {
		return selection.Session{}, apierror.WithStatus(apierror.CodeNotAuthorized, "authentication is required", 401)
	}
	return h.selection.Authenticate(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
}
