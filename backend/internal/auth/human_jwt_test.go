package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type mutableJWKS struct {
	mu       sync.RWMutex
	document jwksDocument
	requests atomic.Int32
}

func (m *mutableJWKS) set(document jwksDocument) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.document = document
}

func (m *mutableJWKS) serve(w http.ResponseWriter, _ *http.Request) {
	m.requests.Add(1)

	m.mu.RLock()
	document := m.document
	m.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(document)
}

func TestHumanVerifierValidatesES256JWKS(t *testing.T) {
	privateKey := mustECDSAKey(t)
	document := jwksDocument{
		Keys: []jwk{ecJWK("test-key", privateKey, "sig", "ES256")},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(document)
	}))
	defer server.Close()

	verifier, err := NewHumanVerifier(
		server.URL,
		"https://issuer.example",
		"authenticated",
		[]string{"ES256"},
	)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	signed := signHumanToken(
		t,
		privateKey,
		"test-key",
		"https://issuer.example",
		"authenticated",
		time.Now().Add(5*time.Minute),
		time.Now().Add(-time.Minute),
	)

	principal, err := verifier.Verify(t.Context(), signed)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if principal.Subject != "human-auth-subject" {
		t.Fatalf("unexpected subject %q", principal.Subject)
	}
}

func TestHumanVerifierRejectsWrongAudience(t *testing.T) {
	privateKey := mustECDSAKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksDocument{
			Keys: []jwk{ecJWK("test-key", privateKey, "sig", "ES256")},
		})
	}))
	defer server.Close()

	verifier, err := NewHumanVerifier(
		server.URL,
		"https://issuer.example",
		"authenticated",
		[]string{"ES256"},
	)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	signed := signHumanToken(
		t,
		privateKey,
		"test-key",
		"https://issuer.example",
		"wrong-audience",
		time.Now().Add(5*time.Minute),
		time.Now().Add(-time.Minute),
	)

	if _, err := verifier.Verify(t.Context(), signed); err == nil {
		t.Fatal("expected wrong audience to fail")
	}
}

func TestHumanVerifierRequiresNonExpiredToken(t *testing.T) {
	privateKey := mustECDSAKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksDocument{
			Keys: []jwk{ecJWK("test-key", privateKey, "sig", "ES256")},
		})
	}))
	defer server.Close()

	verifier, err := NewHumanVerifier(
		server.URL,
		"https://issuer.example",
		"authenticated",
		[]string{"ES256"},
	)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	expired := signHumanToken(
		t,
		privateKey,
		"test-key",
		"https://issuer.example",
		"authenticated",
		time.Now().Add(-time.Minute),
		time.Now().Add(-2*time.Minute),
	)

	if _, err := verifier.Verify(t.Context(), expired); err == nil {
		t.Fatal("expected expired JWT to fail")
	}
}

func TestHumanVerifierRejectsFutureNotBefore(t *testing.T) {
	privateKey := mustECDSAKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksDocument{
			Keys: []jwk{ecJWK("test-key", privateKey, "sig", "ES256")},
		})
	}))
	defer server.Close()

	verifier, err := NewHumanVerifier(
		server.URL,
		"https://issuer.example",
		"authenticated",
		[]string{"ES256"},
	)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	signed := signHumanToken(
		t,
		privateKey,
		"test-key",
		"https://issuer.example",
		"authenticated",
		time.Now().Add(5*time.Minute),
		time.Now().Add(5*time.Minute),
	)

	if _, err := verifier.Verify(t.Context(), signed); err == nil {
		t.Fatal("expected future nbf to fail")
	}
}

func TestHumanVerifierRefreshesUnknownKeyID(t *testing.T) {
	firstKey := mustECDSAKey(t)
	secondKey := mustECDSAKey(t)

	state := &mutableJWKS{}
	state.set(jwksDocument{
		Keys: []jwk{ecJWK("key-one", firstKey, "sig", "ES256")},
	})

	server := httptest.NewServer(http.HandlerFunc(state.serve))
	defer server.Close()

	verifier, err := NewHumanVerifier(
		server.URL,
		"https://issuer.example",
		"authenticated",
		[]string{"ES256"},
	)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	first := signHumanToken(
		t,
		firstKey,
		"key-one",
		"https://issuer.example",
		"authenticated",
		time.Now().Add(5*time.Minute),
		time.Now().Add(-time.Minute),
	)

	if _, err := verifier.Verify(t.Context(), first); err != nil {
		t.Fatalf("verify first key: %v", err)
	}

	state.set(jwksDocument{
		Keys: []jwk{ecJWK("key-two", secondKey, "sig", "ES256")},
	})

	second := signHumanToken(
		t,
		secondKey,
		"key-two",
		"https://issuer.example",
		"authenticated",
		time.Now().Add(5*time.Minute),
		time.Now().Add(-time.Minute),
	)

	if _, err := verifier.Verify(t.Context(), second); err != nil {
		t.Fatalf("verify rotated key: %v", err)
	}

	if state.requests.Load() < 2 {
		t.Fatalf("expected unknown kid to force JWKS refresh")
	}
}

func TestHumanVerifierRejectsEncryptionOnlyJWK(t *testing.T) {
	privateKey := mustECDSAKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwksDocument{
			Keys: []jwk{ecJWK("test-key", privateKey, "enc", "ES256")},
		})
	}))
	defer server.Close()

	verifier, err := NewHumanVerifier(
		server.URL,
		"https://issuer.example",
		"authenticated",
		[]string{"ES256"},
	)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	signed := signHumanToken(
		t,
		privateKey,
		"test-key",
		"https://issuer.example",
		"authenticated",
		time.Now().Add(5*time.Minute),
		time.Now().Add(-time.Minute),
	)

	if _, err := verifier.Verify(t.Context(), signed); err == nil {
		t.Fatal("expected encryption-only JWK to fail")
	}
}

func mustECDSAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	return key
}

func ecJWK(
	kid string,
	privateKey *ecdsa.PrivateKey,
	use string,
	algorithm string,
) jwk {
	return jwk{
		Kid: kid,
		Kty: "EC",
		Alg: algorithm,
		Use: use,
		Crv: "P-256",
		X: base64.RawURLEncoding.EncodeToString(
			padded32(privateKey.PublicKey.X),
		),
		Y: base64.RawURLEncoding.EncodeToString(
			padded32(privateKey.PublicKey.Y),
		),
	}
}

func signHumanToken(
	t *testing.T,
	privateKey *ecdsa.PrivateKey,
	kid string,
	issuer string,
	audience string,
	expiry time.Time,
	notBefore time.Time,
) string {
	t.Helper()

	claims := jwt.MapClaims{
		"sub": "human-auth-subject",
		"iss": issuer,
		"aud": audience,
		"exp": expiry.Unix(),
		"nbf": notBefore.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(privateKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	return signed
}

func padded32(value *big.Int) []byte {
	raw := value.Bytes()
	if len(raw) >= 32 {
		return raw
	}

	out := make([]byte, 32)
	copy(out[32-len(raw):], raw)

	return out
}
