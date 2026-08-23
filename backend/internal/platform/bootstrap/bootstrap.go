package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/config"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/logging"
)

type Resources struct {
	Config          config.Config
	Logger          *slog.Logger
	Database        *pgxpool.Pool
	Transactions    *database.Runner
	PartnerAuth     *auth.PartnerAuthenticator
	Authorizer      *auth.Authorizer
	HumanAuth       *auth.HumanVerifier
	SelectionKeys   *auth.HMACKeyring
	ReservationKeys *auth.HMACKeyring
	QRKeys          *auth.HMACKeyring
}

func Start(ctx context.Context, service string) (Resources, error) {
	cfg, err := config.Load()
	if err != nil {
		return Resources{}, fmt.Errorf("load configuration: %w", err)
	}

	logger := logging.New(os.Stdout, service, cfg)

	pool, err := database.Open(ctx, database.PoolOptions{
		URL:              cfg.Database.URL,
		ApplicationName:  "tktsync-" + service,
		MaxConnections:   cfg.Database.MaxConnections,
		MinConnections:   cfg.Database.MinConnections,
		MaxLifetime:      cfg.Database.MaxLifetime,
		MaxIdleLifetime:  cfg.Database.MaxIdleLifetime,
		ConnectTimeout:   cfg.Database.ConnectTimeout,
		StatementTimeout: cfg.Database.StatementTimeout,
		LockTimeout:      cfg.Database.LockTimeout,
	})
	if err != nil {
		return Resources{}, err
	}

	closeOnError := func(err error) (Resources, error) {
		pool.Close()
		return Resources{}, err
	}

	humanAuth, err := optionalHumanVerifier(cfg.Supabase)
	if err != nil {
		return closeOnError(fmt.Errorf("configure human authentication: %w", err))
	}

	selectionKeys, err := optionalHMACKeyring(
		"selection",
		cfg.Keyrings.Selection,
	)
	if err != nil {
		return closeOnError(err)
	}

	reservationKeys, err := optionalHMACKeyring(
		"reservation",
		cfg.Keyrings.Reservation,
	)
	if err != nil {
		return closeOnError(err)
	}

	qrKeys, err := optionalHMACKeyring(
		"QR",
		cfg.Keyrings.QR,
	)
	if err != nil {
		return closeOnError(err)
	}

	txRunner := database.NewRunner(
		pool,
		cfg.Database.TxMaxAttempts,
		cfg.Database.TxRetryBase,
	)

	return Resources{
		Config:          cfg,
		Logger:          logger,
		Database:        pool,
		Transactions:    txRunner,
		PartnerAuth:     auth.NewPartnerAuthenticator(pool),
		Authorizer:      auth.NewAuthorizer(pool),
		HumanAuth:       humanAuth,
		SelectionKeys:   selectionKeys,
		ReservationKeys: reservationKeys,
		QRKeys:          qrKeys,
	}, nil
}

func optionalHumanVerifier(
	cfg config.Supabase,
) (*auth.HumanVerifier, error) {
	jwksURL := strings.TrimSpace(cfg.JWKSURL)
	issuer := strings.TrimSpace(cfg.JWTIssuer)

	if jwksURL == "" && issuer == "" {
		return nil, nil
	}

	if jwksURL == "" || issuer == "" {
		return nil, fmt.Errorf(
			"SUPABASE_JWKS_URL and SUPABASE_JWT_ISSUER must be configured together",
		)
	}

	return auth.NewHumanVerifier(
		jwksURL,
		issuer,
		cfg.JWTAudience,
		cfg.JWTAlgorithms,
	)
}

func optionalHMACKeyring(
	name string,
	cfg config.HMACKeyring,
) (*auth.HMACKeyring, error) {
	keys := strings.TrimSpace(cfg.Keys)

	if cfg.ActiveVersion == 0 && keys == "" {
		return nil, nil
	}

	if cfg.ActiveVersion == 0 || keys == "" {
		return nil, fmt.Errorf(
			"%s HMAC keyring active version and keys must be configured together",
			name,
		)
	}

	ring, err := auth.ParseHMACKeyring(
		cfg.ActiveVersion,
		keys,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure %s HMAC keyring: %w",
			name,
			err,
		)
	}

	return ring, nil
}

func (r Resources) Close() {
	if r.Database != nil {
		r.Database.Close()
	}
}
