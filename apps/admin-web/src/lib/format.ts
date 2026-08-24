import type { EventState } from '../features/admin/types';

export const eventStateMeta: Record<
  EventState,
  { label: string; tone: 'neutral' | 'positive' | 'warning' | 'critical' | 'info' }
> = {
  DRAFT: { label: 'Draft', tone: 'neutral' },
  ON_SALE: { label: 'On sale', tone: 'positive' },
  PAUSED: { label: 'Paused', tone: 'warning' },
  SALES_CLOSED: { label: 'Sales closed', tone: 'info' },
  COMPLETED: { label: 'Completed', tone: 'neutral' },
  CANCELLED: { label: 'Cancelled', tone: 'critical' },
};

export function formatNumber(value: number | null | undefined) {
  return new Intl.NumberFormat('en-US').format(value ?? 0);
}

export function formatMoney(amountMinor: number, currency = 'NGN') {
  return new Intl.NumberFormat('en-NG', {
    style: 'currency',
    currency,
    maximumFractionDigits: 0,
  }).format(amountMinor / 100);
}

export function optionalISO(value: string) {
  return value ? new Date(value).toISOString() : undefined;
}

export function formatDateTime(value: string | null | undefined) {
  if (!value) return 'Not scheduled';
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value));
}

export function formatDate(value: string | null | undefined) {
  if (!value) return 'Not scheduled';
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  }).format(new Date(value));
}

export function timeAgo(value: string | null | undefined) {
  if (!value) return 'Never';
  const diff = Date.now() - new Date(value).getTime();
  const minutes = Math.max(0, Math.round(diff / 60_000));
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  if (days < 30) return `${days}d ago`;
  return formatDate(value);
}

export function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? '')
    .join('');
}

const uuidTokenPattern = /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi;
const opaqueHexTokenPattern = /\b[0-9a-f]{24,}\b/gi;
const publicIDTokenPattern =
  /\b(?:evt|tkt|adm|scan|usr|ven|sec|row|seat|dev|cred|inv|ptr|lay|res|sal|wh)_[a-z0-9_-]+\b/gi;

/** Removes machine identifiers from a name while preserving its human-authored words. */
export function humanName(value: string | null | undefined, fallback = 'Untitled') {
  const readable = (value ?? '')
    .trim()
    .replace(uuidTokenPattern, ' ')
    .replace(opaqueHexTokenPattern, ' ')
    .replace(publicIDTokenPattern, ' ')
    .replace(/\(\s*\)|\[\s*\]|\{\s*\}/g, ' ')
    .replace(/\s+/g, ' ')
    .replace(/[\s·|:/–—-]+$/g, '')
    .trim();
  return readable || fallback;
}

export function humanGateReference(value: string | null | undefined) {
  return humanName(value, 'Scanner device');
}

const domainLabels: Record<string, string> = {
  APP_USER: 'Administrator',
  EVENT: 'Event',
  PARTNER: 'Partner',
  RESERVATION: 'Reservation',
  SALE: 'Sale',
  TICKET_ENTITLEMENT: 'Ticket',
  ADMISSION: 'Admission',
  WEBHOOK_ENDPOINT: 'Webhook endpoint',
  MANUAL_OVERRIDE_ADMITTED: 'Admitted by manual override',
};

export function humanDomainLabel(value: string | null | undefined, fallback = 'Activity') {
  if (!value) return fallback;
  if (domainLabels[value]) return domainLabels[value];
  const words = value.replaceAll('_', ' ').replaceAll('-', ' ').replaceAll('.', ' ').toLowerCase();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

export function friendlyOperation(operation: string) {
  return operation
    .replace(/^ADMIN_/, '')
    .replaceAll('_', ' ')
    .toLowerCase()
    .replace(/^./, (letter) => letter.toUpperCase());
}

export function admissionLabel(result: string) {
  const labels: Record<string, string> = {
    ADMITTED: 'Admitted',
    MANUAL_OVERRIDE_ADMITTED: 'Admitted',
    ALREADY_ADMITTED: 'Already admitted',
    TICKET_ALREADY_ADMITTED: 'Already admitted',
    INVALID_CREDENTIAL: 'Invalid',
    TICKET_INVALID: 'Invalid',
    NOT_AUTHORIZED: 'Rejected',
    WRONG_EVENT: 'Rejected',
    EVENT_CANCELLED: 'Rejected',
    ADMISSION_NOT_OPEN: 'Rejected',
    TICKET_VOID: 'Rejected',
    CREDENTIAL_REVOKED: 'Rejected',
    CREDENTIAL_SUPERSEDED: 'Rejected',
  };
  return labels[result] ?? 'Rejected';
}
