package inventory

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
)

type OfferSourceKind string

const (
	OfferSourceShared     OfferSourceKind = "SHARED"
	OfferSourceAllocation OfferSourceKind = "ALLOCATION"
)

func OfferID(
	eventID uuid.UUID,
	partnerID uuid.UUID,
	inventoryKind string,
	inventoryID uuid.UUID,
	sourceKind OfferSourceKind,
	sourceID uuid.UUID,
	priceTierID uuid.UUID,
	amountMinor int64,
	currency string,
) string {
	canonical := strings.Join(
		[]string{
			eventID.String(),
			partnerID.String(),
			inventoryKind,
			inventoryID.String(),
			string(sourceKind),
			sourceID.String(),
			priceTierID.String(),
			currency,
			formatInt64(amountMinor),
		},
		"\x1f",
	)

	sum := sha256.Sum256([]byte(canonical))

	return "off_" +
		base64.RawURLEncoding.EncodeToString(sum[:])
}

func formatInt64(value int64) string {
	if value == 0 {
		return "0"
	}

	negative := value < 0
	if negative {
		value = -value
	}

	var buffer [20]byte
	position := len(buffer)

	for value > 0 {
		position--
		buffer[position] = byte('0' + value%10)
		value /= 10
	}

	result := string(buffer[position:])

	if negative {
		return "-" + result
	}

	return result
}
