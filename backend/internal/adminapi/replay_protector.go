package adminapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

type ReplayProtector struct {
	aead cipher.AEAD
}

func NewReplayProtector(
	key []byte,
) (*ReplayProtector, error) {
	if len(key) != 32 {
		return nil, errors.New(
			"credential replay key must be exactly 32 bytes",
		)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &ReplayProtector{
		aead: aead,
	}, nil
}

func NewReplayProtectorFromEncoded(
	encoded string,
) (*ReplayProtector, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}

	decoders := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}

	var key []byte
	var lastErr error

	for _, decoder := range decoders {
		decoded, err := decoder.DecodeString(encoded)
		if err == nil {
			key = decoded
			break
		}
		lastErr = err
	}

	if len(key) == 0 {
		return nil, fmt.Errorf(
			"decode credential replay key: %w",
			lastErr,
		)
	}

	return NewReplayProtector(key)
}

func (p *ReplayProtector) Protect(
	plaintext []byte,
) (string, error) {
	if p == nil {
		return "", errors.New(
			"credential replay protection is not configured",
		)
	}

	nonce := make(
		[]byte,
		p.aead.NonceSize(),
	)

	if _, err := io.ReadFull(
		rand.Reader,
		nonce,
	); err != nil {
		return "", err
	}

	ciphertext := p.aead.Seal(
		nil,
		nonce,
		plaintext,
		nil,
	)

	combined := append(
		append(
			[]byte(nil),
			nonce...,
		),
		ciphertext...,
	)

	return base64.RawURLEncoding.EncodeToString(
		combined,
	), nil
}

func (p *ReplayProtector) Unprotect(
	encoded string,
) ([]byte, error) {
	if p == nil {
		return nil, errors.New(
			"credential replay protection is not configured",
		)
	}

	combined, err := base64.RawURLEncoding.DecodeString(
		encoded,
	)
	if err != nil {
		return nil, err
	}

	nonceSize := p.aead.NonceSize()
	if len(combined) <= nonceSize {
		return nil, errors.New(
			"invalid protected replay payload",
		)
	}

	return p.aead.Open(
		nil,
		combined[:nonceSize],
		combined[nonceSize:],
		nil,
	)
}
