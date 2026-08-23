-- Reporting remains a derived read surface. These indexes support stable,
-- event-scoped audit pagination and authoritative report joins only.
CREATE INDEX IF NOT EXISTS audit_events_event_time_id_idx
ON audit_events(event_id, occurred_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS sales_event_partner_time_idx
ON sales(event_id, partner_id, confirmed_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS ticket_entitlements_event_status_idx
ON ticket_entitlements(event_id, status, id);

CREATE INDEX IF NOT EXISTS admissions_event_status_idx
ON admissions(event_id, status, admitted_at DESC, id DESC);
