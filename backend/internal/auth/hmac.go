package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type HMACKeyring struct {
	active int
	keys   map[int][]byte
}

func ParseHMACKeyring(active int, encoded string) (*HMACKeyring, error) {
	if active <= 0 {
		return nil, errors.New("active key version must be positive")
	}

	keys := map[int][]byte{}

	for _, pair := range strings.Split(encoded, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid keyring entry %q", pair)
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid key version %q", parts[0])
		}

		key, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("decode key version %d: %w", version, err)
		}

		if len(key) < 32 {
			return nil, fmt.Errorf("key version %d must contain at least 256 bits", version)
		}

		keys[version] = append([]byte(nil), key...)
	}

	if len(keys) == 0 {
		return nil, errors.New("keyring contains no keys")
	}

	if _, ok := keys[active]; !ok {
		return nil, fmt.Errorf("active key version %d is missing", active)
	}

	return &HMACKeyring{
		active: active,
		keys:   keys,
	}, nil
}

func (k *HMACKeyring) ActiveVersion() int {
	return k.active
}

func (k *HMACKeyring) MAC(version int, message []byte) ([]byte, error) {
	key, ok := k.keys[version]
	if !ok {
		return nil, fmt.Errorf("unknown key version %d", version)
	}

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil), nil
}

func (k *HMACKeyring) Verify(version int, message, presented []byte) bool {
	expected, err := k.MAC(version, message)
	if err != nil || len(expected) != len(presented) {
		return false
	}

	return subtle.ConstantTimeCompare(expected, presented) == 1
}

func Canonical(parts ...string) []byte {
	total := 0
	for _, part := range parts {
		total += 4 + len(part)
	}

	out := make([]byte, 0, total)
	length := make([]byte, 4)

	for _, part := range parts {
		binary.BigEndian.PutUint32(length, uint32(len(part)))
		out = append(out, length...)
		out = append(out, part...)
	}

	return out
}

func TokenHash(encoded string) [32]byte {
	return sha256.Sum256([]byte(encoded))
}
