package adminapi

import (
	"encoding/json"
	"time"
)

type createVenueRequest struct {
	Name        string          `json:"name"`
	AddressText string          `json:"address_text,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

type sectionRequest struct {
	ObjectKey string          `json:"object_key"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	SortOrder int             `json:"sort_order,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type rowRequest struct {
	ObjectKey  string          `json:"object_key"`
	SectionKey string          `json:"section_key"`
	Label      string          `json:"label"`
	SortOrder  int             `json:"sort_order,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type tableRequest struct {
	ObjectKey  string          `json:"object_key"`
	SectionKey string          `json:"section_key"`
	Label      string          `json:"label"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type seatRequest struct {
	ObjectKey  string          `json:"object_key"`
	SectionKey string          `json:"section_key"`
	RowKey     string          `json:"row_key,omitempty"`
	TableKey   string          `json:"table_key,omitempty"`
	SeatLabel  string          `json:"seat_label"`
	SortOrder  int             `json:"sort_order,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type gaZoneRequest struct {
	ObjectKey       string          `json:"object_key"`
	SectionKey      string          `json:"section_key"`
	Name            string          `json:"name"`
	DefaultCapacity *int            `json:"default_capacity,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type replaceLayoutRequest struct {
	Geometry json.RawMessage  `json:"geometry"`
	Sections []sectionRequest `json:"sections"`
	Rows     []rowRequest     `json:"rows,omitempty"`
	Tables   []tableRequest   `json:"tables,omitempty"`
	Seats    []seatRequest    `json:"seats,omitempty"`
	GAZones  []gaZoneRequest  `json:"ga_zones,omitempty"`
}

type createEventRequest struct {
	VenueID          string     `json:"venue_id"`
	Name             string     `json:"name"`
	StartsAt         *time.Time `json:"starts_at,omitempty"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	SalesOpenAt      *time.Time `json:"sales_open_at,omitempty"`
	SalesCloseAt     *time.Time `json:"sales_close_at,omitempty"`
	AdmissionOpenAt  *time.Time `json:"admission_open_at,omitempty"`
	AdmissionCloseAt *time.Time `json:"admission_close_at,omitempty"`
	TimezoneName     string     `json:"timezone_name,omitempty"`
}

type updateEventRequest struct {
	Name             *string    `json:"name,omitempty"`
	StartsAt         *time.Time `json:"starts_at,omitempty"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	SalesOpenAt      *time.Time `json:"sales_open_at,omitempty"`
	SalesCloseAt     *time.Time `json:"sales_close_at,omitempty"`
	AdmissionOpenAt  *time.Time `json:"admission_open_at,omitempty"`
	AdmissionCloseAt *time.Time `json:"admission_close_at,omitempty"`
	TimezoneName     *string    `json:"timezone_name,omitempty"`
}

type materializeLayoutRequest struct {
	LayoutID string `json:"layout_id"`
}

type transactionPolicyRequest struct {
	HoldDurationSeconds                  int  `json:"hold_duration_seconds"`
	CheckoutProtectionSeconds            int  `json:"checkout_protection_seconds"`
	PaymentRetrySeconds                  int  `json:"payment_retry_seconds"`
	ReconciliationSeconds                int  `json:"reconciliation_seconds"`
	MaxReservationLifetimeSeconds        int  `json:"max_reservation_lifetime_seconds"`
	MaxHoldQuantity                      int  `json:"max_hold_quantity"`
	MaxActiveReservationsPerPartner      int  `json:"max_active_reservations_per_partner"`
	MaxActiveReservationsPerBuyerSession int  `json:"max_active_reservations_per_buyer_session"`
	AllowVoidedInventoryRerelease        bool `json:"allow_voided_inventory_rerelease"`
}

type createPriceTierRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type updatePriceTierRequest struct {
	Name        *string `json:"name,omitempty"`
	AmountMinor *int64  `json:"amount_minor,omitempty"`
	State       *string `json:"state,omitempty"`
}

type pricingAssignmentRequest struct {
	PriceTierID        string   `json:"price_tier_id"`
	SectionObjectKeys  []string `json:"section_object_keys,omitempty"`
	ReservedObjectKeys []string `json:"reserved_object_keys,omitempty"`
	GAPoolObjectKeys   []string `json:"ga_pool_object_keys,omitempty"`
}

type createPartnerRequest struct {
	Name string `json:"name"`
}
