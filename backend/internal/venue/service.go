package venue

import (
	"encoding/json"

	"github.com/tktsync/tktsync/backend/internal/audit"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type Service struct {
	transactions *database.Runner
	audit        audit.Store
	outbox       outbox.Store
}

func NewService(transactions *database.Runner) *Service {
	return &Service{
		transactions: transactions,
	}
}

type CreateVenueInput struct {
	Name        string
	AddressText string
	Metadata    json.RawMessage
}

type SectionInput struct {
	ObjectKey string
	Name      string
	Kind      string
	SortOrder int
	Metadata  json.RawMessage
}

type RowInput struct {
	ObjectKey  string
	SectionKey string
	Label      string
	SortOrder  int
	Metadata   json.RawMessage
}

type TableInput struct {
	ObjectKey  string
	SectionKey string
	Label      string
	Metadata   json.RawMessage
}

type SeatInput struct {
	ObjectKey  string
	SectionKey string
	RowKey     string
	TableKey   string
	SeatLabel  string
	SortOrder  int
	Metadata   json.RawMessage
}

type GAZoneInput struct {
	ObjectKey       string
	SectionKey      string
	Name            string
	DefaultCapacity *int
	Metadata        json.RawMessage
}

type ReplaceLayoutInput struct {
	Geometry json.RawMessage
	Sections []SectionInput
	Rows     []RowInput
	Tables   []TableInput
	Seats    []SeatInput
	GAZones  []GAZoneInput
}
