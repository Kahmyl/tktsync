//go:build integration

package adminapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/adminapi"
	allocsvc "github.com/tktsync/tktsync/backend/internal/allocation"
	"github.com/tktsync/tktsync/backend/internal/auth"
	eventsvc "github.com/tktsync/tktsync/backend/internal/event"
	partnersvc "github.com/tktsync/tktsync/backend/internal/partner"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/httpserver"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	venuesvc "github.com/tktsync/tktsync/backend/internal/venue"
)

func TestAdminHTTPIdempotencyAndCredentialReplay(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	pool, err := pgxpool.New(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	runner := database.NewRunner(
		pool,
		3,
		5*time.Millisecond,
	)

	userID := uuid.New()
	subject := uuid.NewString()
	managedAdminSubject := uuid.NewString()

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO app_users (
				id,
				auth_provider,
				auth_subject,
				display_name,
				state,
				created_at,
				updated_at
			)
			VALUES (
				$1,
				'configuration-http-test',
				$2,
				'Configuration HTTP Admin',
				'ACTIVE',
				clock_timestamp(),
				clock_timestamp()
			)
		`,
		userID,
		subject,
	); err != nil {
		t.Fatalf("create test user: %v", err)
	}

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO platform_user_roles (
				user_id,
				role,
				created_at
			)
			VALUES (
				$1,
				'PLATFORM_ADMIN',
				clock_timestamp()
			)
		`,
		userID,
	); err != nil {
		t.Fatalf("create platform role: %v", err)
	}

	protector, err := adminapi.NewReplayProtector(
		bytes.Repeat(
			[]byte{7},
			32,
		),
	)
	if err != nil {
		t.Fatalf("create replay protector: %v", err)
	}

	adminHandler, err := adminapi.New(
		adminapi.Dependencies{
			Database:     pool,
			Transactions: runner,
			HumanAuth: func(
				_ context.Context,
				token string,
			) (auth.HumanPrincipal, error) {
				if token == "managed-admin-token" {
					return auth.HumanPrincipal{Provider: "supabase", Subject: managedAdminSubject}, nil
				}
				if token != "integration-token" {
					return auth.HumanPrincipal{}, io.EOF
				}

				return auth.HumanPrincipal{
					Provider: "configuration-http-test",
					Subject:  subject,
				}, nil
			},
			VenueService:      venuesvc.NewService(runner),
			EventService:      eventsvc.NewService(runner),
			PartnerService:    partnersvc.NewService(runner),
			AllocationService: allocsvc.NewService(runner),
			ReplayProtector:   protector,
			IdentityAdmin: adminapi.IdentityAdminFunc(func(_ context.Context, email, _ string) (adminapi.IdentityUser, bool, error) {
				return adminapi.IdentityUser{ID: uuid.MustParse(managedAdminSubject), Email: email}, true, nil
			}),
		},
	)
	if err != nil {
		t.Fatalf("create admin handler: %v", err)
	}

	handler := httpserver.Handler(
		slog.New(
			slog.NewTextHandler(
				io.Discard,
				nil,
			),
		),
		pool,
		adminHandler,
	)

	venueName := "Configuration HTTP Venue " + uuid.NewString()
	venueKey := uuid.NewString()

	first := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/venues",
		venueKey,
		map[string]any{
			"name": venueName,
		},
	)

	if first.Code != http.StatusCreated {
		t.Fatalf(
			"create Venue status = %d body=%s",
			first.Code,
			first.Body.String(),
		)
	}

	var firstVenue map[string]any
	if err := json.Unmarshal(
		first.Body.Bytes(),
		&firstVenue,
	); err != nil {
		t.Fatalf("decode Venue: %v", err)
	}

	second := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/venues",
		venueKey,
		map[string]any{
			"name": venueName,
		},
	)

	if second.Code != http.StatusCreated {
		t.Fatalf(
			"Venue replay status = %d body=%s",
			second.Code,
			second.Body.String(),
		)
	}

	var secondVenue map[string]any
	if err := json.Unmarshal(
		second.Body.Bytes(),
		&secondVenue,
	); err != nil {
		t.Fatalf("decode Venue replay: %v", err)
	}

	if firstVenue["id"] != secondVenue["id"] {
		t.Fatalf(
			"Venue replay changed identity: %v != %v",
			firstVenue["id"],
			secondVenue["id"],
		)
	}

	var venueCount int
	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM venues
			WHERE name = $1
		`,
		venueName,
	).Scan(&venueCount); err != nil {
		t.Fatalf("count Venue rows: %v", err)
	}

	if venueCount != 1 {
		t.Fatalf(
			"Venue count = %d, want 1",
			venueCount,
		)
	}

	conflict := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/venues",
		venueKey,
		map[string]any{
			"name": venueName + " changed",
		},
	)

	if conflict.Code != http.StatusConflict {
		t.Fatalf(
			"idempotency conflict status = %d body=%s",
			conflict.Code,
			conflict.Body.String(),
		)
	}

	if !strings.Contains(
		conflict.Body.String(),
		"IDEMPOTENCY_CONFLICT",
	) {
		t.Fatalf(
			"missing IDEMPOTENCY_CONFLICT: %s",
			conflict.Body.String(),
		)
	}

	partnerKey := uuid.NewString()
	partnerName := "Configuration HTTP Partner " + uuid.NewString()

	partnerResponse := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/partners",
		partnerKey,
		map[string]any{
			"name": partnerName,
		},
	)

	if partnerResponse.Code != http.StatusCreated {
		t.Fatalf(
			"create Partner status = %d body=%s",
			partnerResponse.Code,
			partnerResponse.Body.String(),
		)
	}

	var partnerBody struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(
		partnerResponse.Body.Bytes(),
		&partnerBody,
	); err != nil {
		t.Fatalf("decode Partner: %v", err)
	}

	partnerID, err := publicid.Parse(
		partnerBody.ID,
		publicid.Partner,
	)
	if err != nil {
		t.Fatalf("parse Partner ID: %v", err)
	}

	credentialKey := uuid.NewString()

	credentialFirst := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/partners/"+
			partnerBody.ID+
			"/credentials",
		credentialKey,
		struct{}{},
	)

	if credentialFirst.Code != http.StatusCreated {
		t.Fatalf(
			"create credential status = %d body=%s",
			credentialFirst.Code,
			credentialFirst.Body.String(),
		)
	}

	credentialSecond := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/partners/"+
			partnerBody.ID+
			"/credentials",
		credentialKey,
		struct{}{},
	)

	if credentialSecond.Code != http.StatusCreated {
		t.Fatalf(
			"credential replay status = %d body=%s",
			credentialSecond.Code,
			credentialSecond.Body.String(),
		)
	}

	if credentialFirst.Body.String() !=
		credentialSecond.Body.String() {
		t.Fatalf(
			"credential replay changed response:\n%s\n%s",
			credentialFirst.Body.String(),
			credentialSecond.Body.String(),
		)
	}

	if !strings.Contains(
		credentialFirst.Body.String(),
		"tkp_",
	) {
		t.Fatalf(
			"credential response missing opaque secret: %s",
			credentialFirst.Body.String(),
		)
	}

	var credentialCount int
	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM partner_credentials
			WHERE partner_id = $1
		`,
		partnerID,
	).Scan(&credentialCount); err != nil {
		t.Fatalf("count Partner credentials: %v", err)
	}

	if credentialCount != 1 {
		t.Fatalf(
			"credential count = %d, want 1",
			credentialCount,
		)
	}

	var storedPayload string
	if err := pool.QueryRow(
		ctx,
		`
			SELECT result_payload::text
			FROM idempotency_operations
			WHERE scope_kind = 'USER'
			  AND app_user_id = $1
			  AND operation_type =
			      'ADMIN_CREATE_PARTNER_CREDENTIAL'
			  AND idempotency_key = $2
		`,
		userID,
		credentialKey,
	).Scan(&storedPayload); err != nil {
		t.Fatalf(
			"read credential replay payload: %v",
			err,
		)
	}

	if strings.Contains(
		storedPayload,
		"tkp_",
	) {
		t.Fatalf(
			"raw Partner credential leaked into idempotency payload: %s",
			storedPayload,
		)
	}

	eventResponse := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/events",
		uuid.NewString(),
		map[string]any{
			"venue_id": firstVenue["id"],
			"name":     "Configuration HTTP Event " + uuid.NewString(),
		},
	)
	if eventResponse.Code != http.StatusCreated {
		t.Fatalf("create Event status = %d body=%s", eventResponse.Code, eventResponse.Body.String())
	}
	var eventBody struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(eventResponse.Body.Bytes(), &eventBody); err != nil {
		t.Fatalf("decode Event: %v", err)
	}
	eventID, err := publicid.Parse(eventBody.ID, publicid.Event)
	if err != nil {
		t.Fatalf("parse Event ID: %v", err)
	}
	sectionID := uuid.New()
	reservedID := uuid.New()
	gaPoolID := uuid.New()
	blockID := uuid.New()
	allocationID := uuid.New()
	historyTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin historical restriction fixture: %v", err)
	}
	defer historyTx.Rollback(ctx)
	if _, err := historyTx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatalf("isolate historical restriction fixture: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO event_sections (id,event_id,snapshot_object_key,name,sort_order) VALUES ($1,$2,'vip','VIP',0)`, sectionID, eventID); err != nil {
		t.Fatalf("create historical restriction section: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO reserved_inventory_units (id,event_id,event_section_id,snapshot_object_key,row_label,seat_label,display_label) VALUES ($1,$2,$3,'vip-a-10','A','10','VIP A 10')`, reservedID, eventID, sectionID); err != nil {
		t.Fatalf("create historical reserved unit: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO ga_inventory_pools (id,event_id,event_section_id,snapshot_object_key,name,capacity) VALUES ($1,$2,$3,'main-floor-zone','Main Floor',100)`, gaPoolID, eventID, sectionID); err != nil {
		t.Fatalf("create historical GA pool: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO inventory_restrictions (id,event_id,kind,state,purpose,created_by_user_id,released_at) VALUES ($1,$2,'BLOCK','RELEASED','SPONSOR',$3,clock_timestamp()),($4,$2,'ALLOCATION','RELEASED','INTERNAL',$3,clock_timestamp())`, blockID, eventID, userID, allocationID); err != nil {
		t.Fatalf("create historical restrictions: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO blocks (restriction_id) VALUES ($1)`, blockID); err != nil {
		t.Fatalf("create historical block: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO allocations (restriction_id,mode,release_destination_kind) VALUES ($1,'NON_PUBLIC','SHARED')`, allocationID); err != nil {
		t.Fatalf("create historical allocation: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO block_reserved_units (block_id,reserved_inventory_unit_id,released_at) VALUES ($1,$2,clock_timestamp())`, blockID, reservedID); err != nil {
		t.Fatalf("create released block membership: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO allocation_reserved_units (allocation_id,reserved_inventory_unit_id,released_at) VALUES ($1,$2,clock_timestamp())`, allocationID, reservedID); err != nil {
		t.Fatalf("create released allocation membership: %v", err)
	}
	if _, err := historyTx.Exec(ctx, `INSERT INTO ga_block_buckets (block_id,ga_pool_id,assigned_quantity,blocked_quantity,released_quantity) VALUES ($1,$2,10,0,10)`, blockID, gaPoolID); err != nil {
		t.Fatalf("create released GA block bucket: %v", err)
	}
	if err := historyTx.Commit(ctx); err != nil {
		t.Fatalf("commit historical restriction fixture: %v", err)
	}
	restrictionsResponse := perform(t, handler, http.MethodGet, "/api/v1/admin/events/"+eventBody.ID+"/restrictions", "", nil)
	if restrictionsResponse.Code != http.StatusOK {
		t.Fatalf("list restrictions status = %d body=%s", restrictionsResponse.Code, restrictionsResponse.Body.String())
	}
	var restrictionsBody struct {
		Items []struct {
			ID               string   `json:"id"`
			ReservedQuantity int      `json:"reserved_quantity"`
			GAQuantity       int      `json:"ga_quantity"`
			InventoryLabels  []string `json:"inventory_labels"`
		} `json:"items"`
	}
	if err := json.Unmarshal(restrictionsResponse.Body.Bytes(), &restrictionsBody); err != nil {
		t.Fatalf("decode restrictions: %v", err)
	}
	byID := make(map[string]struct {
		ReservedQuantity int
		GAQuantity       int
		InventoryLabels  []string
	})
	for _, item := range restrictionsBody.Items {
		byID[item.ID] = struct {
			ReservedQuantity int
			GAQuantity       int
			InventoryLabels  []string
		}{item.ReservedQuantity, item.GAQuantity, item.InventoryLabels}
	}
	blockHistory := byID[publicid.Encode(publicid.Block, blockID)]
	if blockHistory.ReservedQuantity != 1 || blockHistory.GAQuantity != 10 || strings.Join(blockHistory.InventoryLabels, ",") != "Main Floor,VIP A 10" {
		t.Fatalf("released block history = %+v", blockHistory)
	}
	allocationHistory := byID[publicid.Encode(publicid.Allocation, allocationID)]
	if allocationHistory.ReservedQuantity != 1 || strings.Join(allocationHistory.InventoryLabels, ",") != "VIP A 10" {
		t.Fatalf("released allocation history = %+v", allocationHistory)
	}

	readPaths := []string{
		"/api/v1/admin/dashboard",
		"/api/v1/admin/events?limit=10",
		"/api/v1/admin/events/" + eventBody.ID + "/configuration",
		"/api/v1/admin/partners?limit=10",
		"/api/v1/admin/partners/" + partnerBody.ID,
		"/api/v1/admin/tickets?limit=10",
		"/api/v1/admin/admissions?limit=10",
		"/api/v1/admin/webhook-endpoints?limit=10",
	}
	for _, path := range readPaths {
		response := perform(t, handler, http.MethodGet, path, "", nil)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d body=%s", path, response.Code, response.Body.String())
		}
	}

	partnerRead := perform(t, handler, http.MethodGet, "/api/v1/admin/partners/"+partnerBody.ID, "", nil)
	if strings.Contains(partnerRead.Body.String(), "tkp_") || strings.Contains(partnerRead.Body.String(), "secret_hash") {
		t.Fatalf("partner read leaked credential secret material: %s", partnerRead.Body.String())
	}

	newAdmin := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/users",
		uuid.NewString(),
		map[string]any{
			"email":        "added-admin@example.com",
			"display_name": "Configuration HTTP Added Admin",
		},
	)
	if newAdmin.Code != http.StatusCreated {
		t.Fatalf("create Platform Admin status = %d body=%s", newAdmin.Code, newAdmin.Body.String())
	}
	var newAdminBody struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(newAdmin.Body.Bytes(), &newAdminBody); err != nil {
		t.Fatalf("decode Platform Admin: %v", err)
	}
	if _, err := publicid.Parse(newAdminBody.ID, publicid.User); err != nil {
		t.Fatalf("Platform Admin response has invalid public ID %q: %v", newAdminBody.ID, err)
	}

	adminList := perform(t, handler, http.MethodGet, "/api/v1/admin/users?query=added-admin%40example.com", "", nil)
	if adminList.Code != http.StatusOK || !strings.Contains(adminList.Body.String(), newAdminBody.ID) {
		t.Fatalf("Platform Admin list status = %d body=%s", adminList.Code, adminList.Body.String())
	}

	disabled := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/users/"+newAdminBody.ID+"/disable",
		uuid.NewString(),
		map[string]any{"reason": "Access review test"},
	)
	if disabled.Code != http.StatusOK || !strings.Contains(disabled.Body.String(), "DISABLED") {
		t.Fatalf("disable Platform Admin status = %d body=%s", disabled.Code, disabled.Body.String())
	}
	disabledAdminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	disabledAdminRequest.Header.Set("Authorization", "Bearer managed-admin-token")
	disabledAdminResponse := httptest.NewRecorder()
	handler.ServeHTTP(disabledAdminResponse, disabledAdminRequest)
	if disabledAdminResponse.Code != http.StatusForbidden {
		t.Fatalf("disabled Platform Admin authorization status = %d body=%s", disabledAdminResponse.Code, disabledAdminResponse.Body.String())
	}

	enabled := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/users/"+newAdminBody.ID+"/enable",
		uuid.NewString(),
		map[string]any{"reason": "Access restored test"},
	)
	if enabled.Code != http.StatusOK || !strings.Contains(enabled.Body.String(), "ACTIVE") {
		t.Fatalf("enable Platform Admin status = %d body=%s", enabled.Code, enabled.Body.String())
	}
	enabledAdminRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	enabledAdminRequest.Header.Set("Authorization", "Bearer managed-admin-token")
	enabledAdminResponse := httptest.NewRecorder()
	handler.ServeHTTP(enabledAdminResponse, enabledAdminRequest)
	if enabledAdminResponse.Code != http.StatusOK {
		t.Fatalf("enabled Platform Admin authorization status = %d body=%s", enabledAdminResponse.Code, enabledAdminResponse.Body.String())
	}

	selfDisable := perform(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/users/"+publicid.Encode(publicid.User, userID)+"/disable",
		uuid.NewString(),
		map[string]any{"reason": "Must be rejected"},
	)
	if selfDisable.Code != http.StatusBadRequest {
		t.Fatalf("self-disable status = %d body=%s", selfDisable.Code, selfDisable.Body.String())
	}

	newAdminID, _ := publicid.Parse(newAdminBody.ID, publicid.User)
	var adminAuditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_events
		WHERE entity_type='APP_USER' AND entity_id=$1
		  AND operation IN ('PLATFORM_ADMIN_CREATED','PLATFORM_ADMIN_DISABLED','PLATFORM_ADMIN_ENABLED')
	`, newAdminID).Scan(&adminAuditCount); err != nil {
		t.Fatalf("count Platform Admin audit events: %v", err)
	}
	if adminAuditCount != 3 {
		t.Fatalf("Platform Admin audit event count = %d, want 3", adminAuditCount)
	}

	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorizedRequest)
	if unauthorizedResponse.Code != http.StatusForbidden {
		t.Fatalf("unauthorized dashboard status = %d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}
}

func perform(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	idempotencyKey string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	request := httptest.NewRequest(
		method,
		path,
		bytes.NewReader(raw),
	)

	request.Header.Set(
		"Authorization",
		"Bearer integration-token",
	)

	request.Header.Set(
		"Content-Type",
		"application/json",
	)

	if idempotencyKey != "" {
		request.Header.Set(
			"Idempotency-Key",
			idempotencyKey,
		)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response
}
