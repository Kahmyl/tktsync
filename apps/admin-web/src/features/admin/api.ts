import { createTktSyncClient } from '@tktsync/api-client';
import type {
  AdmissionEntry,
  AdmissionReport,
  DashboardData,
  EventConfiguration,
  EventDetail,
  EventSummary,
  InventoryItem,
  InventoryReport,
  LayoutVersion,
  PageResult,
  PartnerDetail,
  PartnerSummary,
  PlatformAdminUser,
  SalesReport,
  TicketSummary,
  VenueSummary,
  WebhookEndpoint,
} from './types';

const client = createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? '');

type ClientResult = {
  data?: unknown;
  error?: unknown;
  response: Response;
};

export class AdminApiError extends Error {
  readonly code?: string;
  readonly requestId?: string;
  readonly ambiguous: boolean;

  constructor(
    message: string,
    options: { code?: string; requestId?: string; ambiguous?: boolean } = {},
  ) {
    super(message);
    this.name = 'AdminApiError';
    this.code = options.code;
    this.requestId = options.requestId;
    this.ambiguous = options.ambiguous ?? false;
  }
}

function parseError(error: unknown, response: Response) {
  const candidate = error as
    { error?: { message?: string; code?: string }; message?: string; code?: string } | undefined;
  const nested = candidate?.error;
  const message = nested?.message ?? candidate?.message ?? `Request failed (${response.status})`;
  const code = nested?.code ?? candidate?.code;
  return new AdminApiError(message, {
    code,
    requestId: response.headers.get('x-request-id') ?? undefined,
  });
}

async function result<T>(request: Promise<ClientResult>): Promise<T> {
  try {
    const response = await request;
    if (response.error !== undefined || !response.response.ok) {
      throw parseError(response.error, response.response);
    }
    return response.data as T;
  } catch (error) {
    if (error instanceof AdminApiError) throw error;
    throw new AdminApiError(
      'The result could not be confirmed. Check the latest state before retrying.',
      {
        ambiguous: true,
      },
    );
  }
}

function headers(token: string, idempotencyKey?: string) {
  return {
    Authorization: `Bearer ${token}`,
    ...(idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : {}),
  };
}

export const adminApi = {
  dashboard: (token: string) =>
    result<DashboardData>(client.GET('/api/v1/admin/dashboard', { headers: headers(token) })),

  users: (token: string, query = '', state = '') =>
    result<PageResult<PlatformAdminUser>>(
      client.GET('/api/v1/admin/users', {
        headers: headers(token),
        params: { query: { query: query || undefined, state: state || undefined, limit: 100 } },
      }),
    ),

  events: (token: string, query = '', state = '') =>
    result<PageResult<EventSummary>>(
      client.GET('/api/v1/admin/events', {
        headers: headers(token),
        params: { query: { query: query || undefined, state: state || undefined, limit: 100 } },
      }),
    ),
  event: (token: string, eventId: string) =>
    result<EventDetail>(
      client.GET('/api/v1/admin/events/{event_id}', {
        headers: headers(token),
        params: { path: { event_id: eventId } },
      }),
    ),
  eventConfiguration: (token: string, eventId: string) =>
    result<EventConfiguration>(
      client.GET('/api/v1/admin/events/{event_id}/configuration', {
        headers: headers(token),
        params: { path: { event_id: eventId } },
      }),
    ),
  eventInventory: (token: string, eventId: string) =>
    result<{ event_id: string; inventory: InventoryItem[] }>(
      client.GET('/api/v1/admin/events/{event_id}/inventory', {
        headers: headers(token),
        params: { path: { event_id: eventId } },
      }),
    ),
  inventoryReport: (token: string, eventId: string) =>
    result<InventoryReport>(
      client.GET('/api/v1/admin/events/{event_id}/reports/inventory', {
        headers: headers(token),
        params: { path: { event_id: eventId } },
      }),
    ),
  salesReport: (token: string, eventId: string) =>
    result<SalesReport>(
      client.GET('/api/v1/admin/events/{event_id}/reports/sales', {
        headers: headers(token),
        params: { path: { event_id: eventId } },
      }),
    ),
  admissionReport: (token: string, eventId: string) =>
    result<AdmissionReport>(
      client.GET('/api/v1/admin/events/{event_id}/reports/admission', {
        headers: headers(token),
        params: { path: { event_id: eventId } },
      }),
    ),
  audit: (token: string, eventId: string) =>
    result<{ items: Array<Record<string, unknown>>; next_cursor: string | null }>(
      client.GET('/api/v1/admin/events/{event_id}/audit', {
        headers: headers(token),
        params: { path: { event_id: eventId }, query: { limit: 20 } },
      }),
    ),

  venues: async (token: string) => {
    const data = await result<{ venues: VenueSummary[] }>(
      client.GET('/api/v1/admin/venues', { headers: headers(token) }),
    );
    return data.venues;
  },
  venue: (token: string, venueId: string) =>
    result<VenueSummary>(
      client.GET('/api/v1/admin/venues/{venue_id}', {
        headers: headers(token),
        params: { path: { venue_id: venueId } },
      }),
    ),
  layouts: async (token: string, venueId: string) => {
    const data = await result<{ layout_versions: LayoutVersion[] }>(
      client.GET('/api/v1/admin/venues/{venue_id}/layout-versions', {
        headers: headers(token),
        params: { path: { venue_id: venueId } },
      }),
    );
    return data.layout_versions;
  },

  partners: (token: string, query = '', state = '') =>
    result<PageResult<PartnerSummary>>(
      client.GET('/api/v1/admin/partners', {
        headers: headers(token),
        params: { query: { query: query || undefined, state: state || undefined, limit: 100 } },
      }),
    ),
  partner: (token: string, partnerId: string) =>
    result<PartnerDetail>(
      client.GET('/api/v1/admin/partners/{partner_id}', {
        headers: headers(token),
        params: { path: { partner_id: partnerId } },
      }),
    ),

  tickets: (token: string, query = '', eventId = '', state = '') =>
    result<PageResult<TicketSummary>>(
      client.GET('/api/v1/admin/tickets', {
        headers: headers(token),
        params: {
          query: {
            query: query || undefined,
            event_id: eventId || undefined,
            state: state || undefined,
            limit: 100,
          },
        },
      }),
    ),
  ticket: (token: string, ticketId: string) =>
    result<TicketSummary>(
      client.GET('/api/v1/admin/tickets/{ticket_id}', {
        headers: headers(token),
        params: { path: { ticket_id: ticketId } },
      }),
    ),

  admissions: (token: string, eventId = '') =>
    result<PageResult<AdmissionEntry>>(
      client.GET('/api/v1/admin/admissions', {
        headers: headers(token),
        params: { query: { event_id: eventId || undefined, limit: 100 } },
      }),
    ),
  webhooks: (token: string) =>
    result<PageResult<WebhookEndpoint>>(
      client.GET('/api/v1/admin/webhook-endpoints', {
        headers: headers(token),
        params: { query: { limit: 100 } },
      }),
    ),

  createEvent: (
    token: string,
    idempotencyKey: string,
    body: {
      venue_id: string;
      name: string;
      starts_at?: string;
      ends_at?: string;
      sales_open_at?: string;
      sales_close_at?: string;
      admission_open_at?: string;
      admission_close_at?: string;
      timezone_name?: string;
    },
  ) =>
    result<{ id: string }>(
      client.POST('/api/v1/admin/events', {
        headers: headers(token, idempotencyKey),
        params: { header: { 'Idempotency-Key': idempotencyKey } },
        body,
      }),
    ),
  lifecycle: (
    token: string,
    idempotencyKey: string,
    eventId: string,
    action: 'open-sales' | 'pause-sales' | 'resume-sales' | 'close-sales' | 'complete',
  ) => {
    const path = `/api/v1/admin/events/{event_id}/${action}` as const;
    return result<{ event_id: string; state: string }>(
      client.POST(path, {
        headers: headers(token, idempotencyKey),
        params: { path: { event_id: eventId }, header: { 'Idempotency-Key': idempotencyKey } },
      }),
    );
  },
  cancelEvent: (token: string, idempotencyKey: string, eventId: string, reason: string) =>
    result<{ event_id: string; state: string }>(
      client.POST('/api/v1/admin/events/{event_id}/cancel', {
        headers: headers(token, idempotencyKey),
        params: { path: { event_id: eventId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: { reason },
      }),
    ),
  configurePolicy: (token: string, idempotencyKey: string, eventId: string) =>
    result<Record<string, unknown>>(
      client.PUT('/api/v1/admin/events/{event_id}/transaction-policy', {
        headers: headers(token, idempotencyKey),
        params: { path: { event_id: eventId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: {
          hold_duration_seconds: 600,
          checkout_protection_seconds: 120,
          payment_retry_seconds: 300,
          reconciliation_seconds: 600,
          max_reservation_lifetime_seconds: 1800,
          max_hold_quantity: 12,
          max_active_reservations_per_partner: 500,
          max_active_reservations_per_buyer_session: 3,
          allow_voided_inventory_rerelease: false,
        },
      }),
    ),
  createPriceTier: (
    token: string,
    idempotencyKey: string,
    eventId: string,
    body: { code: string; name: string; amount_minor: number; currency: string },
  ) =>
    result<{ id: string }>(
      client.POST('/api/v1/admin/events/{event_id}/price-tiers', {
        headers: headers(token, idempotencyKey),
        params: { path: { event_id: eventId }, header: { 'Idempotency-Key': idempotencyKey } },
        body,
      }),
    ),
  assignPricing: (
    token: string,
    idempotencyKey: string,
    eventId: string,
    body: {
      price_tier_id: string;
      section_object_keys?: string[];
      reserved_object_keys?: string[];
      ga_pool_object_keys?: string[];
    },
  ) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/events/{event_id}/pricing/assignments', {
        headers: headers(token, idempotencyKey),
        params: { path: { event_id: eventId }, header: { 'Idempotency-Key': idempotencyKey } },
        body,
      }),
    ),
  materializeLayout: (token: string, idempotencyKey: string, eventId: string, layoutId: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/events/{event_id}/materialize-layout', {
        headers: headers(token, idempotencyKey),
        params: { path: { event_id: eventId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: { layout_id: layoutId },
      }),
    ),

  createVenue: (
    token: string,
    idempotencyKey: string,
    body: { name: string; address_text?: string },
  ) =>
    result<{ id: string }>(
      client.POST('/api/v1/admin/venues', {
        headers: headers(token, idempotencyKey),
        params: { header: { 'Idempotency-Key': idempotencyKey } },
        body,
      }),
    ),
  createLayout: (token: string, idempotencyKey: string, venueId: string) =>
    result<{ id: string; version_number: number; state: string }>(
      client.POST('/api/v1/admin/venues/{venue_id}/layout-versions', {
        headers: headers(token, idempotencyKey),
        params: { path: { venue_id: venueId }, header: { 'Idempotency-Key': idempotencyKey } },
      }),
    ),
  replaceLayout: (
    token: string,
    idempotencyKey: string,
    layoutId: string,
    sections: Array<{ object_key: string; name: string; kind: 'RESERVED' | 'GA' }>,
  ) =>
    result<Record<string, unknown>>(
      client.PATCH('/api/v1/admin/venue-layouts/{layout_id}', {
        headers: headers(token, idempotencyKey),
        params: { path: { layout_id: layoutId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: {
          geometry: {},
          sections,
          ga_zones: sections
            .filter((section) => section.kind === 'GA')
            .map((section) => ({
              object_key: `${section.object_key}-zone`,
              section_key: section.object_key,
              name: section.name,
            })),
        },
      }),
    ),
  publishLayout: (token: string, idempotencyKey: string, layoutId: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/venue-layouts/{layout_id}/publish', {
        headers: headers(token, idempotencyKey),
        params: { path: { layout_id: layoutId }, header: { 'Idempotency-Key': idempotencyKey } },
      }),
    ),

  createPartner: (token: string, idempotencyKey: string, name: string) =>
    result<{ id: string; state: string }>(
      client.POST('/api/v1/admin/partners', {
        headers: headers(token, idempotencyKey),
        params: { header: { 'Idempotency-Key': idempotencyKey } },
        body: { name },
      }),
    ),
  createPlatformAdmin: (
    token: string,
    idempotencyKey: string,
    body: { email: string; display_name: string },
  ) =>
    result<PlatformAdminUser>(
      client.POST('/api/v1/admin/users', {
        headers: headers(token, idempotencyKey),
        params: { header: { 'Idempotency-Key': idempotencyKey } },
        body,
      }),
    ),
  setPlatformAdminState: (
    token: string,
    idempotencyKey: string,
    userId: string,
    enabled: boolean,
    reason: string,
  ) =>
    result<{ id: string; state: 'ACTIVE' | 'DISABLED' }>(
      enabled
        ? client.POST('/api/v1/admin/users/{user_id}/enable', {
            headers: headers(token, idempotencyKey),
            params: {
              path: { user_id: userId },
              header: { 'Idempotency-Key': idempotencyKey },
            },
            body: { reason },
          })
        : client.POST('/api/v1/admin/users/{user_id}/disable', {
            headers: headers(token, idempotencyKey),
            params: {
              path: { user_id: userId },
              header: { 'Idempotency-Key': idempotencyKey },
            },
            body: { reason },
          }),
    ),
  setPartnerState: (token: string, idempotencyKey: string, partnerId: string, enabled: boolean) =>
    result<Record<string, unknown>>(
      enabled
        ? client.POST('/api/v1/admin/partners/{partner_id}/enable', {
            headers: headers(token, idempotencyKey),
            params: {
              path: { partner_id: partnerId },
              header: { 'Idempotency-Key': idempotencyKey },
            },
          })
        : client.POST('/api/v1/admin/partners/{partner_id}/disable', {
            headers: headers(token, idempotencyKey),
            params: {
              path: { partner_id: partnerId },
              header: { 'Idempotency-Key': idempotencyKey },
            },
          }),
    ),
  issuePartnerCredential: (token: string, idempotencyKey: string, partnerId: string) =>
    result<{ id: string; partner_id: string; credential: string }>(
      client.POST('/api/v1/admin/partners/{partner_id}/credentials', {
        headers: headers(token, idempotencyKey),
        params: { path: { partner_id: partnerId }, header: { 'Idempotency-Key': idempotencyKey } },
      }),
    ),
  revokePartnerCredential: (token: string, idempotencyKey: string, credentialId: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/partner-credentials/{credential_id}/revoke', {
        headers: headers(token, idempotencyKey),
        params: {
          path: { credential_id: credentialId },
          header: { 'Idempotency-Key': idempotencyKey },
        },
      }),
    ),
  setEventAccess: (
    token: string,
    idempotencyKey: string,
    eventId: string,
    partnerId: string,
    enabled: boolean,
  ) =>
    result<Record<string, unknown>>(
      enabled
        ? client.POST('/api/v1/admin/events/{event_id}/partners/{partner_id}/access', {
            headers: headers(token, idempotencyKey),
            params: {
              path: { event_id: eventId, partner_id: partnerId },
              header: { 'Idempotency-Key': idempotencyKey },
            },
          })
        : client.POST('/api/v1/admin/events/{event_id}/partners/{partner_id}/access/disable', {
            headers: headers(token, idempotencyKey),
            params: {
              path: { event_id: eventId, partner_id: partnerId },
              header: { 'Idempotency-Key': idempotencyKey },
            },
          }),
    ),

  voidTicket: (token: string, idempotencyKey: string, ticketId: string, reason: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/tickets/{ticket_id}/void', {
        headers: headers(token, idempotencyKey),
        params: { path: { ticket_id: ticketId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: { reason },
      }),
    ),
  reissueTicket: (token: string, idempotencyKey: string, ticketId: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/tickets/{ticket_id}/credentials/reissue', {
        headers: headers(token, idempotencyKey),
        params: { path: { ticket_id: ticketId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: {},
      }),
    ),
  rereleaseTicket: (token: string, idempotencyKey: string, ticketId: string, reason: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/tickets/{ticket_id}/inventory/re-release', {
        headers: headers(token, idempotencyKey),
        params: { path: { ticket_id: ticketId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: { destination_kind: 'SHARED', reason },
      }),
    ),
  reverseAdmission: (token: string, idempotencyKey: string, admissionId: string, reason: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/admissions/{admission_id}/reverse', {
        headers: headers(token, idempotencyKey),
        params: {
          path: { admission_id: admissionId },
          header: { 'Idempotency-Key': idempotencyKey },
        },
        body: { reason },
      }),
    ),
  manualAdmission: (
    token: string,
    idempotencyKey: string,
    body: { event_id: string; ticket_id: string; gate_reference?: string; reason: string },
  ) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/admissions/manual-override', {
        headers: headers(token, idempotencyKey),
        params: { header: { 'Idempotency-Key': idempotencyKey } },
        body,
      }),
    ),

  createWebhook: (
    token: string,
    idempotencyKey: string,
    partnerId: string,
    url: string,
    subscriptions: string[],
  ) =>
    result<{
      id: string;
      partner_id: string;
      signing_secret: string;
      state: string;
    }>(
      client.POST('/api/v1/admin/partners/{partner_id}/webhook-endpoints', {
        headers: headers(token, idempotencyKey),
        params: { path: { partner_id: partnerId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: { url, subscriptions },
      }),
    ),
  disableWebhook: (token: string, idempotencyKey: string, endpointId: string, reason: string) =>
    result<Record<string, unknown>>(
      client.POST('/api/v1/admin/webhook-endpoints/{endpoint_id}/disable', {
        headers: headers(token, idempotencyKey),
        params: {
          path: { endpoint_id: endpointId },
          header: { 'Idempotency-Key': idempotencyKey },
        },
        body: { reason },
      }),
    ),
  rotateWebhook: (token: string, idempotencyKey: string, endpointId: string) =>
    result<{ endpoint_id: string; signing_secret: string; activated_at: string }>(
      client.POST('/api/v1/admin/webhook-endpoints/{endpoint_id}/secret/rotate', {
        headers: headers(token, idempotencyKey),
        params: {
          path: { endpoint_id: endpointId },
          header: { 'Idempotency-Key': idempotencyKey },
        },
        body: {},
      }),
    ),
};
