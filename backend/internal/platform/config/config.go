package config

import (
	"errors"
	"fmt"
	"net/url"
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
	TicketQRPublicBaseURL      string
	BrowserOrigins             []string
	PartnerCredentialReplayKey string
	Shutdown                   time.Duration
	Telemetry                  Telemetry
}

type HTTP struct {
	Host               string
	Port               int
	ReadHeaderTimeout  time.Duration
	IdleTimeout        time.Duration
	RequestTimeout     time.Duration
	LongRequestTimeout time.Duration
	MaxBodyBytes       int64
	MaxHeaderBytes     int
	MaxInFlight        int
	MetricsEnabled     bool
	MetricsToken       string
}

type Database struct {
	URL              string
	TxMaxAttempts    int
	TxRetryBase      time.Duration
	MaxConnections   int32
	MinConnections   int32
	MaxLifetime      time.Duration
	MaxIdleLifetime  time.Duration
	ConnectTimeout   time.Duration
	StatementTimeout time.Duration
	LockTimeout      time.Duration
}

type Logging struct {
	Level  string
	Format string
}

type Supabase struct {
	URL               string
	AnonKey           string
	SecretKey         string
	InviteRedirectURL string
	JWTIssuer         string
	JWKSURL           string
	JWTAudience       string
	JWTAlgorithms     []string
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
	PollInterval           time.Duration
	ShutdownTimeout        time.Duration
	ReservationConcurrency int
	OutboxConcurrency      int
	WebhookConcurrency     int
	ReservationBatchSize   int
	OutboxBatchSize        int
	WebhookBatchSize       int
	WebhookTimeout         time.Duration
}

type Telemetry struct {
	Enabled      bool
	OTLPEndpoint string
	SampleRatio  float64
}

type Realtime struct {
	Enabled        bool
	ChannelPrefix  string
	MaxConnections int
}

type Webhook struct {
	Enabled              bool
	EncryptionKeyVersion int
	EncryptionKey        string
	EncryptionKeyring    string
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

	positiveDuration := func(key, fallback string) (time.Duration, error) {
		value, parseErr := time.ParseDuration(get(key, fallback))
		if parseErr != nil || value <= 0 {
			return 0, fmt.Errorf("%s must be a positive duration", key)
		}
		return value, nil
	}
	positiveInt := func(key, fallback string, maximum int) (int, error) {
		value, parseErr := strconv.Atoi(get(key, fallback))
		if parseErr != nil || value <= 0 || value > maximum {
			return 0, fmt.Errorf("%s must be between 1 and %d", key, maximum)
		}
		return value, nil
	}

	readHeaderTimeout, err := positiveDuration("HTTP_READ_HEADER_TIMEOUT", "5s")
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := positiveDuration("HTTP_IDLE_TIMEOUT", "60s")
	if err != nil {
		return Config{}, err
	}
	requestTimeout, err := positiveDuration("HTTP_REQUEST_TIMEOUT", "15s")
	if err != nil {
		return Config{}, err
	}
	longRequestTimeout, err := positiveDuration("HTTP_LONG_REQUEST_TIMEOUT", "60s")
	if err != nil {
		return Config{}, err
	}
	maxBodyBytes, err := strconv.ParseInt(get("HTTP_MAX_BODY_BYTES", "1048576"), 10, 64)
	if err != nil || maxBodyBytes < 1024 || maxBodyBytes > 64<<20 {
		return Config{}, errors.New("HTTP_MAX_BODY_BYTES must be between 1024 and 67108864")
	}
	maxHeaderBytes, err := positiveInt("HTTP_MAX_HEADER_BYTES", "1048576", 16<<20)
	if err != nil {
		return Config{}, err
	}
	maxInFlight, err := positiveInt("HTTP_MAX_IN_FLIGHT", "200", 100000)
	if err != nil {
		return Config{}, err
	}
	metricsEnabled, err := strconv.ParseBool(get("METRICS_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("METRICS_ENABLED must be true or false")
	}

	maxConnections, err := positiveInt("DB_MAX_CONNECTIONS", "20", 10000)
	if err != nil {
		return Config{}, err
	}
	minConnectionsRaw, err := strconv.Atoi(get("DB_MIN_CONNECTIONS", "2"))
	if err != nil || minConnectionsRaw < 0 || minConnectionsRaw > maxConnections {
		return Config{}, errors.New("DB_MIN_CONNECTIONS must be between 0 and DB_MAX_CONNECTIONS")
	}
	maxLifetime, err := positiveDuration("DB_MAX_CONNECTION_LIFETIME", "30m")
	if err != nil {
		return Config{}, err
	}
	maxIdleLifetime, err := positiveDuration("DB_MAX_CONNECTION_IDLE_TIME", "5m")
	if err != nil {
		return Config{}, err
	}
	connectTimeout, err := positiveDuration("DB_CONNECT_TIMEOUT", "5s")
	if err != nil {
		return Config{}, err
	}
	statementTimeout, err := positiveDuration("DB_STATEMENT_TIMEOUT", "65s")
	if err != nil {
		return Config{}, err
	}
	lockTimeout, err := positiveDuration("DB_LOCK_TIMEOUT", "3s")
	if err != nil {
		return Config{}, err
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
	reservationConcurrency, err := positiveInt("WORKER_RESERVATION_CONCURRENCY", "2", 256)
	if err != nil {
		return Config{}, err
	}
	outboxConcurrency, err := positiveInt("WORKER_OUTBOX_CONCURRENCY", "2", 256)
	if err != nil {
		return Config{}, err
	}
	webhookConcurrency, err := positiveInt("WORKER_WEBHOOK_CONCURRENCY", "4", 256)
	if err != nil {
		return Config{}, err
	}
	reservationBatch, err := positiveInt("WORKER_RESERVATION_BATCH_SIZE", "100", 10000)
	if err != nil {
		return Config{}, err
	}
	outboxBatch, err := positiveInt("WORKER_OUTBOX_BATCH_SIZE", "100", 10000)
	if err != nil {
		return Config{}, err
	}
	webhookBatch, err := positiveInt("WORKER_WEBHOOK_BATCH_SIZE", "50", 10000)
	if err != nil {
		return Config{}, err
	}
	webhookTimeout, err := positiveDuration("WORKER_WEBHOOK_TIMEOUT", "5s")
	if err != nil {
		return Config{}, err
	}

	shutdown, err := time.ParseDuration(get("SHUTDOWN_TIMEOUT", "10s"))
	if err != nil || shutdown <= 0 {
		return Config{}, errors.New("SHUTDOWN_TIMEOUT must be a positive duration")
	}

	realtimeEnabled, err := strconv.ParseBool(get("REALTIME_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("REALTIME_ENABLED must be true or false")
	}
	realtimeMaxConnections, err := positiveInt("REALTIME_MAX_CONNECTIONS", "1000", 100000)
	if err != nil {
		return Config{}, err
	}

	webhookEnabled, err := strconv.ParseBool(get("WEBHOOK_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("WEBHOOK_ENABLED must be true or false")
	}
	telemetryEnabled, err := strconv.ParseBool(get("OTEL_ENABLED", "false"))
	if err != nil {
		return Config{}, errors.New("OTEL_ENABLED must be true or false")
	}
	sampleRatio, err := strconv.ParseFloat(get("OTEL_TRACE_SAMPLE_RATIO", "0.1"), 64)
	if err != nil || sampleRatio < 0 || sampleRatio > 1 {
		return Config{}, errors.New("OTEL_TRACE_SAMPLE_RATIO must be between 0 and 1")
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
		Environment:                strings.ToLower(strings.TrimSpace(get("APP_ENV", "development"))),
		PartnerCredentialReplayKey: get("PARTNER_CREDENTIAL_REPLAY_KEY", ""),
		HTTP: HTTP{
			Host: get("API_HOST", "127.0.0.1"), Port: port,
			ReadHeaderTimeout: readHeaderTimeout, IdleTimeout: idleTimeout,
			RequestTimeout: requestTimeout, LongRequestTimeout: longRequestTimeout, MaxBodyBytes: maxBodyBytes,
			MaxHeaderBytes: maxHeaderBytes, MaxInFlight: maxInFlight,
			MetricsEnabled: metricsEnabled, MetricsToken: get("METRICS_BEARER_TOKEN", ""),
		},
		Database: Database{
			URL:            get("DATABASE_URL", ""),
			TxMaxAttempts:  txMaxAttempts,
			TxRetryBase:    txRetryBase,
			MaxConnections: int32(maxConnections), MinConnections: int32(minConnectionsRaw),
			MaxLifetime: maxLifetime, MaxIdleLifetime: maxIdleLifetime,
			ConnectTimeout: connectTimeout, StatementTimeout: statementTimeout, LockTimeout: lockTimeout,
		},
		Logging: Logging{
			Level:  strings.ToLower(get("LOG_LEVEL", "info")),
			Format: strings.ToLower(get("LOG_FORMAT", "json")),
		},
		Supabase: Supabase{
			URL:               get("SUPABASE_URL", ""),
			AnonKey:           get("SUPABASE_ANON_KEY", ""),
			SecretKey:         get("SUPABASE_SECRET_KEY", ""),
			InviteRedirectURL: get("ADMIN_INVITE_REDIRECT_URL", "http://localhost:54470/set-password"),
			JWTIssuer:         get("SUPABASE_JWT_ISSUER", ""),
			JWKSURL:           get("SUPABASE_JWKS_URL", ""),
			JWTAudience:       get("SUPABASE_JWT_AUDIENCE", ""),
			JWTAlgorithms:     splitCSV(get("SUPABASE_JWT_ALGORITHMS", "ES256,RS256")),
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
			PollInterval:           poll,
			ShutdownTimeout:        workerShutdown,
			ReservationConcurrency: reservationConcurrency,
			OutboxConcurrency:      outboxConcurrency,
			WebhookConcurrency:     webhookConcurrency,
			ReservationBatchSize:   reservationBatch,
			OutboxBatchSize:        outboxBatch,
			WebhookBatchSize:       webhookBatch,
			WebhookTimeout:         webhookTimeout,
		},
		Realtime: Realtime{
			Enabled:        realtimeEnabled,
			ChannelPrefix:  get("REALTIME_CHANNEL_PREFIX", "tktsync"),
			MaxConnections: realtimeMaxConnections,
		},
		Webhook: Webhook{
			Enabled:              webhookEnabled,
			EncryptionKeyVersion: webhookEncryptionVersion,
			EncryptionKey:        get("WEBHOOK_ENCRYPTION_KEY", ""),
			EncryptionKeyring:    get("WEBHOOK_ENCRYPTION_KEYRING", ""),
		},
		SelectorBaseURL:       get("SELECTOR_BASE_URL", "http://localhost:5174/s"),
		TicketQRPublicBaseURL: get("TICKET_QR_PUBLIC_BASE_URL", "http://localhost:8080"),
		BrowserOrigins:        splitCSV(get("BROWSER_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:5174,http://localhost:5175")),
		Shutdown:              shutdown,
		Telemetry:             Telemetry{Enabled: telemetryEnabled, OTLPEndpoint: strings.TrimSpace(get("OTEL_EXPORTER_OTLP_ENDPOINT", "")), SampleRatio: sampleRatio},
	}

	if cfg.Database.URL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.HTTP.MetricsEnabled && strings.TrimSpace(cfg.HTTP.MetricsToken) == "" {
		return Config{}, errors.New("METRICS_BEARER_TOKEN is required when METRICS_ENABLED=true")
	}
	if cfg.Telemetry.Enabled && cfg.Telemetry.OTLPEndpoint == "" {
		return Config{}, errors.New("OTEL_EXPORTER_OTLP_ENDPOINT is required when OTEL_ENABLED=true")
	}
	if cfg.HTTP.RequestTimeout >= cfg.Database.StatementTimeout {
		return Config{}, errors.New("HTTP_REQUEST_TIMEOUT must be less than DB_STATEMENT_TIMEOUT")
	}
	if cfg.HTTP.LongRequestTimeout >= cfg.Database.StatementTimeout {
		return Config{}, errors.New("HTTP_LONG_REQUEST_TIMEOUT must be less than DB_STATEMENT_TIMEOUT")
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

	ticketQRBaseURL, err := normalizePublicBaseURL(
		cfg.TicketQRPublicBaseURL,
	)
	if err != nil {
		return Config{}, fmt.Errorf("TICKET_QR_PUBLIC_BASE_URL: %w", err)
	}
	cfg.TicketQRPublicBaseURL = ticketQRBaseURL

	switch cfg.Environment {
	case "development", "test":
	case "production":
		if err := validateProduction(cfg); err != nil {
			return Config{}, err
		}
	default:
		return Config{}, errors.New("APP_ENV must be development, test, or production")
	}

	return cfg, nil
}

func validateProduction(cfg Config) error {
	required := []struct {
		name  string
		value string
	}{
		{"SUPABASE_JWKS_URL", cfg.Supabase.JWKSURL},
		{"SUPABASE_JWT_ISSUER", cfg.Supabase.JWTIssuer},
		{"SUPABASE_JWT_AUDIENCE", cfg.Supabase.JWTAudience},
		{"SELECTION_KEYRING_KEYS", cfg.Keyrings.Selection.Keys},
		{"RESERVATION_KEYRING_KEYS", cfg.Keyrings.Reservation.Keys},
		{"QR_KEYRING_KEYS", cfg.Keyrings.QR.Keys},
		{"PARTNER_CREDENTIAL_REPLAY_KEY", cfg.PartnerCredentialReplayKey},
	}
	for _, item := range required {
		if strings.TrimSpace(item.value) == "" {
			return fmt.Errorf("%s is required in production", item.name)
		}
		if obviousPlaceholder(item.value) {
			return fmt.Errorf("%s must not use a placeholder value in production", item.name)
		}
	}
	if cfg.Keyrings.Selection.ActiveVersion <= 0 || cfg.Keyrings.Reservation.ActiveVersion <= 0 || cfg.Keyrings.QR.ActiveVersion <= 0 {
		return errors.New("all HMAC keyring active versions are required in production")
	}
	for name, raw := range map[string]string{
		"SUPABASE_JWKS_URL":         cfg.Supabase.JWKSURL,
		"SUPABASE_JWT_ISSUER":       cfg.Supabase.JWTIssuer,
		"SELECTOR_BASE_URL":         cfg.SelectorBaseURL,
		"TICKET_QR_PUBLIC_BASE_URL": cfg.TicketQRPublicBaseURL,
	} {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "https://") {
			return fmt.Errorf("%s must use HTTPS in production", name)
		}
		if obviousPlaceholder(raw) {
			return fmt.Errorf("%s must not use a placeholder value in production", name)
		}
	}
	for _, origin := range cfg.BrowserOrigins {
		if !strings.HasPrefix(strings.ToLower(origin), "https://") {
			return errors.New("BROWSER_ALLOWED_ORIGINS must contain only HTTPS origins in production")
		}
	}
	if cfg.Webhook.Enabled && (cfg.Webhook.EncryptionKeyVersion <= 0 || (strings.TrimSpace(cfg.Webhook.EncryptionKey) == "" && strings.TrimSpace(cfg.Webhook.EncryptionKeyring) == "")) {
		return errors.New("webhook encryption key and active version are required when WEBHOOK_ENABLED=true in production")
	}
	for _, algorithm := range cfg.Supabase.JWTAlgorithms {
		if algorithm != "ES256" && algorithm != "RS256" {
			return fmt.Errorf("SUPABASE_JWT_ALGORITHMS contains unsupported production algorithm %q", algorithm)
		}
	}
	return nil
}

func normalizePublicBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("must not contain credentials, a query string, or a fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func obviousPlaceholder(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "example", "example-anon-key", "placeholder", "changeme", "change-me", "keys", "replay-key":
		return true
	}
	return strings.Contains(value, "example.com") || strings.Contains(value, "example.supabase.co")
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
