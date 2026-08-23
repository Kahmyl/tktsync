import { expect, test, type Page, type Route } from '@playwright/test';

const API_ORIGIN = 'http://localhost:48080';
const AUTH_ORIGIN = 'http://localhost:48081';

const ACCESS_TOKEN =
  'eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJvcGVyYXRvci0xIiwiYXVkIjoiYXV0aGVudGljYXRlZCIsImV4cCI6NDEwMjQ0NDgwMCwiZW1haWwiOiJvcGVyYXRvckBleGFtcGxlLmNvbSJ9.';

const corsHeaders = {
  'access-control-allow-origin': '*',
  'access-control-allow-methods': 'GET, POST, PATCH, PUT, DELETE, OPTIONS',
  'access-control-allow-headers':
    'authorization, apikey, content-type, idempotency-key, x-client-info, x-request-id, x-supabase-api-version, x-tktsync-reservation-token',
};

const operator = {
  id: 'operator-1',
  aud: 'authenticated',
  role: 'authenticated',
  email: 'operator@example.com',
  email_confirmed_at: '2026-08-23T12:00:00Z',
  confirmed_at: '2026-08-23T12:00:00Z',
  last_sign_in_at: '2026-08-23T12:00:00Z',
  app_metadata: {
    provider: 'email',
    providers: ['email'],
  },
  user_metadata: {},
  identities: [],
  created_at: '2026-08-23T12:00:00Z',
  updated_at: '2026-08-23T12:00:00Z',
};

async function json(route: Route, status: number, body: unknown) {
  await route.fulfill({
    status,
    headers: {
      ...corsHeaders,
      'content-type': 'application/json',
    },
    body: JSON.stringify(body),
  });
}

async function preflight(route: Route) {
  if (route.request().method() !== 'OPTIONS') {
    return false;
  }

  await route.fulfill({
    status: 204,
    headers: corsHeaders,
  });

  return true;
}

async function mockOperatorAuth(page: Page) {
  await page.route(`${AUTH_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;

    const request = route.request();
    const url = new URL(request.url());

    if (url.pathname === '/auth/v1/token' && request.method() === 'POST') {
      await json(route, 200, {
        access_token: ACCESS_TOKEN,
        token_type: 'bearer',
        expires_in: 3600,
        expires_at: 4102444800,
        refresh_token: 'refresh-token',
        user: operator,
      });
      return;
    }

    if (url.pathname === '/auth/v1/user') {
      await json(route, 200, operator);
      return;
    }

    if (url.pathname === '/auth/v1/logout') {
      await route.fulfill({
        status: 204,
        headers: corsHeaders,
      });
      return;
    }

    await json(route, 404, {
      message: `Unexpected auth request: ${url.pathname}`,
    });
  });
}

async function signIn(page: Page) {
  await page.getByLabel('Email', { exact: true }).fill('operator@example.com');
  await page.getByLabel('Password', { exact: true }).fill('correct-horse-battery-staple');
  await page.getByRole('button', { name: 'Sign in' }).click();
}

test('Admin authenticates and performs a structured venue workflow', async ({ page }) => {
  await mockOperatorAuth(page);

  let authorization = '';
  let body: unknown;

  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;

    const request = route.request();
    const url = new URL(request.url());

    if (request.method() === 'POST' && url.pathname === '/api/v1/admin/venues') {
      authorization = request.headers()['authorization'] ?? '';
      body = request.postDataJSON();

      await json(route, 201, {
        id: 'ven_test',
        name: 'E2E Arena',
        timezone_name: 'Africa/Lagos',
      });
      return;
    }

    await json(route, 404, {
      error: {
        message: `Unexpected API request: ${request.method()} ${url.pathname}`,
      },
    });
  });

  await page.goto('http://127.0.0.1:4173');

  await expect(page.getByRole('heading', { name: 'Operator sign in' })).toBeVisible();

  await signIn(page);

  await expect(page.getByRole('heading', { name: 'Venues' })).toBeVisible();

  await expect(page.getByText('Admin bearer')).toHaveCount(0);
  await expect(page.getByPlaceholder('Paste human bearer token')).toHaveCount(0);

  await page.getByLabel('Name', { exact: true }).fill('E2E Arena');

  await page.getByLabel('Timezone Name', { exact: true }).fill('Africa/Lagos');

  await page.getByRole('button', { name: 'Submit' }).click();

  await expect(page.locator('pre.response')).toContainText('ven_test');

  expect(authorization).toBe(`Bearer ${ACCESS_TOKEN}`);
  expect(body).toEqual({
    name: 'E2E Arena',
    timezone_name: 'Africa/Lagos',
  });
});

test('Selector consumes capability, renders reserved layout, and creates a hold', async ({
  page,
}) => {
  let selectionAuthorization = '';
  let reservationBody: unknown;

  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;

    const request = route.request();
    const url = new URL(request.url());

    selectionAuthorization = request.headers()['authorization'] ?? selectionAuthorization;

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/session') {
      await json(route, 200, {
        id: 'sel_1',
        event_id: 'evt_1',
        return_url: 'https://partner.example/checkout/return',
        expires_at: '2099-01-01T00:00:00Z',
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/event') {
      await json(route, 200, {
        name: 'Saturday Live',
        state: 'ON_SALE',
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/layout') {
      await json(route, 200, {
        event_id: 'evt_1',
        geometry: {},
        reserved_units: [
          {
            inventory_id: 'inv_a1',
            section_id: 'Floor',
            row: 'A',
            seat: '1',
            display_label: 'A1',
          },
          {
            inventory_id: 'inv_a2',
            section_id: 'Floor',
            row: 'A',
            seat: '2',
            display_label: 'A2',
          },
        ],
        ga_pools: [
          {
            inventory_id: 'inv_ga',
            section_id: 'Standing',
            name: 'Standing',
            capacity: 100,
          },
        ],
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/availability') {
      await json(route, 200, {
        reserved_units: [
          {
            inventory_id: 'inv_a1',
            section_id: 'Floor',
            row: 'A',
            seat: '1',
            sellability: 'AVAILABLE',
            offer: {
              offer_id: 'off_a1',
              available_quantity: 1,
              price: {
                amount_minor: 250000,
                currency: 'NGN',
              },
            },
          },
          {
            inventory_id: 'inv_a2',
            section_id: 'Floor',
            row: 'A',
            seat: '2',
            sellability: 'SOLD',
          },
        ],
        ga_pools: [
          {
            inventory_id: 'inv_ga',
            section_id: 'Standing',
            name: 'Standing',
            offers: [
              {
                offer_id: 'off_ga',
                available_quantity: 20,
                price: {
                  amount_minor: 150000,
                  currency: 'NGN',
                },
              },
            ],
          },
        ],
        server_time: '2026-08-23T14:00:00Z',
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/realtime/stream') {
      await route.fulfill({
        status: 200,
        headers: {
          ...corsHeaders,
          'content-type': 'text/event-stream',
          'cache-control': 'no-cache',
        },
        body: ': heartbeat\n\n',
      });
      return;
    }

    if (request.method() === 'POST' && url.pathname === '/api/v1/selection/reservations') {
      reservationBody = request.postDataJSON();

      await json(route, 201, {
        id: 'res_1',
        status: 'HELD',
        hold_expires_at: '2099-01-01T00:10:00Z',
        reservation_token: 'reservation-secret',
        items: [
          {
            id: 'item_1',
            inventory_id: 'inv_a1',
            quantity: 1,
          },
        ],
        total: {
          amount_minor: 250000,
          currency: 'NGN',
        },
        return_url: 'https://partner.example/checkout/return',
      });
      return;
    }

    await json(route, 404, {
      error: {
        message: `Unexpected API request: ${request.method()} ${url.pathname}`,
      },
    });
  });

  await page.goto('http://127.0.0.1:4174/#selection-capability-secret');

  await expect(page).toHaveURL('http://127.0.0.1:4174/');

  await expect(page.getByRole('heading', { name: 'Saturday Live' })).toBeVisible();

  await expect(page.getByRole('button', { name: 'A1' })).toBeEnabled();

  await expect(page.getByRole('button', { name: 'A2' })).toBeDisabled();

  await page.getByRole('button', { name: 'A1' }).click();

  await expect(page.getByText('Reserved seat', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Hold selection' }).click();

  await expect(page.getByText('Authoritative hold active')).toBeVisible();

  expect(selectionAuthorization).toBe('Bearer selection-capability-secret');

  expect(reservationBody).toEqual({
    items: [
      {
        offer_id: 'off_a1',
        quantity: 1,
      },
    ],
  });
});

test('Selector refetches authoritative state after realtime invalidation', async ({ page }) => {
  let eventReads = 0;
  let realtimeReads = 0;
  let realtimeAuthorization = '';

  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;

    const request = route.request();
    const url = new URL(request.url());

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/session') {
      await json(route, 200, {
        id: 'sel_realtime',
        event_id: 'evt_realtime',
        return_url: 'https://partner.example/checkout/return',
        expires_at: '2099-01-01T00:00:00Z',
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/event') {
      eventReads += 1;

      await json(route, 200, {
        name: 'Realtime Event',
        state: eventReads === 1 ? 'ON_SALE' : 'CANCELLED',
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/layout') {
      await json(route, 200, {
        event_id: 'evt_realtime',
        geometry: {},
        reserved_units: [],
        ga_pools: [],
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/availability') {
      await json(route, 200, {
        reserved_units: [],
        ga_pools: [],
        server_time: '2026-08-23T14:00:00Z',
      });
      return;
    }

    if (request.method() === 'GET' && url.pathname === '/api/v1/realtime/stream') {
      realtimeReads += 1;
      realtimeAuthorization = request.headers()['authorization'] ?? '';

      await route.fulfill({
        status: 200,
        headers: {
          ...corsHeaders,
          'content-type': 'text/event-stream',
          'cache-control': 'no-cache',
        },
        body:
          realtimeReads === 1
            ? 'event: invalidate\ndata: {"type":"event.changed"}\n\n'
            : ': heartbeat\n\n',
      });
      return;
    }

    await json(route, 404, {
      error: {
        message: `Unexpected API request: ${request.method()} ${url.pathname}`,
      },
    });
  });

  await page.goto('http://127.0.0.1:4174/#realtime-selection-secret');

  await expect(page.getByText('CANCELLED', { exact: true })).toBeVisible();

  expect(eventReads).toBeGreaterThanOrEqual(2);
  expect(realtimeAuthorization).toBe('Bearer realtime-selection-secret');
});

test('Scanner authenticates and distinguishes admission from duplicate admission', async ({
  page,
}) => {
  await mockOperatorAuth(page);

  let scanCount = 0;
  const authorizationHeaders: string[] = [];

  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;

    const request = route.request();
    const url = new URL(request.url());

    if (request.method() === 'POST' && url.pathname === '/api/v1/admission/scans') {
      scanCount += 1;
      authorizationHeaders.push(request.headers()['authorization'] ?? '');

      await json(route, 200, {
        result: scanCount === 1 ? 'ADMITTED' : 'TICKET_ALREADY_ADMITTED',
        admitted_at: '2026-08-23T14:00:00Z',
        ticket: {
          id: 'tkt_1',
          display: {
            section_name: 'Floor',
            row_label: 'A',
            seat_label: '1',
          },
        },
      });
      return;
    }

    await json(route, 404, {
      error: {
        message: `Unexpected API request: ${request.method()} ${url.pathname}`,
      },
    });
  });

  await page.goto('http://127.0.0.1:4175');

  await expect(page.getByRole('heading', { name: 'Scanner sign in' })).toBeVisible();

  await signIn(page);

  await expect(
    page.getByRole('heading', {
      name: 'Authoritative admission',
    }),
  ).toBeVisible();

  await expect(page.getByText('Scanner bearer')).toHaveCount(0);

  await page.getByLabel('Event ID', { exact: true }).fill('evt_1');

  await page.getByLabel('QR payload', { exact: true }).fill('qr1.ticket-one');

  await page.getByRole('button', { name: 'Validate ticket' }).click();

  await expect(page.locator('.result-stage p')).toHaveText('ADMITTED');

  await page.getByLabel('QR payload', { exact: true }).fill('qr1.ticket-one');

  await page.getByRole('button', { name: 'Validate ticket' }).click();

  await expect(page.locator('.result-stage p')).toHaveText('TICKET ALREADY ADMITTED');

  expect(scanCount).toBe(2);
  expect(authorizationHeaders).toEqual([`Bearer ${ACCESS_TOKEN}`, `Bearer ${ACCESS_TOKEN}`]);
});
