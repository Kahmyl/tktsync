//go:build integration

package reservation_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/inventory"
	"github.com/tktsync/tktsync/backend/internal/selection"
	"github.com/tktsync/tktsync/backend/internal/selectionapi"
)

func TestSelectionCapabilityReservationHandoff(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	returnURL := "https://partner.example/checkout/return"
	if _, err := f.pool.Exec(ctx, `UPDATE partners SET metadata=jsonb_set(metadata,'{allowed_return_urls}',$2::jsonb,true) WHERE id=$1`, f.partnerID, `["`+returnURL+`"]`); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 31)
	}
	keys, err := auth.ParseHMACKeyring(1, "1:"+base64.RawURLEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	service := selection.NewService(f.pool, f.runner, keys, "https://select.tktsync.test/s")
	created, err := service.Create(ctx, f.partnerID, f.eventID, "buyer-9", returnURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.SelectionURL, "https://select.tktsync.test/s#sel1.") {
		t.Fatalf("selection URL=%q", created.SelectionURL)
	}
	var persisted int
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM buyer_selection_sessions WHERE id=$1 AND encode(token_hash,'hex') NOT LIKE '%sel1%'`, created.ID).Scan(&persisted); err != nil || persisted != 1 {
		t.Fatalf("capability persistence check=%d err=%v", persisted, err)
	}
	handler, err := selectionapi.New(selectionapi.Dependencies{Database: f.pool, Transactions: f.runner, Selection: service, Reservation: f.reservation, Availability: inventory.NewService(f.pool)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	capability := strings.SplitN(created.SelectionURL, "#", 2)[1]
	request := func(method, path, key string, body any, token string) (int, map[string]any) {
		t.Helper()
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		req, _ := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+capability)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		if token != "" {
			req.Header.Set("X-TktSync-Reservation-Token", token)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		decoded := map[string]any{}
		_ = json.NewDecoder(res.Body).Decode(&decoded)
		return res.StatusCode, decoded
	}
	status, availability := request(http.MethodGet, "/api/v1/selection/availability", "", nil, "")
	if status != 200 {
		t.Fatalf("availability status=%d body=%v", status, availability)
	}
	reserved := availability["reserved_units"].([]any)
	offer := reserved[0].(map[string]any)["offer"].(map[string]any)["offer_id"].(string)
	otherOffer := reserved[1].(map[string]any)["offer"].(map[string]any)["offer_id"].(string)
	status, first := request(http.MethodPost, "/api/v1/selection/reservations", "create-1", map[string]any{"items": []map[string]any{{"offer_id": offer, "quantity": 1}}}, "")
	if status != 201 {
		t.Fatalf("create status=%d body=%v", status, first)
	}
	status, replay := request(http.MethodPost, "/api/v1/selection/reservations", "create-1", map[string]any{"items": []map[string]any{{"offer_id": offer, "quantity": 1}}}, "")
	if status != 201 || replay["id"] != first["id"] || replay["reservation_token"] != first["reservation_token"] {
		t.Fatalf("replay mismatch status=%d body=%v", status, replay)
	}
	status, conflict := request(http.MethodPost, "/api/v1/selection/reservations", "create-1", map[string]any{"items": []map[string]any{{"offer_id": otherOffer, "quantity": 1}}}, "")
	if status != http.StatusConflict {
		t.Fatalf("changed idempotent intent status=%d body=%v", status, conflict)
	}
	reservationID := first["id"].(string)
	token := first["reservation_token"].(string)
	status, forbidden := request(http.MethodPost, "/api/v1/selection/reservations/"+reservationID+"/checkout", "checkout-1", map[string]any{}, token)
	if status != 404 {
		t.Fatalf("selector checkout unexpectedly exposed status=%d body=%v", status, forbidden)
	}
	status, released := request(http.MethodPost, "/api/v1/selection/reservations/"+reservationID+"/release", "release-1", map[string]any{}, token)
	if status != 200 || released["status"] != "RELEASED" {
		t.Fatalf("release status=%d body=%v", status, released)
	}
	badReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/selection/session", nil)
	badReq.Header.Set("Authorization", "Bearer "+capability+"x")
	badRes, e := http.DefaultClient.Do(badReq)
	if e != nil {
		t.Fatal(e)
	}
	badRes.Body.Close()
	if badRes.StatusCode != 401 {
		t.Fatalf("tampered capability status=%d", badRes.StatusCode)
	}
}

func TestSelectionRejectsUnregisteredReturnURL(t *testing.T) {
	f := newFixture(t)
	key := make([]byte, 32)
	keys, _ := auth.ParseHMACKeyring(1, "1:"+base64.RawURLEncoding.EncodeToString(key))
	service := selection.NewService(f.pool, f.runner, keys, "https://select.test/s")
	if _, err := service.Create(context.Background(), f.partnerID, f.eventID, "buyer", "https://evil.example/return"); err == nil {
		t.Fatal("unregistered return URL accepted")
	}
}
