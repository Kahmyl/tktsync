package allocation

import (
	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type Service struct {
	transactions *database.Runner
	audit        audit.Store
	outbox       outbox.Store
	qrKeys       *auth.HMACKeyring
}

func NewService(
	transactions *database.Runner,
	qrKeys ...*auth.HMACKeyring,
) *Service {
	var qrKeyring *auth.HMACKeyring

	if len(qrKeys) > 0 {
		qrKeyring = qrKeys[0]
	}

	return &Service{
		transactions: transactions,
		qrKeys:       qrKeyring,
	}
}

type GATarget struct {
	PoolID   uuid.UUID
	Quantity int
}

type BlockInput struct {
	Purpose         string
	Reason          string
	ReservedUnitIDs []uuid.UUID
	GATargets       []GATarget
}

type AllocationInput struct {
	Mode                   string
	PartnerID              *uuid.UUID
	Purpose                string
	Reason                 string
	ReleaseDestinationKind string
	ReleaseDestinationID   *uuid.UUID
	ReservedUnitIDs        []uuid.UUID
	GATargets              []GATarget
}
