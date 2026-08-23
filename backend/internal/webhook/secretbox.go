package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type SecretBox struct {
	defaultAEAD cipher.AEAD
	versions    map[int]cipher.AEAD
}

func NewSecretBox(encoded string) (*SecretBox, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, nil
	}

	aead, err := decodeSecretAEAD(encoded)
	if err != nil {
		return nil, err
	}

	return &SecretBox{
		defaultAEAD: aead,
	}, nil
}

func NewVersionedSecretBox(
	activeVersion int,
	activeEncoded string,
	keyringJSON string,
) (*SecretBox, error) {
	activeEncoded = strings.TrimSpace(activeEncoded)
	keyringJSON = strings.TrimSpace(keyringJSON)

	if activeEncoded == "" &&
		keyringJSON == "" {
		return nil, nil
	}

	if activeVersion <= 0 {
		return nil,
			errors.New(
				"webhook encryption key version must be positive",
			)
	}

	versions :=
		map[int]cipher.AEAD{}

	if keyringJSON != "" {
		var raw map[string]string

		if err := json.Unmarshal(
			[]byte(keyringJSON),
			&raw,
		); err != nil {
			return nil,
				fmt.Errorf(
					"parse webhook encryption keyring: %w",
					err,
				)
		}

		for rawVersion, encoded := range raw {
			version, err :=
				strconv.Atoi(
					strings.TrimSpace(
						rawVersion,
					),
				)
			if err != nil ||
				version <= 0 {
				return nil,
					fmt.Errorf(
						"webhook encryption keyring version %q is invalid",
						rawVersion,
					)
			}

			aead, err :=
				decodeSecretAEAD(
					encoded,
				)
			if err != nil {
				return nil,
					fmt.Errorf(
						"webhook encryption key version %d: %w",
						version,
						err,
					)
			}

			versions[version] = aead
		}
	}

	if activeEncoded != "" {
		aead, err :=
			decodeSecretAEAD(
				activeEncoded,
			)
		if err != nil {
			return nil, err
		}

		versions[activeVersion] = aead
	}

	activeAEAD, ok :=
		versions[activeVersion]
	if !ok {
		return nil,
			fmt.Errorf(
				"webhook encryption keyring does not contain active version %d",
				activeVersion,
			)
	}

	return &SecretBox{
		defaultAEAD: activeAEAD,
		versions:    versions,
	}, nil
}

func decodeSecretAEAD(
	encoded string,
) (cipher.AEAD, error) {
	encoded = strings.TrimSpace(encoded)

	var key []byte

	for _, decoder := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		decoded, err :=
			decoder.DecodeString(
				encoded,
			)
		if err == nil {
			key = decoded
			break
		}
	}

	if len(key) != 32 {
		return nil,
			errors.New(
				"webhook encryption key must decode to exactly 32 bytes",
			)
	}

	block, err :=
		aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aead, err :=
		cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return aead, nil
}

func (b *SecretBox) Seal(
	plaintext []byte,
) ([]byte, error) {
	if b == nil ||
		b.defaultAEAD == nil {
		return nil,
			errors.New(
				"webhook secret encryption is not configured",
			)
	}

	return sealWithAEAD(
		b.defaultAEAD,
		plaintext,
	)
}

func (b *SecretBox) Open(
	ciphertext []byte,
) ([]byte, error) {
	if b == nil ||
		b.defaultAEAD == nil {
		return nil,
			errors.New(
				"webhook secret encryption is not configured",
			)
	}

	return openWithAEAD(
		b.defaultAEAD,
		ciphertext,
	)
}

func (b *SecretBox) SealVersion(
	version int,
	plaintext []byte,
) ([]byte, error) {
	aead, err :=
		b.aeadForVersion(
			version,
		)
	if err != nil {
		return nil, err
	}

	return sealWithAEAD(
		aead,
		plaintext,
	)
}

func (b *SecretBox) OpenVersion(
	version int,
	ciphertext []byte,
) ([]byte, error) {
	aead, err :=
		b.aeadForVersion(
			version,
		)
	if err != nil {
		return nil, err
	}

	return openWithAEAD(
		aead,
		ciphertext,
	)
}

func (b *SecretBox) aeadForVersion(
	version int,
) (cipher.AEAD, error) {
	if b == nil ||
		b.defaultAEAD == nil {
		return nil,
			errors.New(
				"webhook secret encryption is not configured",
			)
	}

	if len(b.versions) == 0 {
		return b.defaultAEAD, nil
	}

	aead, ok :=
		b.versions[version]
	if !ok {
		return nil,
			fmt.Errorf(
				"webhook encryption key version %d is not configured",
				version,
			)
	}

	return aead, nil
}

func sealWithAEAD(
	aead cipher.AEAD,
	plaintext []byte,
) ([]byte, error) {
	nonce :=
		make(
			[]byte,
			aead.NonceSize(),
		)

	if _, err :=
		io.ReadFull(
			rand.Reader,
			nonce,
		); err != nil {
		return nil, err
	}

	return append(
		nonce,
		aead.Seal(
			nil,
			nonce,
			plaintext,
			nil,
		)...,
	), nil
}

func openWithAEAD(
	aead cipher.AEAD,
	ciphertext []byte,
) ([]byte, error) {
	size :=
		aead.NonceSize()

	if len(ciphertext) <= size {
		return nil,
			errors.New(
				"invalid webhook secret ciphertext",
			)
	}

	return aead.Open(
		nil,
		ciphertext[:size],
		ciphertext[size:],
		nil,
	)
}
