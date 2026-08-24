export type EventState =
  'DRAFT' | 'ON_SALE' | 'PAUSED' | 'SALES_CLOSED' | 'COMPLETED' | 'CANCELLED';

export interface EventSummary {
  id: string;
  venue_id: string;
  venue_name: string;
  name: string;
  state: EventState;
  starts_at: string | null;
  ends_at: string | null;
  timezone_name: string | null;
  capacity: number;
  sold: number;
  created_at: string;
  updated_at: string;
}

export interface EventDetail extends Omit<EventSummary, 'venue_name' | 'capacity' | 'sold'> {
  sales_open_at: string | null;
  sales_close_at: string | null;
  admission_open_at: string | null;
  admission_close_at: string | null;
}

export interface DashboardData {
  metrics: {
    active_events: number;
    tickets_sold: number;
    reservations_today: number;
    checkins_today: number;
  };
  upcoming_events: Array<
    Pick<EventSummary, 'id' | 'name' | 'state' | 'starts_at' | 'venue_name' | 'capacity' | 'sold'>
  >;
  attention: Array<{
    event_id: string;
    event_name: string;
    state: EventState;
    layout_ready: boolean;
    pricing_ready: boolean;
    policy_ready: boolean;
  }>;
  recent_activity: ActivityItem[];
}

export interface VenueSummary {
  id: string;
  name: string;
  address_text: string | null;
  created_at: string;
  updated_at: string;
}

export interface LayoutVersion {
  id: string;
  version_number: number;
  state: 'DRAFT' | 'PUBLISHED' | 'RETIRED';
  published_at: string | null;
  retired_at: string | null;
  created_at: string;
}

export interface PartnerSummary {
  id: string;
  name: string;
  state: 'ACTIVE' | 'DISABLED';
  active_event_count: number;
  active_credential_count: number;
  active_endpoint_count: number;
  last_activity_at: string | null;
  created_at: string;
  disabled_at: string | null;
}

export interface PlatformAdminUser {
  id: string;
  email: string | null;
  display_name: string | null;
  state: 'ACTIVE' | 'DISABLED';
  role: 'PLATFORM_ADMIN';
  is_current_user: boolean;
  created_at?: string;
  updated_at?: string;
  invitation_sent?: boolean;
}

export interface PartnerDetail {
  id: string;
  name: string;
  state: 'ACTIVE' | 'DISABLED';
  created_at: string;
  disabled_at: string | null;
  credentials: Array<{
    id: string;
    key_id: string;
    state: 'ACTIVE' | 'REVOKED';
    created_at: string;
    last_used_at: string | null;
    revoked_at: string | null;
  }>;
  event_access: Array<{
    event_id: string;
    event_name: string;
    event_state: EventState;
    state: 'ACTIVE' | 'DISABLED';
    created_at: string;
    disabled_at: string | null;
  }>;
  activity: ActivityItem[];
}

export interface EventConfiguration {
  layout?: { id: string; version_number: number; finalized_at: string | null };
  price_tiers: Array<{
    id: string;
    code: string;
    name: string;
    amount_minor: number;
    currency: string;
    state: 'ACTIVE' | 'RETIRED';
    created_at: string;
    updated_at: string;
  }>;
  partner_access: Array<{
    partner_id: string;
    partner_name: string;
    partner_state: 'ACTIVE' | 'DISABLED';
    access_state: 'ACTIVE' | 'DISABLED' | null;
    created_at: string | null;
    disabled_at: string | null;
  }>;
  transaction_policy: Record<string, number | boolean> | null;
}

export interface InventoryItem {
  kind: 'RESERVED' | 'GA';
  id: string;
  snapshot_object_key: string;
  label: string;
  quantity: number;
  price_tier_id: string | null;
}

export interface InventoryReport {
  generated_at: string;
  event: { id: string; name: string; state: EventState; starts_at: string | null };
  total: InventoryDimensions;
  reserved_seating: InventoryDimensions;
  general_admission: InventoryDimensions;
}

export interface InventoryDimensions {
  capacity: number;
  available: number;
  held: number;
  committing: number;
  payment_retry: number;
  reconciling: number;
  blocked: number;
  allocated: number;
  sold_current: number;
  issued_current: number;
  voided_tickets: number;
  capacity_consumed: number;
  historical_sold: number;
  historical_issued: number;
}

export interface SalesReport {
  generated_at: string;
  event: { id: string; name: string; state: EventState; starts_at: string | null };
  sale_count: number;
  historical_sale_quantity: number;
  historical_amount_minor: number;
  active_sold_tickets: number;
  voided_sold_tickets: number;
  current_sold_capacity: number;
  currency: string | null;
}

export interface AdmissionReport {
  generated_at: string;
  event: { id: string; name: string; state: EventState; starts_at: string | null };
  active_admissions: number;
  reversed_admissions: number;
  scan_outcomes: Record<string, number>;
}

export interface TicketSummary {
  id: string;
  event_id: string;
  event_name: string;
  status: 'ACTIVE' | 'VOIDED';
  inventory_kind: 'RESERVED' | 'GA';
  attendee_name: string | null;
  display_label: string | null;
  credential_id?: string;
  credential_state: 'ACTIVE' | 'SUPERSEDED' | 'REVOKED' | null;
  admission_id?: string;
  admission_state: 'ACTIVE' | 'REVERSED' | null;
  admitted_at: string | null;
  created_at: string;
  voided_at: string | null;
  void_reason: string | null;
}

export interface AdmissionEntry {
  id: string;
  event_id: string;
  event_name: string;
  result: string;
  gate_reference: string | null;
  attendee_name: string | null;
  display_label: string | null;
  ticket_id?: string;
  admission_id?: string;
  admission_state: 'ACTIVE' | 'REVERSED' | null;
  occurred_at: string;
}

export interface WebhookEndpoint {
  id: string;
  partner_id: string;
  partner_name: string;
  url: string;
  state: 'ACTIVE' | 'DISABLED';
  subscriptions: string[];
  delivered_24h: number;
  failed_24h: number;
  last_delivery_at: string | null;
  created_at: string;
}

export interface ActivityItem {
  operation: string;
  entity_type: string;
  occurred_at: string;
  event_id?: string;
  event_name?: string | null;
  partner_id?: string;
  partner_name?: string | null;
}

export interface PageResult<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}
