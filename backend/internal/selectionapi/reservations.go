package selectionapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/reservation"
	"github.com/tktsync/tktsync/backend/internal/selection"
)

type itemRequest struct {
	OfferID  string `json:"offer_id"`
	Quantity int    `json:"quantity"`
}
type createRequest struct {
	Items []itemRequest `json:"items"`
}
type adjustRequest struct {
	ReservationItemID string `json:"reservation_item_id"`
	NewQuantity       int    `json:"new_quantity"`
}
type modifyRequest struct {
	RemoveItemIDs    []string        `json:"remove_item_ids,omitempty"`
	AdjustQuantities []adjustRequest `json:"adjust_quantities,omitempty"`
	AddItems         []itemRequest   `json:"add_items,omitempty"`
}

func (h *Handler) createReservation(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var req createRequest
	if err = decode(r, &req); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	normalizeItems(req.Items)
	h.runMutation(w, r, s, "SELECTION_CREATE_RESERVATION", req, func(ctx context.Context) (map[string]any, *uuid.UUID, error) {
		items, err := h.resolveItems(ctx, s, req.Items)
		if err != nil {
			return nil, nil, err
		}
		created, err := h.reservation.Create(ctx, reservation.CreateInput{EventID: s.EventID, PartnerID: s.PartnerID, BuyerSelectionSessionID: &s.ID, BuyerSessionRef: s.BuyerSessionRef, Items: items})
		if err != nil {
			return nil, nil, err
		}
		view, err := h.loadReservation(ctx, s, created.ReservationID)
		if err != nil {
			return nil, nil, err
		}
		view["reservation_token"] = created.Token
		return view, &created.ReservationID, nil
	})
}

func (h *Handler) getReservation(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	id, err := parseReservation(r.PathValue("reservation_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if err = h.reservation.MaterializeDue(r.Context(), id); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	view, err := h.loadReservation(r.Context(), s, id)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, 200, view)
}

func (h *Handler) modifyReservation(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	id, err := parseReservation(r.PathValue("reservation_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	token, err := reservationToken(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var req modifyRequest
	if err = decode(r, &req); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	sort.Strings(req.RemoveItemIDs)
	h.runMutation(w, r, s, "SELECTION_MODIFY_RESERVATION", struct {
		Request   modifyRequest `json:"request"`
		TokenHash string        `json:"reservation_token"`
	}{req, reservationTokenFingerprint(token)}, func(ctx context.Context) (map[string]any, *uuid.UUID, error) {
		current, eventID, err := h.loadItemInputs(ctx, s, id)
		if err != nil {
			return nil, nil, err
		}
		if eventID != s.EventID {
			return nil, nil, apierror.New(apierror.CodeHoldNotOwned, "Reservation is outside this selection session")
		}
		byID := map[uuid.UUID]int{}
		for i, item := range current {
			byID[*item.ReservationItemID] = i
		}
		removed := map[uuid.UUID]bool{}
		for _, raw := range req.RemoveItemIDs {
			x, e := publicid.Parse(raw, publicid.ReservationItem)
			_, ok := byID[x]
			if e != nil || !ok {
				return nil, nil, apierror.New(apierror.CodeValidation, "remove_item_ids is invalid")
			}
			removed[x] = true
		}
		for _, adj := range req.AdjustQuantities {
			x, e := publicid.Parse(adj.ReservationItemID, publicid.ReservationItem)
			i, ok := byID[x]
			if e != nil || !ok || adj.NewQuantity <= 0 || removed[x] {
				return nil, nil, apierror.New(apierror.CodeValidation, "adjust_quantities is invalid")
			}
			current[i].Quantity = adj.NewQuantity
		}
		final := []reservation.ItemInput{}
		for _, item := range current {
			if !removed[*item.ReservationItemID] {
				final = append(final, item)
			}
		}
		added, e := h.resolveItems(ctx, s, req.AddItems)
		if e != nil {
			return nil, nil, e
		}
		final = append(final, added...)
		if _, e = h.reservation.Modify(ctx, s.PartnerID, token, final); e != nil {
			return nil, nil, e
		}
		view, e := h.loadReservation(ctx, s, id)
		return view, &id, e
	})
}

func (h *Handler) releaseReservation(w http.ResponseWriter, r *http.Request) {
	s, err := h.authenticate(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	id, err := parseReservation(r.PathValue("reservation_id"))
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	token, err := reservationToken(r)
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	var empty struct{}
	if err = decode(r, &empty); err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	h.runMutation(w, r, s, "SELECTION_RELEASE_RESERVATION", map[string]string{"reservation_id": id.String(), "reservation_token": reservationTokenFingerprint(token)}, func(ctx context.Context) (map[string]any, *uuid.UUID, error) {
		if _, _, e := h.loadItemInputs(ctx, s, id); e != nil {
			return nil, nil, e
		}
		if e := h.reservation.Release(ctx, s.PartnerID, token); e != nil {
			return nil, nil, e
		}
		return map[string]any{"reservation_id": publicid.Encode(publicid.Reservation, id), "status": "RELEASED", "server_time": serverTime(ctx, h.db)}, &id, nil
	})
}

type mutateFunc func(context.Context) (map[string]any, *uuid.UUID, error)

func (h *Handler) runMutation(w http.ResponseWriter, r *http.Request, s selection.Session, operation string, intent any, mutate mutateFunc) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpserver.WriteError(w, r, apierror.New(apierror.CodeValidation, "Idempotency-Key header is required"))
		return
	}
	raw, _ := json.Marshal(intent)
	var body map[string]any
	var businessErr error
	var status = 200
	if operation == "SELECTION_CREATE_RESERVATION" {
		status = http.StatusCreated
	}
	err := h.transactions.Run(r.Context(), func(tx pgx.Tx) error {
		var authorized uuid.UUID
		if e := tx.QueryRow(r.Context(), `SELECT id FROM buyer_selection_sessions WHERE id=$1 AND partner_id=$2 AND event_id=$3 AND state='ACTIVE' AND expires_at>clock_timestamp() FOR UPDATE`, s.ID, s.PartnerID, s.EventID).Scan(&authorized); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return apierror.WithStatus(apierror.CodeNotAuthorized, "Selection capability is invalid or expired", http.StatusUnauthorized)
			}
			return e
		}
		store := idempotency.Store{}
		claim, e := store.Claim(r.Context(), tx, idempotency.Scope{Kind: idempotency.ScopeBuyerSession, ID: s.ID}, operation, key, idempotency.Fingerprint(raw))
		if e != nil {
			return e
		}
		if !claim.Owner {
			if claim.Replay == nil {
				return errors.New("idempotency replay is missing")
			}
			if claim.Replay.ExecutionState == "FAILED_BUSINESS" {
				var stored struct {
					Message    string         `json:"message"`
					Details    map[string]any `json:"details,omitempty"`
					HTTPStatus int            `json:"http_status"`
				}
				if e = json.Unmarshal(claim.Replay.Payload, &stored); e != nil {
					return e
				}
				replayed := apierror.New(apierror.Code(claim.Replay.Code), stored.Message)
				replayed.Details = stored.Details
				if stored.HTTPStatus != 0 {
					replayed.HTTPStatus = stored.HTTPStatus
				}
				businessErr = replayed
				return nil
			}
			if e = json.Unmarshal(claim.Replay.Payload, &body); e != nil {
				return e
			}
			if claim.Replay.EntityID != nil && operation == "SELECTION_CREATE_RESERVATION" {
				token, e := h.reservation.RecoverToken(database.WithTransaction(r.Context(), tx), *claim.Replay.EntityID, s.PartnerID)
				if e != nil {
					return e
				}
				body["reservation_token"] = token
			}
			return nil
		}
		businessTx, e := tx.Begin(r.Context())
		if e != nil {
			return e
		}
		ctx := database.WithTransaction(r.Context(), businessTx)
		var entity *uuid.UUID
		body, entity, e = mutate(ctx)
		if e != nil {
			apiErr, ok := apierror.As(e)
			if !ok {
				_ = businessTx.Rollback(r.Context())
				return e
			}
			switch apiErr.Code {
			case apierror.CodeHoldExpired, apierror.CodeEventCancelled:
				if commitErr := businessTx.Commit(r.Context()); commitErr != nil {
					return commitErr
				}
			default:
				if rollbackErr := businessTx.Rollback(r.Context()); rollbackErr != nil {
					return rollbackErr
				}
			}
			stored := map[string]any{"message": apiErr.Message, "details": apiErr.Details, "http_status": apiErr.HTTPStatus}
			if completeErr := store.CompleteBusinessFailure(r.Context(), tx, claim.ID, string(apiErr.Code), stored); completeErr != nil {
				return completeErr
			}
			businessErr = apiErr
			return nil
		}
		if e = businessTx.Commit(r.Context()); e != nil {
			return e
		}
		stored := body
		if operation == "SELECTION_CREATE_RESERVATION" {
			stored = cloneMap(body)
			delete(stored, "reservation_token")
		}
		return store.CompleteSuccess(r.Context(), tx, claim.ID, "OK", "RESERVATION", entity, stored)
	})
	if err != nil {
		httpserver.WriteError(w, r, err)
		return
	}
	if businessErr != nil {
		httpserver.WriteError(w, r, businessErr)
		return
	}
	httpserver.WriteJSON(w, status, body)
}

func (h *Handler) resolveItems(ctx context.Context, s selection.Session, requests []itemRequest) ([]reservation.ItemInput, error) {
	if len(requests) == 0 {
		return nil, apierror.New(apierror.CodeValidation, "items must contain at least one offer")
	}
	items := make([]reservation.ItemInput, 0, len(requests))
	for _, item := range requests {
		if item.Quantity <= 0 {
			return nil, apierror.New(apierror.CodeValidation, "quantity must be positive")
		}
		offer, err := h.availability.ResolvePartnerOffer(ctx, s.PartnerID, s.EventID, strings.TrimSpace(item.OfferID))
		if err != nil {
			return nil, err
		}
		items = append(items, reservation.ItemInput{OfferID: item.OfferID, InventoryKind: offer.InventoryKind, InventoryID: offer.InventoryID, Quantity: item.Quantity, SourceKind: string(offer.SourceKind), SourceAllocationID: offer.SourceAllocationID})
	}
	return items, nil
}

func (h *Handler) loadItemInputs(ctx context.Context, s selection.Session, id uuid.UUID) ([]reservation.ItemInput, uuid.UUID, error) {
	var eventID uuid.UUID
	err := queryer(ctx, h.db).QueryRow(ctx, `SELECT event_id FROM reservations WHERE id=$1 AND partner_id=$2 AND buyer_selection_session_id=$3`, id, s.PartnerID, s.ID).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, uuid.Nil, apierror.New(apierror.CodeResourceNotFound, "Reservation not found")
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	rows, err := query(ctx, h.db, `SELECT ri.id,ri.inventory_kind,ri.reserved_inventory_unit_id,ri.ga_pool_id,ri.quantity,ri.source_kind,COALESCE(aru.allocation_id,gab.allocation_id) FROM reservation_items ri LEFT JOIN allocation_reserved_units aru ON aru.id=ri.source_allocation_reserved_unit_id LEFT JOIN ga_allocation_buckets gab ON gab.id=ri.source_ga_allocation_bucket_id WHERE ri.reservation_id=$1 AND ri.removed_at IS NULL ORDER BY ri.created_at,ri.id`, id)
	if err != nil {
		return nil, uuid.Nil, err
	}
	defer rows.Close()
	items := []reservation.ItemInput{}
	for rows.Next() {
		var itemID uuid.UUID
		var kind string
		var reservedID, gaID, allocationID *uuid.UUID
		var qty int
		var source string
		if err = rows.Scan(&itemID, &kind, &reservedID, &gaID, &qty, &source, &allocationID); err != nil {
			return nil, uuid.Nil, err
		}
		inventoryID := gaID
		if kind == reservation.InventoryReserved {
			inventoryID = reservedID
		}
		if inventoryID == nil {
			return nil, uuid.Nil, apierror.New(apierror.CodeInternal, "Reservation item inventory is missing")
		}
		copyID := itemID
		items = append(items, reservation.ItemInput{ReservationItemID: &copyID, InventoryKind: kind, InventoryID: *inventoryID, Quantity: qty, SourceKind: source, SourceAllocationID: allocationID})
	}
	return items, eventID, rows.Err()
}

func (h *Handler) loadReservation(ctx context.Context, s selection.Session, id uuid.UUID) (map[string]any, error) {
	q := queryer(ctx, h.db)
	var eventID uuid.UUID
	var state, currency string
	var hold, max time.Time
	if err := q.QueryRow(ctx, `SELECT event_id,state,currency,hold_expires_at,max_lifetime_at FROM reservations WHERE id=$1 AND partner_id=$2 AND buyer_selection_session_id=$3`, id, s.PartnerID, s.ID).Scan(&eventID, &state, &currency, &hold, &max); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierror.New(apierror.CodeResourceNotFound, "Reservation not found")
		}
		return nil, err
	}
	rows, err := query(ctx, h.db, `SELECT id,inventory_kind,reserved_inventory_unit_id,ga_pool_id,quantity,unit_amount_minor,currency FROM reservation_items WHERE reservation_id=$1 AND removed_at IS NULL ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	var total int64
	for rows.Next() {
		var itemID uuid.UUID
		var kind string
		var rid, gid *uuid.UUID
		var qty int
		var amount int64
		var itemCurrency string
		if err = rows.Scan(&itemID, &kind, &rid, &gid, &qty, &amount, &itemCurrency); err != nil {
			return nil, err
		}
		inventoryID := ""
		if rid != nil {
			inventoryID = publicid.Encode(publicid.ReservedInventory, *rid)
		} else if gid != nil {
			inventoryID = publicid.Encode(publicid.GAPool, *gid)
		}
		items = append(items, map[string]any{"id": publicid.Encode(publicid.ReservationItem, itemID), "inventory_kind": kind, "inventory_id": inventoryID, "quantity": qty, "unit_amount_minor": amount, "currency": itemCurrency})
		total += amount * int64(qty)
	}
	return map[string]any{"id": publicid.Encode(publicid.Reservation, id), "event_id": publicid.Encode(publicid.Event, eventID), "status": state, "currency": currency, "hold_expires_at": hold, "max_lifetime_at": max, "server_time": serverTime(ctx, h.db), "items": items, "total": map[string]any{"amount_minor": total, "currency": currency}, "return_url": s.ReturnURL}, rows.Err()
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func queryer(ctx context.Context, db *pgxpool.Pool) queryRower {
	if tx, ok := database.TransactionFromContext(ctx); ok {
		return tx
	}
	return db
}
func query(ctx context.Context, db *pgxpool.Pool, sql string, args ...any) (pgx.Rows, error) {
	if tx, ok := database.TransactionFromContext(ctx); ok {
		return tx.Query(ctx, sql, args...)
	}
	return db.Query(ctx, sql, args...)
}
func decode(r *http.Request, target any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte(`{}`)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(target); err != nil {
		return apierror.New(apierror.CodeValidation, "request body is invalid")
	}
	return nil
}
func normalizeItems(items []itemRequest) {
	for i := range items {
		items[i].OfferID = strings.TrimSpace(items[i].OfferID)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].OfferID < items[j].OfferID })
}
func parseReservation(raw string) (uuid.UUID, error) {
	id, err := publicid.Parse(strings.TrimSpace(raw), publicid.Reservation)
	if err != nil {
		return uuid.Nil, apierror.New(apierror.CodeValidation, "reservation_id is invalid")
	}
	return id, nil
}
func reservationToken(r *http.Request) (string, error) {
	token := strings.TrimSpace(r.Header.Get("X-TktSync-Reservation-Token"))
	if token == "" {
		return "", apierror.New(apierror.CodeHoldNotOwned, "X-TktSync-Reservation-Token header is required")
	}
	return token, nil
}
func reservationTokenFingerprint(token string) string {
	return string(idempotency.Fingerprint([]byte(token)))
}
func serverTime(ctx context.Context, db *pgxpool.Pool) time.Time {
	var value time.Time
	_ = queryer(ctx, db).QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&value)
	return value
}
func cloneMap(source map[string]any) map[string]any {
	result := map[string]any{}
	for k, v := range source {
		result[k] = v
	}
	return result
}
