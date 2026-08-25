package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := load(func(string) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("expected missing DATABASE_URL to fail")
	}
}

func TestLoadUsesTypedValues(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":              "postgres://example",
		"API_PORT":                  "9090",
		"REALTIME_ENABLED":          "true",
		"DB_TX_MAX_ATTEMPTS":        "4",
		"DB_TX_RETRY_BASE_DELAY":    "25ms",
		"SUPABASE_JWT_ALGORITHMS":   "ES256",
		"DB_MAX_CONNECTIONS":        "32",
		"DB_MIN_CONNECTIONS":        "4",
		"WORKER_OUTBOX_CONCURRENCY": "6",
		"TICKET_QR_PUBLIC_BASE_URL": "https://tickets.acme.test/",
	}

	cfg, err := load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.HTTP.Port != 9090 {
		t.Fatalf("unexpected port: %d", cfg.HTTP.Port)
	}

	if !cfg.Realtime.Enabled {
		t.Fatal("expected realtime enabled")
	}

	if cfg.Database.TxMaxAttempts != 4 {
		t.Fatalf("unexpected transaction attempts: %d", cfg.Database.TxMaxAttempts)
	}

	if cfg.Database.TxRetryBase.String() != "25ms" {
		t.Fatalf("unexpected retry base: %s", cfg.Database.TxRetryBase)
	}

	if len(cfg.Supabase.JWTAlgorithms) != 1 ||
		cfg.Supabase.JWTAlgorithms[0] != "ES256" {
		t.Fatalf("unexpected algorithms: %#v", cfg.Supabase.JWTAlgorithms)
	}
	if cfg.Database.MaxConnections != 32 || cfg.Database.MinConnections != 4 {
		t.Fatalf("unexpected pool sizing: %d/%d", cfg.Database.MinConnections, cfg.Database.MaxConnections)
	}
	if cfg.Worker.OutboxConcurrency != 6 {
		t.Fatalf("unexpected outbox concurrency: %d", cfg.Worker.OutboxConcurrency)
	}
	if cfg.TicketQRPublicBaseURL != "https://tickets.acme.test" {
		t.Fatalf("unexpected Ticket QR base URL: %q", cfg.TicketQRPublicBaseURL)
	}
}

func TestLoadRejectsInvalidTicketQRPublicBaseURL(t *testing.T) {
	for _, value := range []string{
		"",
		"tickets.example.test",
		"ftp://tickets.example.test",
		"https://user:pass@tickets.example.test",
		"https://tickets.example.test?credential=secret",
		"https://tickets.example.test#fragment",
	} {
		values := map[string]string{
			"DATABASE_URL":              "postgres://example",
			"TICKET_QR_PUBLIC_BASE_URL": value,
		}
		_, err := load(func(key string) (string, bool) {
			candidate, ok := values[key]
			return candidate, ok
		})
		if err == nil || !strings.Contains(err.Error(), "TICKET_QR_PUBLIC_BASE_URL") {
			t.Fatalf("base URL %q error=%v", value, err)
		}
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL": "postgres://example",
		"API_PORT":     "70000",
	}

	_, err := load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil {
		t.Fatal("expected invalid API_PORT to fail")
	}
}

func TestLoadRejectsMetricsWithoutBearerToken(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://example", "METRICS_ENABLED": "true"}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil {
		t.Fatal("expected unprotected metrics configuration to fail")
	}
}

func TestLoadRejectsPoolBudgetMismatch(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://example", "DB_MAX_CONNECTIONS": "2", "DB_MIN_CONNECTIONS": "3"}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil {
		t.Fatal("expected invalid pool budget to fail")
	}
}

func TestLoadRejectsInvalidTransactionRetryCount(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":       "postgres://example",
		"DB_TX_MAX_ATTEMPTS": "0",
	}

	_, err := load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil {
		t.Fatal("expected invalid transaction attempt count to fail")
	}
}

func TestLoadRequiresHTTPTimeoutsInsideDatabaseBudget(t *testing.T) {
	for _, values := range []map[string]string{
		{"DATABASE_URL": "postgres://example", "HTTP_REQUEST_TIMEOUT": "30s", "DB_STATEMENT_TIMEOUT": "30s"},
		{"DATABASE_URL": "postgres://example", "HTTP_LONG_REQUEST_TIMEOUT": "61s", "DB_STATEMENT_TIMEOUT": "60s"},
	} {
		_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
		if err == nil || !strings.Contains(err.Error(), "must be less than DB_STATEMENT_TIMEOUT") {
			t.Fatalf("expected timeout budget validation, got %v", err)
		}
	}
}

func TestLoadRejectsIncompleteProductionConfiguration(t *testing.T) {
	values := map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://example", "DB_STATEMENT_TIMEOUT": "65s",
	}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil || !strings.Contains(err.Error(), "SUPABASE_JWKS_URL is required in production") {
		t.Fatalf("expected production auth validation, got %v", err)
	}
}

func TestLoadAcceptsCompleteProductionConfiguration(t *testing.T) {
	values := map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://example", "DB_STATEMENT_TIMEOUT": "65s",
		"SUPABASE_JWKS_URL":   "https://identity.acme.internal/.well-known/jwks.json",
		"SUPABASE_JWT_ISSUER": "https://identity.acme.internal/auth/v1", "SUPABASE_JWT_AUDIENCE": "authenticated",
		"SUPABASE_URL": "https://identity.acme.internal", "SUPABASE_SECRET_KEY": "production-secret-key", "ADMIN_INVITE_REDIRECT_URL": "https://admin.acme.internal",
		"SELECTION_KEYRING_ACTIVE_VERSION": "1", "SELECTION_KEYRING_KEYS": "production-selection-keyring",
		"RESERVATION_KEYRING_ACTIVE_VERSION": "1", "RESERVATION_KEYRING_KEYS": "production-reservation-keyring",
		"QR_KEYRING_ACTIVE_VERSION": "1", "QR_KEYRING_KEYS": "production-qr-keyring",
		"PARTNER_CREDENTIAL_REPLAY_KEY": "production-replay-protection-key",
		"SELECTOR_BASE_URL":             "https://select.acme.internal/s", "BROWSER_ALLOWED_ORIGINS": "https://admin.acme.internal,https://scanner.acme.internal",
		"TICKET_QR_PUBLIC_BASE_URL": "https://tickets.acme.internal",
	}
	cfg, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatalf("complete production config rejected: %v", err)
	}
	if cfg.Environment != "production" {
		t.Fatalf("unexpected environment %q", cfg.Environment)
	}
}

func TestLoadRejectsInsecureProductionBrowserOrigin(t *testing.T) {
	values := map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://example", "DB_STATEMENT_TIMEOUT": "65s",
		"SUPABASE_JWKS_URL": "https://identity.acme.internal/jwks", "SUPABASE_JWT_ISSUER": "https://identity.acme.internal/auth/v1", "SUPABASE_JWT_AUDIENCE": "authenticated",
		"SUPABASE_URL": "https://identity.acme.internal", "SUPABASE_SECRET_KEY": "production-secret-key", "ADMIN_INVITE_REDIRECT_URL": "https://admin.acme.internal",
		"SELECTION_KEYRING_ACTIVE_VERSION": "1", "SELECTION_KEYRING_KEYS": "production-selection-keyring", "RESERVATION_KEYRING_ACTIVE_VERSION": "1", "RESERVATION_KEYRING_KEYS": "production-reservation-keyring", "QR_KEYRING_ACTIVE_VERSION": "1", "QR_KEYRING_KEYS": "production-qr-keyring",
		"PARTNER_CREDENTIAL_REPLAY_KEY": "production-replay-protection-key", "SELECTOR_BASE_URL": "https://select.acme.internal/s", "BROWSER_ALLOWED_ORIGINS": "http://localhost:5173",
		"TICKET_QR_PUBLIC_BASE_URL": "https://tickets.acme.internal",
	}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil || !strings.Contains(err.Error(), "only HTTPS") {
		t.Fatalf("expected insecure origin rejection, got %v", err)
	}
}

func TestLoadRejectsProductionPlaceholder(t *testing.T) {
	values := map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://example", "DB_STATEMENT_TIMEOUT": "65s",
		"SUPABASE_JWKS_URL":   "https://example.supabase.co/auth/v1/.well-known/jwks.json",
		"SUPABASE_JWT_ISSUER": "https://identity.acme.internal/auth/v1", "SUPABASE_JWT_AUDIENCE": "authenticated",
		"SUPABASE_URL": "https://identity.acme.internal", "SUPABASE_SECRET_KEY": "production-secret-key", "ADMIN_INVITE_REDIRECT_URL": "https://admin.acme.internal",
		"SELECTION_KEYRING_ACTIVE_VERSION": "1", "SELECTION_KEYRING_KEYS": "production-selection-keyring", "RESERVATION_KEYRING_ACTIVE_VERSION": "1", "RESERVATION_KEYRING_KEYS": "production-reservation-keyring", "QR_KEYRING_ACTIVE_VERSION": "1", "QR_KEYRING_KEYS": "production-qr-keyring",
		"PARTNER_CREDENTIAL_REPLAY_KEY": "production-replay-protection-key", "SELECTOR_BASE_URL": "https://select.acme.internal/s", "BROWSER_ALLOWED_ORIGINS": "https://admin.acme.internal",
		"TICKET_QR_PUBLIC_BASE_URL": "https://tickets.acme.internal",
	}
	_, err := load(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err == nil || !strings.Contains(err.Error(), "placeholder value") {
		t.Fatalf("expected production placeholder rejection, got %v", err)
	}
}
