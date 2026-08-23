package selectionapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, 200, map[string]any{"id": publicid.Encode(publicid.SelectionSession, s.ID), "event_id": publicid.Encode(publicid.Event, s.EventID), "buyer_session_ref": s.BuyerSessionRef, "return_url": s.ReturnURL, "state": s.State, "expires_at": s.ExpiresAt, "server_time": serverTime(r.Context(), h.db)})
}

func (h *Handler) getEvent(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var name, state string
	var startsAt, endsAt *time.Time
	if err = h.db.QueryRow(r.Context(), `SELECT name,state,starts_at,ends_at FROM events WHERE id=$1`, s.EventID).Scan(&name, &state, &startsAt, &endsAt); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, 200, map[string]any{"id": publicid.Encode(publicid.Event, s.EventID), "name": name, "state": state, "starts_at": startsAt, "ends_at": endsAt, "server_time": serverTime(r.Context(), h.db)})
}

func (h *Handler) getLayout(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var geometry []byte
	if err = h.db.QueryRow(r.Context(), `SELECT COALESCE(snapshot_json->'geometry','{}'::jsonb) FROM event_layout_snapshots WHERE event_id=$1`, s.EventID).Scan(&geometry); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	rows, err := h.db.Query(r.Context(), `SELECT riu.id,riu.event_section_id,riu.row_label,riu.seat_label,riu.display_label FROM reserved_inventory_units riu JOIN event_sections es ON es.id=riu.event_section_id WHERE riu.event_id=$1 ORDER BY es.sort_order,riu.snapshot_object_key`, s.EventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	reserved := []map[string]any{}
	for rows.Next() {
		var id, section uuid.UUID
		var row *string
		var seat, display string
		if err = rows.Scan(&id, &section, &row, &seat, &display); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		reserved = append(reserved, map[string]any{"inventory_id": publicid.Encode(publicid.ReservedInventory, id), "section_id": publicid.Encode(publicid.EventSection, section), "row": row, "seat": seat, "display_label": display})
	}
	gaRows, err := h.db.Query(r.Context(), `SELECT id,event_section_id,name,capacity FROM ga_inventory_pools WHERE event_id=$1 ORDER BY snapshot_object_key`, s.EventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	defer gaRows.Close()
	ga := []map[string]any{}
	for gaRows.Next() {
		var id, section uuid.UUID
		var name string
		var capacity int
		if err = gaRows.Scan(&id, &section, &name, &capacity); err != nil {
			httpserver.WriteError(w, r, err)
			return
		}
		ga = append(ga, map[string]any{"inventory_id": publicid.Encode(publicid.GAPool, id), "section_id": publicid.Encode(publicid.EventSection, section), "name": name, "capacity": capacity})
	}
	httpserver.WriteJSON(w, 200, map[string]any{"event_id": publicid.Encode(publicid.Event, s.EventID), "geometry": json.RawMessage(geometry), "reserved_units": reserved, "ga_pools": ga})
}

func (h *Handler) getAvailability(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	result, err := h.availability.PartnerAvailability(r.Context(), s.PartnerID, s.EventID)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	reserved := []map[string]any{}
	for _, item := range result.ReservedUnits {
		entry := map[string]any{"inventory_id": publicid.Encode(publicid.ReservedInventory, item.InventoryID), "section_id": publicid.Encode(publicid.EventSection, item.SectionID), "row": item.Row, "seat": item.Seat, "sellability": item.Sellability}
		if item.Offer != nil {
			entry["offer"] = offerJSON(*item.Offer)
		}
		reserved = append(reserved, entry)
	}
	ga := []map[string]any{}
	for _, pool := range result.GAPools {
		offers := []map[string]any{}
		for _, offer := range pool.Offers {
			offers = append(offers, offerJSON(offer))
		}
		ga = append(ga, map[string]any{"inventory_id": publicid.Encode(publicid.GAPool, pool.InventoryID), "name": pool.Name, "offers": offers})
	}
	httpserver.WriteJSON(w, 200, map[string]any{"event_id": publicid.Encode(publicid.Event, result.EventID), "as_of": result.AsOf, "server_time": result.ServerTime, "reserved_units": reserved, "ga_pools": ga})
}

func offerJSON(offer inventory.Offer) map[string]any {
	return map[string]any{"offer_id": offer.OfferID, "available_quantity": offer.AvailableQuantity, "price": map[string]any{"amount_minor": offer.Price.AmountMinor, "currency": offer.Price.Currency}}
}
