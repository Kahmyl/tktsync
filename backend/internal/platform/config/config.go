package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment                string
	HTTP                       HTTP
	Database                   Database
	Logging                    Logging
	Supabase                   Supabase
	Keyrings                   Keyrings
	Worker                     Worker
	Realtime                   Realtime
	Webhook                    Webhook
	SelectorBaseURL            string
	BrowserOrigins             []string
	PartnerCredentialReplayKey string
	Shutdown                   time.Duration
}

type HTTP struct {
	Host string
	Port int
}

type Database struct {
	URL           string
	TxMaxAttempts int
	TxRetryBase   time.Duration
}

type Logging struct {
	Level  string
	Format string
}

type Supabase struct {
	URL           string
	AnonKey       string
	JWTIssuer     string
	JWKSURL       string
	JWTAudience   string
	JWTAlgorithms []string
}

type HMACKeyring struct {
	ActiveVersion int
	Keys          string
}

type Keyrings struct {
	Selection   HMACKeyring
	Reservation HMACKeyring
	QR          HMACKeyring
}

type Worker struct {
	PollInterval    time.Duration
	ShutdownTimeout time.Duration
}

type Realtime struct {
	Enabled       bool
	ChannelPrefix string
}

type Webhook struct {
	Enabled              bool
	EncryptionKeyVersion int
	EncryptionKey        string
}

func (h HTTP) Address() string {
	return fmt.Sprintf("%s:%d", h.Host, h.Port)
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}

	port, err := strconv.Atoi(get("API_PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, errors.New("API_PORT must be a valid TCP port")
	}

	txMaxAttempts, err := strconv.Atoi(get("DB_TX_MAX_ATTEMPTS", "3"))
	if err != nil || txMaxAttempts < 1 || txMaxAttempts > 10 {
		return Config{}, errors.New("DB_TX_MAX_ATTEMPTS must be between 1 and 10")
	}

	txRetryBase, err := time.ParseDuration(get("DB_TX_RETRY_BASE_DELAY", "10ms"))
	if err != nil || txRetryBase <= 0 {
		return Config{}, errors.New("DB_TX_RETRY_BASE_DELAY must be a positive duration")
	}

	poll, err := time.ParseDuration(get("WORKER_POLL_INTERVAL", "5s"))
	if err != nil || poll <= 0 {
		return Config{}, errors.New("WORKER_POLL_INTERVAL must be a positive duration")
	}

	workerShutdown, err := time.ParseDuration(get("WORKER_SHUTDOWN_TIMEOUT", "10s"))
	if err != nil || workerShutdown <= 0 {
		return Config{}, errors.New("WORKER_SHUTDOWN_TIMEOUT must be a positive duration")
	}

	shutdown, err := time.ParseDuration(get("SHUTDOWN_TIMEOUT", "10s"))
	if err != nil || shutdown <= 0 {
		return Config{}, errors.New("SHUTDOWN_TIMEOUT must be a positive duration")
	}

	realtimeEnabled, err := strconv.ParseBool(get("REALTIME_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("REALTIME_ENABLED must be true or false")
	}

	webhookEnabled, err := strconv.ParseBool(get("WEBHOOK_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("WEBHOOK_ENABLED must be true or false")
	}

	selectionVersion, err := optionalPositiveInt(get("SELECTION_KEYRING_ACTIVE_VERSION", ""))
	if err != nil {
		return Config{}, fmt.Errorf("SELECTION_KEYRING_ACTIVE_VERSION: %w", err)
	}

	reservationVersion, err := optionalPositiveInt(get("RESERVATION_KEYRING_ACTIVE_VERSION", ""))
	if err != nil {
		return Config{}, fmt.Errorf("RESERVATION_KEYRING_ACTIVE_VERSION: %w", err)
	}

	qrVersion, err := optionalPositiveInt(get("QR_KEYRING_ACTIVE_VERSION", ""))
	if err != nil {
		return Config{}, fmt.Errorf("QR_KEYRING_ACTIVE_VERSION: %w", err)
	}

	webhookEncryptionVersion, err := optionalPositiveInt(get("WEBHOOK_ENCRYPTION_KEY_VERSION", ""))
	if err != nil {
		return Config{}, fmt.Errorf("WEBHOOK_ENCRYPTION_KEY_VERSION: %w", err)
	}

	cfg := Config{
		Environment:                get("APP_ENV", "development"),
		PartnerCredentialReplayKey: get("PARTNER_CREDENTIAL_REPLAY_KEY", ""),
		HTTP: HTTP{
			Host: get("API_HOST", "127.0.0.1"),
			Port: port,
		},
		Database: Database{
			URL:           get("DATABASE_URL", ""),
			TxMaxAttempts: txMaxAttempts,
			TxRetryBase:   txRetryBase,
		},
		Logging: Logging{
			Level:  strings.ToLower(get("LOG_LEVEL", "info")),
			Format: strings.ToLower(get("LOG_FORMAT", "json")),
		},
		Supabase: Supabase{
			URL:           get("SUPABASE_URL", ""),
			AnonKey:       get("SUPABASE_ANON_KEY", ""),
			JWTIssuer:     get("SUPABASE_JWT_ISSUER", ""),
			JWKSURL:       get("SUPABASE_JWKS_URL", ""),
			JWTAudience:   get("SUPABASE_JWT_AUDIENCE", ""),
			JWTAlgorithms: splitCSV(get("SUPABASE_JWT_ALGORITHMS", "ES256,RS256")),
		},
		Keyrings: Keyrings{
			Selection: HMACKeyring{
				ActiveVersion: selectionVersion,
				Keys:          get("SELECTION_KEYRING_KEYS", ""),
			},
			Reservation: HMACKeyring{
				ActiveVersion: reservationVersion,
				Keys:          get("RESERVATION_KEYRING_KEYS", ""),
			},
			QR: HMACKeyring{
				ActiveVersion: qrVersion,
				Keys:          get("QR_KEYRING_KEYS", ""),
			},
		},
		Worker: Worker{
			PollInterval:    poll,
			ShutdownTimeout: workerShutdown,
		},
		Realtime: Realtime{
			Enabled:       realtimeEnabled,
			ChannelPrefix: get("REALTIME_CHANNEL_PREFIX", "tktsync"),
		},
		Webhook: Webhook{
			Enabled:              webhookEnabled,
			EncryptionKeyVersion: webhookEncryptionVersion,
			EncryptionKey:        get("WEBHOOK_ENCRYPTION_KEY", ""),
		},
		SelectorBaseURL: get("SELECTOR_BASE_URL", "http://localhost:5174/s"),
		BrowserOrigins:  splitCSV(get("BROWSER_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:5174,http://localhost:5175")),
		Shutdown:        shutdown,
	}

	if cfg.Database.URL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	if cfg.Logging.Format != "json" && cfg.Logging.Format != "text" {
		return Config{}, errors.New("LOG_FORMAT must be json or text")
	}

	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}

	if len(cfg.Supabase.JWTAlgorithms) == 0 {
		return Config{}, errors.New("SUPABASE_JWT_ALGORITHMS must contain at least one algorithm")
	}

	return cfg, nil
}

func optionalPositiveInt(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, errors.New("must be a positive integer when configured")
	}

	return value, nil
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}

	return out
}
