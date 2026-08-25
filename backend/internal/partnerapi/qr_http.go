package partnerapi

import (
	"net/http"

	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/ticketqr"
)

func (h *Handler) getTicketQR(
	w http.ResponseWriter,
	r *http.Request,
) {
	ticketID, err := parseTicketID(r.PathValue("ticket_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	principal, err := h.authenticatePartner(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	credential, err := h.reservation.RecoverActiveCredentialForPartner(
		r.Context(),
		principal.PartnerID,
		ticketID,
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	writeTicketQR(w, r, credential)
}

func (h *Handler) getHostedTicketQR(
	w http.ResponseWriter,
	r *http.Request,
) {
	credential, err := h.reservation.RecoverActiveCredentialForPresentation(
		r.Context(),
		r.PathValue("capability"),
	)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}

	writeTicketQR(w, r, credential)
}

func (h *Handler) ticketQRURL(
	credential reservation.ActiveCredential,
) (string, error) {
	capability, err := h.reservation.TicketQRPresentationCapability(
		credential.TicketID,
		credential.CredentialID,
	)
	if err != nil {
		return "", err
	}

	return h.ticketQRPublicBaseURL +
		"/api/v1/ticket-qr/" +
		capability, nil
}

func writeTicketQR(
	w http.ResponseWriter,
	r *http.Request,
	credential reservation.ActiveCredential,
) {
	svg, err := ticketqr.RenderSVG(credential.QRPayload)
	if err != nil {
		httpserver.WriteError(
			w,
			r,
			apierror.New(
				apierror.CodeAuthorityTemporarilyUnavailable,
				"Ticket QR could not be generated",
			),
		)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(svg)
}
