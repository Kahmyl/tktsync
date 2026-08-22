package config

import "testing"

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := load(func(string) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("expected missing DATABASE_URL to fail")
	}
}

func TestLoadUsesTypedValues(t *testing.T) {
	values := map[string]string{
		"DATABASE_URL":            "postgres://example",
		"API_PORT":                "9090",
		"REALTIME_ENABLED":        "true",
		"DB_TX_MAX_ATTEMPTS":      "4",
		"DB_TX_RETRY_BASE_DELAY":  "25ms",
		"SUPABASE_JWT_ALGORITHMS": "ES256",
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
