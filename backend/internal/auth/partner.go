package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

const PartnerCredentialPrefix = "tkp_"

type QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PartnerPrincipal struct {
	PartnerID    uuid.UUID
	CredentialID uuid.UUID
	PartnerState string
}

type GeneratedPartnerCredential struct {
	KeyID      string
	Secret     string
	Encoded    string
	SecretHash []byte
}

type PartnerAuthenticator struct {
	db QueryRower
}

func NewPartnerAuthenticator(db QueryRower) *PartnerAuthenticator {
	return &PartnerAuthenticator{db: db}
}

func GeneratePartnerCredential() (GeneratedPartnerCredential, error) {
	var keyIDBytes [12]byte
	if _, err := rand.Read(keyIDBytes[:]); err != nil {
		return GeneratedPartnerCredential{}, err
	}

	var secretBytes [32]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return GeneratedPartnerCredential{}, err
	}

	keyID := hex.EncodeToString(keyIDBytes[:])
	secret := base64.RawURLEncoding.EncodeToString(secretBytes[:])
	sum := sha256.Sum256(secretBytes[:])

	return GeneratedPartnerCredential{
		KeyID:      keyID,
		Secret:     secret,
		Encoded:    PartnerCredentialPrefix + keyID + "_" + secret,
		SecretHash: append([]byte(nil), sum[:]...),
	}, nil
}

func ParsePartnerCredential(encoded string) (string, string, error) {
	if !strings.HasPrefix(encoded, PartnerCredentialPrefix) {
		return "", "", errors.New("invalid Partner credential prefix")
	}

	remainder := strings.TrimPrefix(encoded, PartnerCredentialPrefix)
	parts := strings.SplitN(remainder, "_", 2)

	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("invalid Partner credential format")
	}

	keyIDBytes, err := hex.DecodeString(parts[0])
	if err != nil || len(keyIDBytes) != 12 {
		return "", "", errors.New("invalid Partner credential key ID")
	}

	secretBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secretBytes) != 32 {
		return "", "", errors.New("invalid Partner credential secret")
	}

	return parts[0], parts[1], nil
}

func partnerSecretHash(secret string) ([]byte, error) {
	secretBytes, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil || len(secretBytes) != 32 {
		return nil, errors.New("invalid Partner credential secret")
	}

	sum := sha256.Sum256(secretBytes)
	return append([]byte(nil), sum[:]...), nil
}

func (a *PartnerAuthenticator) Authenticate(
	ctx context.Context,
	encoded string,
) (PartnerPrincipal, error) {
	keyID, secret, err := ParsePartnerCredential(encoded)
	if err != nil {
		return PartnerPrincipal{}, authenticationFailure()
	}

	presentedHash, err := partnerSecretHash(secret)
	if err != nil {
		return PartnerPrincipal{}, authenticationFailure()
	}

	var (
		credentialID uuid.UUID
		partnerID    uuid.UUID
		storedHash   []byte
		partnerState string
	)

	err = a.db.QueryRow(
		ctx,
		`
			SELECT
				pc.id,
				pc.partner_id,
				pc.secret_hash,
				p.state
			FROM partner_credentials pc
			JOIN partners p ON p.id = pc.partner_id
			WHERE pc.key_id = $1
			  AND pc.state = 'ACTIVE'
		`,
		keyID,
	).Scan(
		&credentialID,
		&partnerID,
		&storedHash,
		&partnerState,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PartnerPrincipal{}, authenticationFailure()
		}
		return PartnerPrincipal{}, err
	}

	if len(storedHash) != len(presentedHash) ||
		subtle.ConstantTimeCompare(storedHash, presentedHash) != 1 {
		return PartnerPrincipal{}, authenticationFailure()
	}

	return PartnerPrincipal{
		PartnerID:    partnerID,
		CredentialID: credentialID,
		PartnerState: partnerState,
	}, nil
}

func authenticationFailure() error {
	return apierror.WithStatus(
		apierror.CodeNotAuthorized,
		"authentication failed",
		401,
	)
}
