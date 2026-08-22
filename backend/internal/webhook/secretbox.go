package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

type SecretBox struct{ aead cipher.AEAD }

func NewSecretBox(encoded string) (*SecretBox, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}
	var key []byte
	for _, decoder := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		decoded, err := decoder.DecodeString(encoded)
		if err == nil {
			key = decoded
			break
		}
	}
	if len(key) != 32 {
		return nil, errors.New("webhook encryption key must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretBox{aead: aead}, nil
}

func (b *SecretBox) Seal(plaintext []byte) ([]byte, error) {
	if b == nil {
		return nil, errors.New("webhook secret encryption is not configured")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return append(nonce, b.aead.Seal(nil, nonce, plaintext, nil)...), nil
}
func (b *SecretBox) Open(ciphertext []byte) ([]byte, error) {
	if b == nil {
		return nil, errors.New("webhook secret encryption is not configured")
	}
	size := b.aead.NonceSize()
	if len(ciphertext) <= size {
		return nil, errors.New("invalid webhook secret ciphertext")
	}
	return b.aead.Open(nil, ciphertext[:size], ciphertext[size:], nil)
}
