package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const maxJWKSBytes = 1 << 20

type signingKey struct {
	key       any
	algorithm string
}

type HumanVerifier struct {
	issuer     string
	audience   string
	jwksURL    string
	allowed    []string
	allowedSet map[string]struct{}
	client     *http.Client
	cacheTTL   time.Duration

	mu        sync.RWMutex
	keys      map[string]signingKey
	fetchedAt time.Time
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func NewHumanVerifier(
	jwksURL string,
	issuer string,
	audience string,
	allowedAlgorithms []string,
) (*HumanVerifier, error) {
	if strings.TrimSpace(jwksURL) == "" {
		return nil, errors.New("JWKS URL is required")
	}

	if strings.TrimSpace(issuer) == "" {
		return nil, errors.New("JWT issuer is required")
	}

	if len(allowedAlgorithms) == 0 {
		return nil, errors.New("at least one JWT algorithm is required")
	}

	allowedSet := make(map[string]struct{}, len(allowedAlgorithms))

	for _, algorithm := range allowedAlgorithms {
		algorithm = strings.TrimSpace(algorithm)
		if algorithm == "" || algorithm == "none" {
			return nil, errors.New("invalid JWT signing algorithm")
		}
		allowedSet[algorithm] = struct{}{}
	}

	return &HumanVerifier{
		jwksURL:    jwksURL,
		issuer:     issuer,
		audience:   audience,
		allowed:    append([]string(nil), allowedAlgorithms...),
		allowedSet: allowedSet,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		cacheTTL: 10 * time.Minute,
		keys:     map[string]signingKey{},
	}, nil
}

func (v *HumanVerifier) Verify(
	ctx context.Context,
	raw string,
) (HumanPrincipal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return HumanPrincipal{}, authenticationFailure()
	}

	unverified, _, err := jwt.NewParser().ParseUnverified(
		raw,
		jwt.MapClaims{},
	)
	if err != nil {
		return HumanPrincipal{}, authenticationFailure()
	}

	algorithm := unverified.Method.Alg()

	if _, ok := v.allowedSet[algorithm]; !ok {
		return HumanPrincipal{}, authenticationFailure()
	}

	kid, _ := unverified.Header["kid"].(string)
	if kid == "" {
		return HumanPrincipal{}, authenticationFailure()
	}

	key, err := v.key(ctx, kid, algorithm)
	if err != nil {
		return HumanPrincipal{}, authenticationFailure()
	}

	options := []jwt.ParserOption{
		jwt.WithValidMethods(v.allowed),
		jwt.WithIssuer(v.issuer),
	}

	if v.audience != "" {
		options = append(options, jwt.WithAudience(v.audience))
	}

	token, err := jwt.Parse(
		raw,
		func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != algorithm {
				return nil, errors.New("JWT algorithm changed during parse")
			}
			return key, nil
		},
		options...,
	)
	if err != nil || !token.Valid {
		return HumanPrincipal{}, authenticationFailure()
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return HumanPrincipal{}, authenticationFailure()
	}

	expiry, err := claims.GetExpirationTime()
	if err != nil || expiry == nil || !time.Now().Before(expiry.Time) {
		return HumanPrincipal{}, authenticationFailure()
	}

	notBefore, err := claims.GetNotBefore()
	if err != nil {
		return HumanPrincipal{}, authenticationFailure()
	}

	if notBefore != nil && time.Now().Before(notBefore.Time) {
		return HumanPrincipal{}, authenticationFailure()
	}

	subject, err := claims.GetSubject()
	if err != nil || subject == "" {
		return HumanPrincipal{}, authenticationFailure()
	}

	return HumanPrincipal{
		Provider: "supabase",
		Subject:  subject,
	}, nil
}

func (v *HumanVerifier) key(
	ctx context.Context,
	kid string,
	algorithm string,
) (any, error) {
	v.mu.RLock()
	cached, ok := v.keys[kid]
	fresh := time.Since(v.fetchedAt) < v.cacheTTL
	v.mu.RUnlock()

	if ok && fresh {
		return validateSigningKey(cached, algorithm)
	}

	if err := v.refresh(ctx); err != nil {
		return nil, err
	}

	v.mu.RLock()
	cached, ok = v.keys[kid]
	v.mu.RUnlock()

	if !ok {
		return nil, errors.New("JWT key ID not found")
	}

	return validateSigningKey(cached, algorithm)
}

func validateSigningKey(value signingKey, algorithm string) (any, error) {
	if value.algorithm != "" && value.algorithm != algorithm {
		return nil, errors.New("JWT algorithm does not match JWK algorithm")
	}

	switch value.key.(type) {
	case *ecdsa.PublicKey:
		if algorithm != "ES256" {
			return nil, errors.New("JWT algorithm incompatible with P-256 key")
		}

	case *rsa.PublicKey:
		if !strings.HasPrefix(algorithm, "RS") &&
			!strings.HasPrefix(algorithm, "PS") {
			return nil, errors.New("JWT algorithm incompatible with RSA key")
		}

	default:
		return nil, errors.New("unsupported JWT verification key")
	}

	return value.key, nil
}

func (v *HumanVerifier) refresh(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		v.jwksURL,
		nil,
	)
	if err != nil {
		return err
	}

	response, err := v.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS returned HTTP %d", response.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(response.Body, maxJWKSBytes+1))
	if err != nil {
		return fmt.Errorf("read JWKS: %w", err)
	}

	if len(raw) > maxJWKSBytes {
		return errors.New("JWKS response exceeds size limit")
	}

	var document jwksDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	keys := map[string]signingKey{}

	for _, item := range document.Keys {
		if item.Kid == "" {
			continue
		}

		if item.Use != "" && item.Use != "sig" {
			continue
		}

		if item.Alg != "" {
			if _, ok := v.allowedSet[item.Alg]; !ok {
				continue
			}
		}

		if _, duplicate := keys[item.Kid]; duplicate {
			return errors.New("JWKS contains duplicate key ID")
		}

		key, err := parseJWK(item)
		if err != nil {
			continue
		}

		keys[item.Kid] = signingKey{
			key:       key,
			algorithm: item.Alg,
		}
	}

	if len(keys) == 0 {
		return errors.New("JWKS contains no usable signing keys")
	}

	v.keys = keys
	v.fetchedAt = time.Now()

	return nil
}

func parseJWK(value jwk) (any, error) {
	switch value.Kty {
	case "EC":
		if value.Crv != "P-256" {
			return nil, fmt.Errorf("unsupported EC curve %q", value.Crv)
		}

		if value.Alg != "" && value.Alg != "ES256" {
			return nil, errors.New("P-256 JWK declares incompatible algorithm")
		}

		xBytes, err := base64.RawURLEncoding.DecodeString(value.X)
		if err != nil {
			return nil, err
		}

		yBytes, err := base64.RawURLEncoding.DecodeString(value.Y)
		if err != nil {
			return nil, err
		}

		if len(xBytes) == 0 || len(yBytes) == 0 ||
			len(xBytes) > 32 || len(yBytes) > 32 {
			return nil, errors.New("invalid P-256 JWK coordinate length")
		}

		x := new(big.Int).SetBytes(xBytes)
		y := new(big.Int).SetBytes(yBytes)
		curve := elliptic.P256()

		if !curve.IsOnCurve(x, y) {
			return nil, errors.New("EC JWK point is not on P-256")
		}

		return &ecdsa.PublicKey{
			Curve: curve,
			X:     x,
			Y:     y,
		}, nil

	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(value.N)
		if err != nil {
			return nil, err
		}

		eBytes, err := base64.RawURLEncoding.DecodeString(value.E)
		if err != nil {
			return nil, err
		}

		if len(nBytes) == 0 {
			return nil, errors.New("empty RSA modulus")
		}

		if len(eBytes) == 0 || len(eBytes) > 4 {
			return nil, errors.New("invalid RSA exponent")
		}

		exponent := 0
		for _, b := range eBytes {
			exponent = exponent<<8 + int(b)
		}

		if exponent < 3 || exponent%2 == 0 {
			return nil, errors.New("invalid RSA exponent")
		}

		return &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: exponent,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported JWK type %q", value.Kty)
	}
}
