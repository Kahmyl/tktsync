import { expect, test, type Page, type Route } from '@playwright/test';

const API_ORIGIN = 'http://localhost:48080';
const AUTH_ORIGIN = 'http://localhost:48081';
const ACCESS_TOKEN =
  'eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJhZG1pbi1vcGVyYXRvciIsImF1ZCI6ImF1dGhlbnRpY2F0ZWQiLCJleHAiOjQxMDI0NDQ4MDB9.';
const NOW = '2026-08-23T14:00:00Z';
const MACHINE_UUID = '7467aa88-7976-4b27-b578-8b3268dc42a4';

const corsHeaders = {
  'access-control-allow-origin': '*',
  'access-control-allow-methods': 'GET, POST, PATCH, PUT, DELETE, OPTIONS',
  'access-control-allow-headers':
    'authorization, apikey, content-type, idempotency-key, x-client-info, x-request-id, x-supabase-api-version',
};

const operator = {
  id: 'admin-operator',
  aud: 'authenticated',
  role: 'authenticated',
  email: 'amina@example.com',
  email_confirmed_at: NOW,
  confirmed_at: NOW,
  last_sign_in_at: NOW,
  app_metadata: { provider: 'email', providers: ['email'] },
  user_metadata: { full_name: 'Amina Okafor' },
  identities: [],
  created_at: NOW,
  updated_at: NOW,
};

async function json(route: Route, status: number, body: unknown) {
  await route.fulfill({
    status,
    headers: { ...corsHeaders, 'content-type': 'application/json', 'x-request-id': 'e2e-request' },
    body: JSON.stringify(body),
  });
}

async function preflight(route: Route) {
  if (route.request().method() !== 'OPTIONS') return false;
  await route.fulfill({ status: 204, headers: corsHeaders });
  return true;
}

async function mockOperatorAuth(page: Page, requiresPasswordSetup = false) {
  const mutations: Array<{ method: string; path: string; url: string; body?: unknown }> = [];
  const authOperator = requiresPasswordSetup
    ? {
        ...operator,
        user_metadata: { ...operator.user_metadata, tktsync_password_setup_required: true },
      }
    : operator;
  await page.route(`${AUTH_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (request.method() !== 'GET') {
      mutations.push({
        method: request.method(),
        path: pathname,
        url: request.url(),
        body: request.postData() ? request.postDataJSON() : undefined,
      });
    }
    if (pathname === '/auth/v1/token' && request.method() === 'POST') {
      await json(route, 200, {
        access_token: ACCESS_TOKEN,
        token_type: 'bearer',
        expires_in: 3600,
        expires_at: 4102444800,
        refresh_token: 'e2e-refresh-token',
        user: authOperator,
      });
      return;
    }
    if (pathname === '/auth/v1/user' && request.method() === 'GET') {
      await json(route, 200, authOperator);
      return;
    }
    if (pathname === '/auth/v1/user' && request.method() === 'PUT') {
      await json(route, 200, {
        ...authOperator,
        user_metadata: {
          ...authOperator.user_metadata,
          tktsync_password_setup_required: false,
        },
      });
      return;
    }
    if (pathname === '/auth/v1/recover' && request.method() === 'POST') {
      await json(route, 200, {});
      return;
    }
    if (pathname === '/auth/v1/logout') {
      await route.fulfill({ status: 204, headers: corsHeaders });
      return;
    }
    await json(route, 404, { message: `Unexpected auth request: ${pathname}` });
  });
  return { mutations };
}

async function signIn(page: Page) {
  await page.goto('http://127.0.0.1:4173');
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
  await page.getByLabel('Email', { exact: true }).fill('amina@example.com');
  await page.getByLabel('Password', { exact: true }).fill('e2e-password');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByText('Upcoming events')).toBeVisible();
}

type MutationRecord = {
  method: string;
  path: string;
  authorization: string;
  idempotencyKey: string;
  body?: unknown;
};

async function mockAdminApi(page: Page) {
  let eventId = 'evt_festival';
  let eventName = `Festival Night ${MACHINE_UUID}`;
  let eventState = 'DRAFT';
  let layoutState: 'DRAFT' | 'PUBLISHED' = 'DRAFT';
  let partnerName = `Seed Partner ${MACHINE_UUID}`;
  let partnerAccess: Array<Record<string, unknown>> = [];
  let credentials: Array<Record<string, unknown>> = [];
  let administrators: Array<Record<string, unknown>> = [
    {
      id: 'usr_current',
      email: 'amina@example.com',
      display_name: 'Amina Okafor',
      state: 'ACTIVE',
      role: 'PLATFORM_ADMIN',
      is_current_user: true,
      created_at: NOW,
      updated_at: NOW,
    },
  ];
  const mutations: MutationRecord[] = [];

  const venue = {
    id: 'ven_civic',
    name: `Civic Arena ${MACHINE_UUID}`,
    address_text: '12 Marina Road, Lagos',
    created_at: NOW,
    updated_at: NOW,
  };
  const event = () => ({
    id: eventId,
    venue_id: venue.id,
    venue_name: venue.name,
    name: eventName,
    state: eventState,
    starts_at: '2026-09-10T18:00:00Z',
    ends_at: '2026-09-10T22:00:00Z',
    sales_open_at: '2026-08-24T08:00:00Z',
    sales_close_at: '2026-09-10T18:00:00Z',
    admission_open_at: '2026-09-10T16:00:00Z',
    admission_close_at: '2026-09-10T22:00:00Z',
    timezone_name: 'Africa/Lagos',
    capacity: 500,
    sold: 284,
    created_at: NOW,
    updated_at: NOW,
  });
  const partnerSummary = () => ({
    id: 'ptr_partner',
    name: partnerName,
    state: 'ACTIVE',
    active_event_count: partnerAccess.length,
    active_credential_count: credentials.length,
    active_endpoint_count: 0,
    last_activity_at: NOW,
    created_at: NOW,
    disabled_at: null,
  });
  const emptyDimensions = {
    capacity: 500,
    available: 216,
    held: 4,
    committing: 0,
    payment_retry: 0,
    reconciling: 0,
    blocked: 0,
    allocated: 0,
    sold_current: 280,
    issued_current: 0,
    voided_tickets: 0,
    capacity_consumed: 284,
    historical_sold: 284,
    historical_issued: 0,
  };

  await page.route(`${API_ORIGIN}/**`, async (route) => {
    if (await preflight(route)) return;
    const request = route.request();
    const method = request.method();
    const path = new URL(request.url()).pathname;
    const authorization = request.headers().authorization ?? '';
    if (authorization !== `Bearer ${ACCESS_TOKEN}`) {
      await json(route, 401, { error: { message: 'Missing Admin authorization' } });
      return;
    }

    if (method !== 'GET') {
      mutations.push({
        method,
        path,
        authorization,
        idempotencyKey: request.headers()['idempotency-key'] ?? '',
        body: request.postData() ? request.postDataJSON() : undefined,
      });
    }

    if (method === 'GET' && path === '/api/v1/admin/dashboard') {
      await json(route, 200, {
        metrics: {
          active_events: 2,
          tickets_sold: 284,
          reservations_today: 18,
          checkins_today: 42,
        },
        upcoming_events: [event()],
        attention: [
          {
            event_id: eventId,
            event_name: eventName,
            state: eventState,
            layout_ready: true,
            pricing_ready: true,
            policy_ready: true,
          },
        ],
        recent_activity: [
          {
            operation: 'ADMIN_CREATE_EVENT',
            entity_type: 'EVENT',
            occurred_at: NOW,
            event_name: eventName,
          },
        ],
      });
      return;
    }
    if (method === 'GET' && path === '/api/v1/admin/users') {
      await json(route, 200, {
        items: administrators,
        total: administrators.length,
        limit: 100,
        offset: 0,
      });
      return;
    }
    if (method === 'POST' && path === '/api/v1/admin/users') {
      const body = request.postDataJSON() as { email: string; display_name: string };
      const administrator = {
        id: 'usr_invited',
        email: body.email,
        display_name: body.display_name,
        state: 'ACTIVE',
        role: 'PLATFORM_ADMIN',
        is_current_user: false,
        invitation_sent: true,
        created_at: NOW,
        updated_at: NOW,
      };
      administrators = [administrator, ...administrators];
      await json(route, 201, administrator);
      return;
    }
    if (method === 'GET' && path === '/api/v1/admin/events') {
      await json(route, 200, { items: [event()], total: 1, limit: 100, offset: 0 });
      return;
    }
    if (method === 'POST' && path === '/api/v1/admin/events') {
      const body = request.postDataJSON() as { name: string };
      eventId = 'evt_created';
      eventName = body.name;
      eventState = 'DRAFT';
      await json(route, 201, { id: eventId });
      return;
    }
    if (method === 'GET' && /^\/api\/v1\/admin\/events\/[^/]+$/.test(path)) {
      const { venue_name: _venueName, capacity: _capacity, sold: _sold, ...detail } = event();
      await json(route, 200, detail);
      return;
    }
    if (method === 'GET' && path.endsWith('/configuration')) {
      await json(route, 200, {
        layout: { id: 'lay_materialized', version_number: 1, finalized_at: NOW },
        price_tiers: [
          {
            id: 'price_standard',
            code: 'STD',
            name: 'Standard',
            amount_minor: 250000,
            currency: 'NGN',
            state: 'ACTIVE',
            created_at: NOW,
            updated_at: NOW,
          },
        ],
        partner_access: [
          {
            partner_id: 'ptr_partner',
            partner_name: partnerName,
            partner_state: 'ACTIVE',
            access_state: 'ACTIVE',
            created_at: NOW,
            disabled_at: null,
          },
        ],
        transaction_policy: { hold_duration_seconds: 600 },
      });
      return;
    }
    if (method === 'GET' && /\/events\/[^/]+\/inventory$/.test(path)) {
      await json(route, 200, {
        event_id: eventId,
        inventory: [
          {
            kind: 'GA',
            id: 'ga_floor',
            snapshot_object_key: 'floor',
            label: 'Main floor',
            quantity: 500,
            price_tier_id: 'price_standard',
          },
        ],
      });
      return;
    }
    if (method === 'GET' && path.endsWith('/reports/inventory')) {
      await json(route, 200, {
        generated_at: NOW,
        event: event(),
        total: emptyDimensions,
        reserved_seating: { ...emptyDimensions, capacity: 0 },
        general_admission: emptyDimensions,
      });
      return;
    }
    if (method === 'GET' && path.endsWith('/reports/sales')) {
      await json(route, 200, {
        generated_at: NOW,
        event: event(),
        sale_count: 120,
        historical_sale_quantity: 284,
        historical_amount_minor: 71000000,
        active_sold_tickets: 284,
        voided_sold_tickets: 0,
        current_sold_capacity: 284,
        currency: 'NGN',
      });
      return;
    }
    if (method === 'GET' && path.endsWith('/reports/admission')) {
      await json(route, 200, {
        generated_at: NOW,
        event: event(),
        active_admissions: 42,
        reversed_admissions: 1,
        scan_outcomes: { ADMITTED: 42, ALREADY_ADMITTED: 3 },
      });
      return;
    }
    if (method === 'GET' && path.endsWith('/audit')) {
      await json(route, 200, {
        items: [{ operation: 'ADMIN_CREATE_EVENT', occurred_at: NOW }],
        next_cursor: null,
      });
      return;
    }
    if (
      method === 'POST' &&
      /\/events\/[^/]+\/(open-sales|pause-sales|resume-sales|close-sales|complete)$/.test(path)
    ) {
      const action = path.split('/').at(-1);
      eventState =
        action === 'open-sales' || action === 'resume-sales'
          ? 'ON_SALE'
          : action === 'pause-sales'
            ? 'PAUSED'
            : action === 'close-sales'
              ? 'SALES_CLOSED'
              : 'COMPLETED';
      await json(route, 200, { event_id: eventId, state: eventState });
      return;
    }
    if (method === 'POST' && path.endsWith('/partners/ptr_partner/access')) {
      partnerAccess = [
        {
          event_id: eventId,
          event_name: eventName,
          event_state: eventState,
          state: 'ACTIVE',
          created_at: NOW,
          disabled_at: null,
        },
      ];
      await json(route, 200, { state: 'ACTIVE' });
      return;
    }

    if (method === 'GET' && path === '/api/v1/admin/venues') {
      await json(route, 200, { venues: [venue] });
      return;
    }
    if (method === 'GET' && path === `/api/v1/admin/venues/${venue.id}`) {
      await json(route, 200, venue);
      return;
    }
    if (method === 'GET' && path === `/api/v1/admin/venues/${venue.id}/layout-versions`) {
      await json(route, 200, {
        layout_versions: [
          {
            id: 'lay_draft',
            version_number: 1,
            state: layoutState,
            published_at: layoutState === 'PUBLISHED' ? NOW : null,
            retired_at: null,
            created_at: NOW,
          },
        ],
      });
      return;
    }
    if (method === 'PATCH' && path === '/api/v1/admin/venue-layouts/lay_draft') {
      await json(route, 200, { id: 'lay_draft' });
      return;
    }
    if (method === 'POST' && path === '/api/v1/admin/venue-layouts/lay_draft/publish') {
      layoutState = 'PUBLISHED';
      await json(route, 200, { id: 'lay_draft', state: layoutState });
      return;
    }

    if (method === 'GET' && path === '/api/v1/admin/partners') {
      await json(route, 200, { items: [partnerSummary()], total: 1, limit: 100, offset: 0 });
      return;
    }
    if (method === 'POST' && path === '/api/v1/admin/partners') {
      partnerName = (request.postDataJSON() as { name: string }).name;
      partnerAccess = [];
      credentials = [];
      await json(route, 201, { id: 'ptr_partner', state: 'ACTIVE' });
      return;
    }
    if (method === 'GET' && path === '/api/v1/admin/partners/ptr_partner') {
      await json(route, 200, {
        ...partnerSummary(),
        credentials,
        event_access: partnerAccess,
        activity: [],
      });
      return;
    }
    if (method === 'POST' && path === '/api/v1/admin/partners/ptr_partner/credentials') {
      credentials = [
        {
          id: 'pcred_new',
          key_id: 'partner-key',
          state: 'ACTIVE',
          created_at: NOW,
          last_used_at: null,
          revoked_at: null,
        },
      ];
      await json(route, 201, {
        id: 'pcred_new',
        partner_id: 'ptr_partner',
        credential: 'tkp_one_time_e2e_secret',
      });
      return;
    }

    if (method === 'GET' && path === '/api/v1/admin/tickets') {
      await json(route, 200, {
        items: [
          {
            id: 'tkt_e2e',
            event_id: eventId,
            event_name: eventName,
            status: 'ACTIVE',
            inventory_kind: 'GA',
            attendee_name: 'Lola Adeyemi',
            display_label: 'Main floor',
            credential_id: 'cred_e2e',
            credential_state: 'ACTIVE',
            admission_state: null,
            admitted_at: null,
            created_at: NOW,
            voided_at: null,
            void_reason: null,
          },
        ],
        total: 1,
        limit: 100,
        offset: 0,
      });
      return;
    }
    if (method === 'POST' && path === '/api/v1/admin/tickets/tkt_e2e/credentials/reissue') {
      await json(route, 200, { ticket_id: 'tkt_e2e', credential_id: 'cred_reissued' });
      return;
    }
    if (method === 'GET' && path === '/api/v1/admin/admissions') {
      await json(route, 200, { items: [], total: 0, limit: 100, offset: 0 });
      return;
    }
    if (method === 'GET' && path === '/api/v1/admin/webhook-endpoints') {
      await json(route, 200, { items: [], total: 0, limit: 100, offset: 0 });
      return;
    }

    await json(route, 404, { error: { message: `Unexpected API request: ${method} ${path}` } });
  });

  return { mutations };
}

test('Admin signs in, reviews authoritative operations, creates an event, and changes lifecycle', async ({
  page,
}) => {
  await mockOperatorAuth(page);
  const state = await mockAdminApi(page);
  await signIn(page);

  const sidebarSignOut = page.locator('.sidebar > .sidebar-signout');
  await expect(sidebarSignOut.getByRole('button', { name: 'Sign Out' })).toBeVisible();
  await expect(sidebarSignOut.getByRole('button', { name: 'Sign Out' })).toHaveCSS(
    'color',
    'rgb(220, 20, 60)',
  );
  await expect(sidebarSignOut).not.toContainText(operator.email);
  await expect(sidebarSignOut.locator('.avatar')).toHaveCount(0);

  await expect(page.getByText('Festival Night').first()).toBeVisible();
  await expect(page.locator('body')).not.toContainText(MACHINE_UUID);
  await expect(page.getByText('284').first()).toBeVisible();

  await page.getByRole('link', { name: 'Events', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Events' })).toBeVisible();
  await expect(page.getByRole('link', { name: /Festival Night/ })).toBeVisible();

  await page
    .getByRole('link', { name: /Create event/ })
    .first()
    .click();
  await page.getByLabel('Event name').fill('Harbour Lights');
  await page.getByLabel('Venue').selectOption('ven_civic');
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByLabel('Starts').fill('2026-09-12T18:00');
  await page.getByRole('button', { name: 'Continue' }).click();
  await page.getByRole('button', { name: 'Create draft event' }).click({ noWaitAfter: true });

  await expect(page.getByRole('heading', { name: 'Harbour Lights' })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Pricing' })).toBeVisible();
  await page.getByRole('button', { name: 'Open sales' }).click();
  await expect(page.getByText('On sale', { exact: true }).first()).toBeVisible();

  expect(state.mutations.map((entry) => entry.path)).toEqual(
    expect.arrayContaining(['/api/v1/admin/events', '/api/v1/admin/events/evt_created/open-sales']),
  );
  expect(state.mutations.every((entry) => entry.authorization === `Bearer ${ACCESS_TOKEN}`)).toBe(
    true,
  );
  expect(state.mutations.every((entry) => entry.idempotencyKey.length > 0)).toBe(true);
});

test('Admin edits a venue, creates a partner, reveals one credential once, grants access, and reissues a ticket', async ({
  page,
}) => {
  await mockOperatorAuth(page);
  const state = await mockAdminApi(page);
  await signIn(page);

  await page.getByRole('link', { name: 'Venues', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Venues' })).toBeVisible();
  await page
    .getByRole('link', { name: /Civic Arena/ })
    .first()
    .click();
  await page.getByRole('button', { name: 'Edit draft' }).click();
  await page.getByLabel('Section name').fill('Main Floor');
  await page.getByRole('button', { name: 'Add section' }).click();
  await page.getByRole('button', { name: 'Save draft layout' }).click();
  await page.getByRole('button', { name: 'Publish' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Publish layout' }).click();
  await expect(page.locator('tbody').getByText('Published', { exact: true })).toBeVisible();

  await page.getByRole('link', { name: 'Partners', exact: true }).first().click();
  await page.getByRole('button', { name: 'Add partner' }).first().click();
  await page.getByLabel('Partner name').fill('Metro Tickets');
  await page.getByRole('dialog').getByRole('button', { name: 'Add partner' }).click();
  await page.getByRole('link', { name: /Metro Tickets/ }).click();

  await page.getByRole('button', { name: 'Issue credential' }).first().click();
  await expect(page.getByRole('dialog', { name: 'Partner credential created' })).toBeVisible();
  await expect(page.getByTestId('one-time-secret')).toHaveText('tkp_one_time_e2e_secret');
  await page.getByRole('button', { name: 'I have stored it' }).click();
  await expect(page.getByTestId('one-time-secret')).toHaveCount(0);

  await page.getByRole('tab', { name: 'Event access' }).click();
  await page.getByLabel('Event to grant').selectOption('evt_festival');
  await page.getByRole('button', { name: 'Grant access' }).click();
  await expect(page.getByText('Enabled', { exact: true })).toBeVisible();

  await page.getByRole('link', { name: 'Tickets', exact: true }).click();
  await expect(page.locator('body')).not.toContainText('tkt_e2e');
  await page.locator('.desktop-table tbody tr').first().click();
  await page.getByRole('button', { name: 'Reissue credential' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Reissue credential' }).click();
  await expect(page.getByRole('dialog', { name: 'Ticket detail' })).toHaveCount(0);

  expect(state.mutations.map((entry) => entry.path)).toEqual(
    expect.arrayContaining([
      '/api/v1/admin/venue-layouts/lay_draft',
      '/api/v1/admin/venue-layouts/lay_draft/publish',
      '/api/v1/admin/partners',
      '/api/v1/admin/partners/ptr_partner/credentials',
      '/api/v1/admin/events/evt_festival/partners/ptr_partner/access',
      '/api/v1/admin/tickets/tkt_e2e/credentials/reissue',
    ]),
  );
  expect(state.mutations.every((entry) => entry.idempotencyKey.length > 0)).toBe(true);
});

test('Admin account popover uses session identity and signs out', async ({ page }) => {
  await mockOperatorAuth(page);
  await mockAdminApi(page);
  await signIn(page);

  await page.getByRole('button', { name: 'Account menu' }).click();
  await expect(page.getByText('Amina Okafor', { exact: true }).last()).toBeVisible();
  await expect(page.getByText('amina@example.com', { exact: true }).last()).toBeVisible();
  await page.getByRole('button', { name: 'Log out' }).click();
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
});

test('Platform Admin invites another administrator with a name and email', async ({ page }) => {
  await mockOperatorAuth(page);
  const state = await mockAdminApi(page);
  await signIn(page);

  await page.getByRole('link', { name: 'Administrators' }).click();
  await expect(page.getByRole('heading', { name: 'Administrators' })).toBeVisible();
  await page.getByRole('button', { name: 'Add administrator' }).first().click();
  await page.getByLabel('Display name').fill('Ada Okafor');
  await page.getByLabel('Work email').fill('ada@example.com');
  await page.getByRole('dialog').getByRole('button', { name: 'Invite administrator' }).click();

  await expect(page.getByText('Invitation sent to ada@example.com')).toBeVisible();
  await expect(page.getByText('Ada Okafor', { exact: true })).toBeVisible();
  await expect(page.getByText('ada@example.com', { exact: true })).toBeVisible();
  const mutation = state.mutations.find((entry) => entry.path === '/api/v1/admin/users');
  expect(mutation?.body).toEqual({ email: 'ada@example.com', display_name: 'Ada Okafor' });
  expect(mutation?.idempotencyKey).toBeTruthy();
});

test('Invited administrator creates a reusable password before continuing', async ({ page }) => {
  const authState = await mockOperatorAuth(page, true);
  await mockAdminApi(page);
  await page.goto('http://127.0.0.1:4173/sign-in');
  await page.getByLabel('Email', { exact: true }).fill('amina@example.com');
  await page.getByLabel('Password', { exact: true }).fill('temporary-invite-session');
  await page.getByRole('button', { name: 'Sign in' }).click();

  await expect(page.getByRole('heading', { name: 'Create your password' })).toBeVisible();
  await expect(page).toHaveURL(/\/set-password$/);
  await page.getByLabel('New password').fill('correct horse battery staple');
  await page.getByLabel('Confirm password').fill('correct horse battery staple');
  await page.getByRole('button', { name: 'Save password' }).click();

  await expect(page.getByRole('heading', { name: 'Password created' })).toBeVisible();
  expect(
    authState.mutations.find((entry) => entry.method === 'PUT' && entry.path === '/auth/v1/user')
      ?.body,
  ).toMatchObject({
    password: 'correct horse battery staple',
    data: { tktsync_password_setup_required: false },
  });
});

test('Signed-out administrator can request a password recovery link', async ({ page }) => {
  const authState = await mockOperatorAuth(page);
  await page.goto('http://127.0.0.1:4173/sign-in');
  await page.getByRole('link', { name: 'Forgot password?' }).click();
  await expect(page.getByRole('heading', { name: 'Reset your password' })).toBeVisible();
  await expect(page).toHaveURL(/\/forgot-password$/);
  await page.getByLabel('Email').fill('amina@example.com');
  await page.getByRole('button', { name: 'Send reset link' }).click();

  await expect(page.getByText(/password-reset link has been sent/)).toBeVisible();
  const recovery = authState.mutations.find((entry) => entry.path === '/auth/v1/recover');
  expect(recovery?.body).toMatchObject({ email: 'amina@example.com' });
  expect(new URL(recovery?.url ?? '').searchParams.get('redirect_to')).toBe(
    'http://127.0.0.1:4173/set-password',
  );
});

test('Admin mobile drawer provides usable navigation', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockOperatorAuth(page);
  await mockAdminApi(page);
  await signIn(page);

  await page.getByRole('button', { name: 'Open navigation' }).click();
  const drawer = page.locator('.mobile-drawer');
  await expect(drawer).toHaveClass(/open/);
  await drawer.getByRole('link', { name: 'Events', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Events' })).toBeVisible();
  await expect(drawer).not.toHaveClass(/open/);
});
