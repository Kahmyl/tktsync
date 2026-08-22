package reservation

import (
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

const (
	InventoryReserved = "RESERVED"
	InventoryGA       = "GA"

	SourceShared     = "SHARED"
	SourceAllocation = "ALLOCATION"
)

type ItemInput struct {
	ReservationItemID  *uuid.UUID
	OfferID            string
	InventoryKind      string
	InventoryID        uuid.UUID
	Quantity           int
	SourceKind         string
	SourceAllocationID *uuid.UUID
}

type CreateInput struct {
	EventID                 uuid.UUID
	PartnerID               uuid.UUID
	BuyerSelectionSessionID *uuid.UUID
	PartnerCustomerRef      string
	PartnerOrderRef         string
	BuyerSessionRef         string
	Items                   []ItemInput
}

type Created struct {
	ReservationID uuid.UUID
	Token         string
	State         string
	Currency      string
	HoldExpiresAt time.Time
	MaxLifetimeAt time.Time
}

type Modified struct {
	ReservationID uuid.UUID
	State         string
	Currency      string
	HoldExpiresAt time.Time
}

type Checkout struct {
	ReservationID     uuid.UUID
	CheckoutAttemptID uuid.UUID
	AttemptNumber     int
	State             string
	CommitExpiresAt   time.Time
}

type Retry struct {
	ReservationID         uuid.UUID
	State                 string
	PaymentRetryExpiresAt time.Time
}

type Reconciliation struct {
	ReservationID           uuid.UUID
	State                   string
	ReconciliationExpiresAt time.Time
}

type ConfirmInput struct {
	CheckoutAttemptID uuid.UUID
	PartnerOrderRef   string
	PartnerPaymentRef string
}

type ConfirmedTicket struct {
	TicketID     uuid.UUID
	CredentialID uuid.UUID
	State        string
}

type ActiveCredential struct {
	TicketID     uuid.UUID
	CredentialID uuid.UUID
	State        string
	QRPayload    string
}

type Confirmation struct {
	ReservationID     uuid.UUID
	State             string
	SaleID            uuid.UUID
	ConfirmedAt       time.Time
	PartnerOrderRef   string
	PartnerPaymentRef string
	Tickets           []ConfirmedTicket
}

type Service struct {
	transactions *database.Runner
	keys         *auth.HMACKeyring
	qrKeys       *auth.HMACKeyring
	audit        audit.Store
	outbox       outbox.Store
}

func NewService(
	transactions *database.Runner,
	keys *auth.HMACKeyring,
	qrKeys ...*auth.HMACKeyring,
) *Service {
	var qrKeyring *auth.HMACKeyring

	if len(qrKeys) > 0 {
		qrKeyring = qrKeys[0]
	}

	return &Service{
		transactions: transactions,
		keys:         keys,
		qrKeys:       qrKeyring,
		audit:        audit.Store{},
		outbox:       outbox.Store{},
	}
}
