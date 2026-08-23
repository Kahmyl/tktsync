//go:build integration

package adminapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/adminapi"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func TestAdminLifecycleAuthorizationReasonAuditAndIdempotency(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	runner := database.NewRunner(pool, 5, 5*time.Millisecond)
	managerID, viewerID := uuid.New(), uuid.New()
	managerSubject, viewerSubject := uuid.NewString(), uuid.NewString()
	for _, user := range []struct {
		id, subject uuid.UUID
		name        string
	}{{managerID, uuid.MustParse(managerSubject), "Release Manager"}, {viewerID, uuid.MustParse(viewerSubject), "Release Viewer"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO app_users(id,auth_provider,auth_subject,display_name,state,created_at,updated_at) VALUES($1,'release-lifecycle',$2,$3,'ACTIVE',clock_timestamp(),clock_timestamp())`, user.id, user.subject.String(), user.name); err != nil {
			t.Fatal(err)
		}
	}
	venueService := venuesvc.NewService(runner)
	eventService := eventsvc.NewService(runner)
	venueID, err := venueService.CreateVenue(ctx, managerID, venuesvc.CreateVenueInput{Name: "Release Lifecycle " + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := eventService.Create(ctx, managerID, eventsvc.CreateInput{VenueID: venueID, Name: "Release Lifecycle Event"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE events SET state='ON_SALE' WHERE id=$1`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO event_staff_assignments(event_id,user_id,role,state,created_at) VALUES($1,$2,'EVENT_MANAGER','ACTIVE',clock_timestamp()),($1,$3,'VIEWER','ACTIVE',clock_timestamp())`, eventID, managerID, viewerID); err != nil {
		t.Fatal(err)
	}
	protector, err := adminapi.NewReplayProtector(bytes.Repeat([]byte{11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := adminapi.New(adminapi.Dependencies{
		Database: pool, Transactions: runner,
		HumanAuth: func(_ context.Context, token string) (auth.HumanPrincipal, error) {
			subject := managerSubject
			if token == "viewer" {
				subject = viewerSubject
			}
			return auth.HumanPrincipal{Provider: "release-lifecycle", Subject: subject}, nil
		},
		VenueService: venueService, EventService: eventService, PartnerService: partnersvc.NewService(runner), ReplayProtector: protector,
	})
	if err != nil {
		t.Fatal(err)
	}
	eventPath := "/api/v1/admin/events/" + publicid.Encode(publicid.Event, eventID)
	call := func(token, path, key string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	viewer := call("viewer", eventPath+"/pause-sales", uuid.NewString(), struct{}{})
	if viewer.Code != http.StatusForbidden {
		t.Fatalf("viewer pause status=%d body=%s", viewer.Code, viewer.Body.String())
	}
	pauseKey := uuid.NewString()
	first := call("manager", eventPath+"/pause-sales", pauseKey, struct{}{})
	replay := call("manager", eventPath+"/pause-sales", pauseKey, struct{}{})
	if first.Code != http.StatusOK || replay.Code != http.StatusOK || !sameJSON(first.Body.Bytes(), replay.Body.Bytes()) {
		t.Fatalf("pause/replay=%d/%d %s %s", first.Code, replay.Code, first.Body.String(), replay.Body.String())
	}
	if resumed := call("manager", eventPath+"/resume-sales", uuid.NewString(), struct{}{}); resumed.Code != http.StatusOK {
		t.Fatalf("resume=%d %s", resumed.Code, resumed.Body.String())
	}
	missingReason := call("manager", eventPath+"/cancel", uuid.NewString(), map[string]any{"reason": "  "})
	if missingReason.Code != http.StatusBadRequest {
		t.Fatalf("missing cancellation reason=%d %s", missingReason.Code, missingReason.Body.String())
	}
	cancelKey := uuid.NewString()
	cancelled := call("manager", eventPath+"/cancel", cancelKey, map[string]any{"reason": "Venue safety closure"})
	cancelReplay := call("manager", eventPath+"/cancel", cancelKey, map[string]any{"reason": "Venue safety closure"})
	if cancelled.Code != http.StatusOK || cancelReplay.Code != http.StatusOK || !sameJSON(cancelled.Body.Bytes(), cancelReplay.Body.Bytes()) {
		t.Fatalf("cancel/replay=%d/%d %s %s", cancelled.Code, cancelReplay.Code, cancelled.Body.String(), cancelReplay.Body.String())
	}
	var state string
	var pauseFacts, cancelFacts int
	var reason string
	if err := pool.QueryRow(ctx, `SELECT e.state,(SELECT COUNT(*) FROM audit_events WHERE event_id=e.id AND operation='EVENT_SALES_PAUSED'),(SELECT COUNT(*) FROM audit_events WHERE event_id=e.id AND operation='EVENT_CANCELLED'),COALESCE((SELECT reason FROM audit_events WHERE event_id=e.id AND operation='EVENT_CANCELLED' ORDER BY occurred_at DESC LIMIT 1),'') FROM events e WHERE e.id=$1`, eventID).Scan(&state, &pauseFacts, &cancelFacts, &reason); err != nil {
		t.Fatal(err)
	}
	if state != "CANCELLED" || pauseFacts != 1 || cancelFacts != 1 || reason != "Venue safety closure" {
		t.Fatalf("state/pause/cancel/reason=%s/%d/%d/%q", state, pauseFacts, cancelFacts, reason)
	}
}

func sameJSON(left, right []byte) bool {
	var leftValue, rightValue map[string]any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftCanonical, _ := json.Marshal(leftValue)
	rightCanonical, _ := json.Marshal(rightValue)
	return bytes.Equal(leftCanonical, rightCanonical)
}
