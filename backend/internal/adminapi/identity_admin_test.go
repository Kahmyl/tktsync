package adminapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestSupabaseIdentityAdminInvitesNewUser(t *testing.T) {
	userID := uuid.New()
	var inviteCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "server-secret" || r.Header.Get("Authorization") != "Bearer server-secret" {
			t.Fatal("Supabase administrative authorization headers are missing")
		}
		switch r.URL.Path {
		case "/auth/v1/admin/users":
			_ = json.NewEncoder(w).Encode(map[string]any{"users": []any{}})
		case "/auth/v1/invite":
			inviteCalls++
			if got := r.URL.Query().Get("redirect_to"); got != "https://admin.example.com" {
				t.Fatalf("redirect_to = %q", got)
			}
			var body struct {
				Email string         `json:"email"`
				Data  map[string]any `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Email != "ada@example.com" || body.Data["display_name"] != "Ada Okafor" || body.Data["tktsync_password_setup_required"] != true {
				t.Fatalf("unexpected invite body: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": userID, "email": body.Email})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	admin, err := NewSupabaseIdentityAdmin(server.URL, "server-secret", "https://admin.example.com")
	if err != nil {
		t.Fatal(err)
	}
	user, invited, err := admin.EnsureInvited(context.Background(), "ada@example.com", "Ada Okafor")
	if err != nil {
		t.Fatal(err)
	}
	if !invited || inviteCalls != 1 || user.ID != userID || user.Email != "ada@example.com" {
		t.Fatalf("unexpected result: %#v invited=%v calls=%d", user, invited, inviteCalls)
	}
}

func TestSupabaseIdentityAdminUsesExistingUserWithoutInviting(t *testing.T) {
	userID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/admin/users" {
			t.Fatalf("unexpected identity request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"users": []map[string]any{{"id": userID, "email": "existing@example.com"}}})
	}))
	defer server.Close()

	admin, err := NewSupabaseIdentityAdmin(server.URL, "server-secret", "")
	if err != nil {
		t.Fatal(err)
	}
	user, invited, err := admin.EnsureInvited(context.Background(), "existing@example.com", "Existing User")
	if err != nil {
		t.Fatal(err)
	}
	if invited || user.ID != userID {
		t.Fatalf("unexpected result: %#v invited=%v", user, invited)
	}
}

func TestSupabaseIdentityAdminRequiresServerConfiguration(t *testing.T) {
	if _, err := NewSupabaseIdentityAdmin("", "server-secret", ""); err == nil {
		t.Fatal("expected incomplete identity administration configuration to fail")
	}
	admin, err := NewSupabaseIdentityAdmin("https://example.supabase.co", "", "")
	if err != nil || admin != nil {
		t.Fatalf("empty optional development configuration = %#v, %v", admin, err)
	}
}

func TestProductionIdentityAdminRequiresHTTPSAndSecret(t *testing.T) {
	if err := ValidateProductionIdentityAdmin("https://project.supabase.co", "server-secret", "https://admin.example.com"); err != nil {
		t.Fatalf("valid production configuration rejected: %v", err)
	}
	for _, values := range [][3]string{
		{"https://project.supabase.co", "", "https://admin.example.com"},
		{"http://project.supabase.co", "server-secret", "https://admin.example.com"},
		{"https://project.supabase.co", "server-secret", "http://admin.example.com"},
	} {
		if err := ValidateProductionIdentityAdmin(values[0], values[1], values[2]); err == nil {
			t.Fatalf("invalid production configuration accepted: %#v", values)
		}
	}
}
