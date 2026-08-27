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
  app_metadata: { provider: 'email', providers: ['email'] },
  user_metadata: {},
  identities: [],
  created_at: '2026-08-23T12:00:00Z',
  updated_at: '2026-08-23T12:00:00Z',
};
const scannerEvent = {
  id: 'evt_championship',
  name: 'Championship Night',
  state: 'ON_SALE',
  starts_at: '2026-08-24T19:00:00Z',
  ends_at: '2026-08-24T23:00:00Z',
  timezone_name: 'Africa/Lagos',
  venue_name: 'Eko Convention Centre',
  address_text: 'Victoria Island, Lagos',
};

async function json(route: Route, status: number, body: unknown) {
  await route.fulfill({
    status,
    headers: { ...corsHeaders, 'content-type': 'application/json' },
    body: JSON.stringify(body),
  });
}

async function preflight(route: Route) {
  if (route.request().method() !== 'OPTIONS') return false;
  await route.fulfill({ status: 204, headers: corsHeaders });
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
      await route.fulfill({ status: 204, headers: corsHeaders });
      return;
    }
    await json(route, 404, { message: `Unexpected auth request: ${url.pathname}` });
  });
}

async function signIn(page: Page) {
  await page.getByLabel('Email', { exact: true }).fill('operator@example.com');
  await page.getByLabel('Password', { exact: true }).fill('correct-horse-battery-staple');
  await page.getByRole('button', { name: 'Sign in' }).click();
}

const baseLayout = {
  event_id: 'evt_1',
  geometry: {},
  reserved_units: [
    {
      inventory_id: 'inv_a1',
      section_id: 'sec_floor',
      section_name: 'VIP',
      row: 'A',
      seat: '1',
      display_label: 'A1',
    },
    {
      inventory_id: 'inv_a2',
      section_id: 'sec_floor',
      section_name: 'VIP',
      row: 'A',
      seat: '2',
      display_label: 'A2',
    },
    {
      inventory_id: 'inv_a3',
      section_id: 'sec_floor',
      section_name: 'VIP',
      row: 'A',
      seat: '3',
      display_label: 'A3',
    },
  ],
  ga_pools: [
    {
      inventory_id: 'inv_ga',
      section_id: 'sec_standing',
      section_name: 'North Terrace',
      name: 'Standing',
      capacity: 100,
    },
  ],
};

function availability(includeFirstSeat = true) {
  return {
    reserved_units: [
      {
        inventory_id: 'inv_a1',
        section_id: 'sec_floor',
        row: 'A',
        seat: '1',
        sellability: includeFirstSeat ? 'AVAILABLE' : 'HELD',
        ...(includeFirstSeat
          ? {
              offer: {
                offer_id: 'off_a1',
                available_quantity: 1,
                price: { amount_minor: 5_000_000, currency: 'NGN' },
              },
            }
          : {}),
      },
      {
        inventory_id: 'inv_a2',
        section_id: 'sec_floor',
        row: 'A',
        seat: '2',
        sellability: 'AVAILABLE',
        offer: {
          offer_id: 'off_a2',
          available_quantity: 1,
          price: { amount_minor: 5_000_000, currency: 'NGN' },
        },
      },
      { inventory_id: 'inv_a3', section_id: 'sec_floor', row: 'A', seat: '3', sellability: 'SOLD' },
    ],
    ga_pools: [
      {
        inventory_id: 'inv_ga',
        section_id: 'sec_standing',
        name: 'Standing',
        offers: [
          {
            offer_id: 'off_ga',
            available_quantity: 3,
            price: { amount_minor: 1_500_000, currency: 'NGN' },
          },
        ],
      },
    ],
    server_time: new Date().toISOString(),
  };
}

type SelectorMockOptions = {
  availabilityForRead?: (read: number) => ReturnType<typeof availability>;
  realtimeGate?: Promise<void>;
  holdExpiresAt?: string;
  layout?: Record<string, unknown>;
  reservationError?: { code: string; message: string; status: number };
};

async function mockSelector(page: Page, options: SelectorMockOptions = {}) {
  let availabilityReads = 0;
  let reservationBody: unknown;
  let releaseCalls = 0;
  let authorization = '';
  let realtimeReads = 0;
  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;
    const request = route.request();
    const url = new URL(request.url());
    authorization = request.headers()['authorization'] ?? authorization;
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
        name: 'Championship Night',
        state: 'ON_SALE',
        starts_at: '2026-08-24T19:00:00Z',
        venue_name: 'Eko Convention Centre',
      });
      return;
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/layout') {
      await json(route, 200, options.layout ?? baseLayout);
      return;
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/selection/availability') {
      availabilityReads += 1;
      await json(route, 200, options.availabilityForRead?.(availabilityReads) ?? availability());
      return;
    }
    if (request.method() === 'GET' && url.pathname === '/api/v1/realtime/stream') {
      realtimeReads += 1;
      if (realtimeReads === 1 && options.realtimeGate) {
        await options.realtimeGate;
        await route.fulfill({
          status: 200,
          headers: { ...corsHeaders, 'content-type': 'text/event-stream' },
          body: 'event: invalidate\ndata: {"type":"availability.changed"}\n\n',
        });
      } else {
        await route.fulfill({
          status: 200,
          headers: { ...corsHeaders, 'content-type': 'text/event-stream' },
          body: ': heartbeat\n\n',
        });
      }
      return;
    }
    if (request.method() === 'POST' && url.pathname === '/api/v1/selection/reservations') {
      reservationBody = request.postDataJSON();
      if (options.reservationError) {
        await json(route, options.reservationError.status, {
          error: {
            code: options.reservationError.code,
            message: options.reservationError.message,
            details: null,
            request_id: crypto.randomUUID(),
          },
        });
        return;
      }
      await json(route, 201, {
        id: 'res_1',
        status: 'HELD',
        hold_expires_at: options.holdExpiresAt ?? '2099-01-01T00:10:00Z',
        reservation_token: 'reservation-secret',
        items: [{ id: 'item_1', inventory_id: 'inv_a1', quantity: 1 }],
        total: { amount_minor: 14_500_000, currency: 'NGN' },
        return_url: 'https://partner.example/checkout/return',
      });
      return;
    }
    if (
      request.method() === 'POST' &&
      url.pathname === '/api/v1/selection/reservations/res_1/release'
    ) {
      releaseCalls += 1;
      await json(route, 200, { id: 'res_1', status: 'RELEASED' });
      return;
    }
    await json(route, 404, {
      error: { message: `Unexpected API request: ${request.method()} ${url.pathname}` },
    });
  });
  return {
    get reservationBody() {
      return reservationBody;
    },
    get releaseCalls() {
      return releaseCalls;
    },
    get authorization() {
      return authorization;
    },
    get availabilityReads() {
      return availabilityReads;
    },
  };
}

async function mockScannerAPI(
  page: Page,
  outcomes: Array<string | 'NETWORK'> = [],
  events = [scannerEvent],
) {
  let scans = 0;
  const authorizations: string[] = [];
  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;
    const request = route.request();
    const url = new URL(request.url());
    if (request.method() === 'GET' && url.pathname === '/api/v1/admission/events') {
      authorizations.push(request.headers()['authorization'] ?? '');
      await json(route, 200, { items: events });
      return;
    }
    if (request.method() === 'POST' && url.pathname === '/api/v1/admission/scans') {
      authorizations.push(request.headers()['authorization'] ?? '');
      const outcome = outcomes[scans++] ?? 'ADMITTED';
      if (outcome === 'NETWORK') {
        await json(route, 503, {
          error: { code: 'AUTHORITY_TEMPORARILY_UNAVAILABLE', message: 'unavailable' },
        });
        return;
      }
      await json(route, 200, {
        result: outcome,
        scan_attempt_id: `scan_${scans}`,
        admitted_at: outcome === 'ADMITTED' ? '2026-08-24T19:04:00Z' : undefined,
        previous_admission:
          outcome === 'TICKET_ALREADY_ADMITTED'
            ? { admitted_at: '2026-08-24T19:04:00Z', gate_reference: 'west' }
            : undefined,
        ticket: { id: 'tkt_1', display: { section: 'VIP', row: 'A', seat: '12' } },
      });
      return;
    }
    await json(route, 404, {
      error: { message: `Unexpected API request: ${request.method()} ${url.pathname}` },
    });
  });
  return {
    get scans() {
      return scans;
    },
    authorizations,
  };
}

async function identifyBrowserAsPhone(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'userAgentData', {
      configurable: true,
      value: { mobile: true },
    });
  });
}

async function openScanner(page: Page) {
  await page.goto('http://127.0.0.1:4175');
  await signIn(page);
  await expect(page.getByRole('heading', { name: 'Choose an event to scan' })).toBeVisible();
  await page.getByRole('button', { name: /Championship Night/ }).click();
  await expect(page.getByText('Point the camera at the ticket QR code')).toBeVisible();
}

async function manualScan(page: Page, code: string) {
  await page.getByRole('button', { name: 'Enter code manually' }).first().click();
  await page.getByLabel('Manual admission code').fill(code);
  await page.getByRole('button', { name: 'Check ticket' }).click();
}

test('Selector bare page fails closed without revealing Event data', async ({ page }) => {
  let apiRequests = 0;
  await page.route(`${API_ORIGIN}/**`, async (route) => {
    apiRequests += 1;
    await route.abort();
  });
  await page.goto('http://127.0.0.1:4174/');
  await expect(
    page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
  ).toBeVisible();
  await expect(page.getByText('Championship Night')).toHaveCount(0);
  expect(apiRequests).toBe(0);
});

test('Selector consumes its fragment, keeps unavailable seats visible, bounds GA, and holds real selections', async ({
  page,
}) => {
  const mocked = await mockSelector(page);
  await page.goto('http://127.0.0.1:4174/#selection-capability-secret');
  await expect(page).toHaveURL('http://127.0.0.1:4174/');
  await expect(page.getByRole('heading', { name: 'Championship Night' })).toBeVisible();
  const seat1 = page.getByRole('button', { name: /VIP, Row A · Seat 1/ });
  const seat2 = page.getByRole('button', { name: /VIP, Row A · Seat 2/ });
  const unavailableSeat = page.getByRole('button', { name: /VIP, Row A · Seat 3, unavailable/ });
  await expect(unavailableSeat).toBeVisible();
  await expect(unavailableSeat).toBeDisabled();
  await seat1.click();
  await seat2.click();
  const addGA = page.getByRole('button', { name: 'Add one Standing ticket' });
  await addGA.click();
  await addGA.click();
  await addGA.click();
  await expect(addGA).toBeDisabled();
  await page.getByRole('button', { name: 'Hold tickets' }).click();
  await expect(page.getByRole('heading', { name: 'Your tickets are held' })).toBeVisible();
  await expect(page.getByText('Time remaining')).toBeVisible();
  expect(mocked.authorization).toBe('Bearer selection-capability-secret');
  expect(mocked.reservationBody).toEqual({
    items: [
      { offer_id: 'off_a1', quantity: 1 },
      { offer_id: 'off_a2', quantity: 1 },
      { offer_id: 'off_ga', quantity: 3 },
    ],
  });
  await page.getByRole('button', { name: 'Change selection' }).click();
  await expect(page.getByText('Choose your tickets').first()).toBeVisible();
  expect(mocked.releaseCalls).toBe(1);
});

test('Selector preserves authored spatial geometry when a visual object intersects', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const reservedUnits = [
    ...['A', 'B', 'C'].flatMap((row) =>
      ['1', '2', '3', '4', '5'].map((seat) => ({
        inventory_id: `inv_reserved_${row}_${seat}`,
        section_id: 'sec_reserved',
        section_object_key: 'reserved-section',
        section_name: 'Reserved section',
        row,
        seat,
        display_label: `${row}${seat}`,
      })),
    ),
    ...['Table 1', 'Table 2', 'Table 3'].flatMap((table) =>
      ['1', '2', '3', '4'].map((seat) => ({
        inventory_id: `inv_${table}_${seat}`,
        section_id: 'sec_table',
        section_object_key: 'table-area',
        section_name: 'Table area',
        table,
        seat,
        display_label: `${table} Seat ${seat}`,
      })),
    ),
  ];
  await mockSelector(page, {
    layout: {
      event_id: 'evt_1',
      geometry: {
        objects: [
          {
            object_key: 'stage',
            type: 'STAGE',
            label: 'Stage',
            x: 360,
            y: 120,
            width: 300,
            height: 90,
          },
          {
            object_key: 'reserved-section',
            type: 'SECTION',
            label: 'Reserved section',
            x: 240,
            y: 150,
            width: 270,
            height: 160,
            rotation: 12,
          },
          {
            object_key: 'table-area',
            type: 'SECTION',
            label: 'Table area',
            x: 570,
            y: 200,
            width: 270,
            height: 160,
          },
          {
            object_key: 'ga-section',
            type: 'SECTION',
            label: 'General admission',
            x: 360,
            y: 400,
            width: 270,
            height: 160,
          },
        ],
      },
      reserved_units: reservedUnits,
      ga_pools: [
        {
          inventory_id: 'inv_ga',
          section_id: 'sec_ga',
          section_object_key: 'ga-section',
          section_name: 'General admission',
          name: 'General admission',
          capacity: 100,
        },
      ],
    },
  });
  await page.goto('http://127.0.0.1:4174/#spatial-card-capability');

  const cards = page.locator('.spatial-section, .spatial-ga');
  await expect(cards).toHaveCount(3);
  const reserved = page.locator('section.spatial-section[aria-label="Reserved section"]');
  await expect(reserved).toHaveCSS('position', 'absolute');
  expect(await reserved.getAttribute('style')).toContain('left: 264px');
  expect(await reserved.getAttribute('style')).toContain('top: 174px');
  expect(await reserved.getAttribute('style')).toContain('rotate(12deg)');
  await expect(page.locator('.spatial-orientation')).toHaveCSS('position', 'absolute');
  expect(
    await page
      .locator('.spatial-map-scroll')
      .evaluate((element) => element.scrollWidth > element.clientWidth),
  ).toBe(true);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true);
  const clipping = await cards.evaluateAll((elements) =>
    elements.map((element) => ({
      label: element.getAttribute('aria-label'),
      clippedVertically: element.scrollHeight > element.clientHeight,
      clippedHorizontally: element.scrollWidth > element.clientWidth,
    })),
  );
  expect(clipping).toEqual([
    { label: 'Reserved section', clippedVertically: false, clippedHorizontally: false },
    { label: 'Table area', clippedVertically: false, clippedHorizontally: false },
    { label: 'Standing', clippedVertically: false, clippedHorizontally: false },
  ]);
  await expect(page.getByRole('button', { name: /Table area, Table 3 · Seat 4/ })).toBeVisible();
});

test('Selector explains a future sales window instead of blaming availability', async ({
  page,
}) => {
  await mockSelector(page, {
    reservationError: {
      code: 'EVENT_NOT_ON_SALE',
      message: 'Event sales have not opened',
      status: 409,
    },
  });
  await page.goto('http://127.0.0.1:4174/#future-sales-capability');
  await page.getByRole('button', { name: /VIP, Row A · Seat 1/ }).click();
  await page.getByRole('button', { name: 'Hold tickets' }).click();
  await expect(
    page.getByText('Ticket sales have not opened yet. Please return when the sales window begins.'),
  ).toBeVisible();
  await expect(page.getByText(/review the latest availability/i)).toHaveCount(0);
});

test('Selector visibly counts down an active hold', async ({ page }) => {
  await mockSelector(page, { holdExpiresAt: new Date(Date.now() + 125_000).toISOString() });
  await page.goto('http://127.0.0.1:4174/#countdown-capability');
  await page.getByRole('button', { name: /VIP, Row A · Seat 1/ }).click();
  await page.getByRole('button', { name: 'Hold tickets' }).click();

  const timer = page.locator('.hold-timer strong');
  const firstValue = await timer.innerText();
  await expect(timer).not.toHaveText(firstValue, { timeout: 3_000 });
  expect(await timer.innerText()).toMatch(/^\d{2}:\d{2}$/);
});

test('Selector realtime removes a selected seat that becomes unavailable without fabricating success', async ({
  page,
}) => {
  let releaseRealtime = () => {};
  const realtimeGate = new Promise<void>((resolve) => {
    releaseRealtime = resolve;
  });
  const mocked = await mockSelector(page, {
    realtimeGate,
    availabilityForRead: (read) => availability(read === 1),
  });
  await page.goto('http://127.0.0.1:4174/#realtime-capability');
  await page.getByRole('button', { name: /VIP, Row A · Seat 1/ }).click();
  releaseRealtime();
  await expect(
    page.getByText('That seat is no longer available. Choose another seat to continue.'),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: /VIP, Row A · Seat 1, unavailable/ }),
  ).toBeDisabled();
  expect(mocked.availabilityReads).toBeGreaterThanOrEqual(2);
});

test('Selector mobile review sheet hands off by secure POST and never puts the reservation token in the URL', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockSelector(page);
  await page.route('https://partner.example/**', async (route) => {
    await route.fulfill({ status: 200, contentType: 'text/html', body: '<h1>Checkout</h1>' });
  });
  await page.goto('http://127.0.0.1:4174/#mobile-capability');
  await page.getByRole('button', { name: /VIP, Row A · Seat 1/ }).click();
  await page.getByRole('button', { name: 'Review' }).click();
  await expect(page.getByRole('dialog', { name: 'Your selection' })).toBeVisible();
  await page.getByRole('dialog').getByRole('button', { name: 'Hold tickets' }).click();
  const requestPromise = page.waitForRequest(
    (request) =>
      request.url().startsWith('https://partner.example/') && request.method() === 'POST',
  );
  await page.getByRole('button', { name: 'Continue to checkout' }).click();
  const handoff = await requestPromise;
  expect(handoff.postData()).toContain('reservation_token=reservation-secret');
  expect(handoff.url()).not.toContain('reservation-secret');
  expect(page.url()).not.toContain('reservation-secret');
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});

test('Selector shows buyer-friendly network and expired-hold states', async ({ page }) => {
  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;
    await route.abort();
  });
  await page.goto('http://127.0.0.1:4174/#network-capability');
  await expect(
    page.getByRole('heading', { name: "We couldn't refresh availability." }),
  ).toBeVisible();
  await page.unroute(`${API_ORIGIN}/**`);
  await mockSelector(page, { holdExpiresAt: '2020-01-01T00:00:00Z' });
  await page.goto('http://127.0.0.1:4174/?expired=1#expired-capability');
  await page.getByRole('button', { name: /VIP, Row A · Seat 1/ }).click();
  await page.getByRole('button', { name: 'Hold tickets' }).click();
  await expect(page.getByRole('heading', { name: 'Your reservation expired' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Choose tickets again' })).toBeVisible();
});

test('Scanner signs in with Supabase, selects an authorized Event, and signs out', async ({
  page,
}) => {
  await mockOperatorAuth(page);
  const eventUUID = '1c56ee8e-8c04-458a-957c-7ad81ad58342';
  const venueUUID = '9ccb9148-a76a-4e87-bf78-dd429f501bd3';
  const mocked = await mockScannerAPI(
    page,
    [],
    [
      {
        ...scannerEvent,
        name: `Reservation Event ${eventUUID}`,
        venue_name: `Reservation Venue ${venueUUID}`,
      },
      {
        ...scannerEvent,
        id: 'evt_second',
        name: 'Reservation Event 901e5f65-7fe4-4080-9e1d-53fc0a4d0843',
        venue_name: 'Reservation Venue 4ed775b9-0b5c-481a-8340-3709deb83aed',
      },
    ],
  );
  await page.goto('http://127.0.0.1:4175');
  await expect(page.getByRole('heading', { name: 'Scanner sign in' })).toBeVisible();
  await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Event ID')).toHaveCount(0);
  await signIn(page);
  await expect(page.getByRole('heading', { name: 'Your events' })).toBeVisible();
  await expect(page.getByText('2 events assigned to you')).toBeVisible();
  await expect(page.getByText(/Event reference/)).toHaveCount(2);
  await page
    .getByRole('button', { name: /Reservation Event/ })
    .first()
    .click();
  await expect(page.getByText('Reservation Venue').first()).toBeVisible();
  await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open camera' })).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText(eventUUID);
  await expect(page.locator('body')).not.toContainText(venueUUID);
  await page.getByRole('button', { name: 'Scanner settings' }).click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Scanner sign in' })).toBeVisible();
  expect(mocked.authorizations).toContain(`Bearer ${ACCESS_TOKEN}`);
});

test('Scanner camera and real manual path distinguish admit, duplicate, invalid, wrong Event, and failure', async ({
  page,
}) => {
  await identifyBrowserAsPhone(page);
  await page.addInitScript(() => {
    class TestBarcodeDetector {
      async detect() {
        return [];
      }
    }
    Object.defineProperty(window, 'BarcodeDetector', {
      configurable: true,
      value: TestBarcodeDetector,
    });
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: async () => document.createElement('canvas').captureStream() },
    });
  });
  await mockOperatorAuth(page);
  const mocked = await mockScannerAPI(page, [
    'ADMITTED',
    'TICKET_ALREADY_ADMITTED',
    'CREDENTIAL_REVOKED',
    'WRONG_EVENT',
    'NETWORK',
  ]);
  await openScanner(page);
  await page.getByRole('button', { name: 'Open camera' }).click();
  await expect(page.getByText('Hold the code steady inside the frame')).toBeVisible();

  await manualScan(page, 'ticket-valid');
  await expect(page.getByRole('heading', { name: 'Admit guest' })).toBeVisible();
  await expect(page.locator('.decision-overlay').getByText('VIP · Row A · Seat 12')).toBeVisible();
  await page.getByRole('button', { name: 'Scan next ticket' }).click();

  await manualScan(page, 'ticket-repeat');
  await expect(page.getByRole('heading', { name: 'Already checked in' })).toBeVisible();
  await page.getByRole('button', { name: 'Scan next ticket' }).click();

  await manualScan(page, 'ticket-revoked');
  await expect(page.getByRole('heading', { name: 'Ticket not valid' })).toBeVisible();
  await page.getByRole('button', { name: 'Scan next ticket' }).click();

  await manualScan(page, 'ticket-wrong-event');
  await expect(page.getByRole('heading', { name: 'Wrong event' })).toBeVisible();
  await expect(page.getByText('This ticket is not valid for Championship Night.')).toBeVisible();
  await page.getByRole('button', { name: 'Scan next ticket' }).click();

  await manualScan(page, 'ticket-network');
  await expect(page.getByRole('heading', { name: "Can't verify ticket" })).toBeVisible();
  await expect(
    page.getByText('Do not admit this guest until the ticket can be verified.', { exact: false }),
  ).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Admit guest' })).toHaveCount(0);
  expect(mocked.scans).toBe(5);
  expect(mocked.authorizations.filter((value) => value.startsWith('Bearer '))).toEqual(
    Array(6).fill(`Bearer ${ACCESS_TOKEN}`),
  );
});

test('Scanner camera denial remains usable and mobile layout has no body overflow', async ({
  page,
}) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await identifyBrowserAsPhone(page);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getUserMedia: async () => {
          throw new DOMException('denied', 'NotAllowedError');
        },
      },
    });
  });
  await mockOperatorAuth(page);
  await mockScannerAPI(page, ['TICKET_INVALID']);
  await openScanner(page);
  await page.getByRole('button', { name: 'Open camera' }).click();
  await expect(
    page.getByText('Camera access is off. Allow access or enter the manual admission code.'),
  ).toBeVisible();
  await manualScan(page, 'manual-after-denial');
  await expect(page.getByRole('heading', { name: 'Ticket not valid' })).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});

test('Scanner rejects a front camera and asks for a phone with a rear camera', async ({ page }) => {
  await identifyBrowserAsPhone(page);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: {
        getUserMedia: async () => ({
          getVideoTracks: () => [{ getSettings: () => ({ facingMode: 'user' }) }],
          getTracks: () => [{ stop: () => undefined }],
        }),
      },
    });
  });
  await mockOperatorAuth(page);
  await mockScannerAPI(page);
  await openScanner(page);
  await page.getByRole('button', { name: 'Open camera' }).click();
  await expect(page.getByText('Rear camera not found', { exact: true })).toBeVisible();
  await expect(
    page.getByText(
      'A rear camera was not found. Use a phone with a rear camera or enter the manual admission code.',
    ),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Enter code manually' }).first()).toBeVisible();
});
