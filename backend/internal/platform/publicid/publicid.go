package publicid

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Kind string

const (
	Event             Kind = "evt"
	Reservation       Kind = "res"
	Sale              Kind = "sale"
	Ticket            Kind = "tkt"
	ReservedInventory Kind = "inv"
	GAPool            Kind = "ga"
	CheckoutAttempt   Kind = "chk"
	Credential        Kind = "cred"
	ScanAttempt       Kind = "scan"
	Admission         Kind = "adm"
	Partner           Kind = "ptr"
	SelectionSession  Kind = "sel"
	WebhookEndpoint   Kind = "wh"
)

var ErrInvalid = errors.New("invalid public identifier")

func Encode(kind Kind, id uuid.UUID) string {
	return string(kind) + "_" + base64.RawURLEncoding.EncodeToString(id[:])
}

func New(kind Kind) (string, uuid.UUID) {
	id := uuid.New()
	return Encode(kind, id), id
}

func Parse(value string, expected Kind) (uuid.UUID, error) {
	prefix := string(expected) + "_"
	if !strings.HasPrefix(value, prefix) {
		return uuid.Nil, ErrInvalid
	}

	raw := strings.TrimPrefix(value, prefix)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 16 {
		return uuid.Nil, ErrInvalid
	}

	id, err := uuid.FromBytes(decoded)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	return id, nil
}
