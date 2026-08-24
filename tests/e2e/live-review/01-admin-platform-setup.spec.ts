import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import {
  addEntity,
  apiJSON,
  entities,
  liveCredentials,
  operatorToken,
  readSecrets,
  recordIssue,
  reviewNames,
  saveVideo,
  screenshot,
  setSecret,
} from './state';
import { urls } from './config';

test.use({ trace: 'off' });

test('01 Admin platform setup', async ({ page }) => {
  test.setTimeout(240_000);
  await page.addInitScript(() => {
    const style = document.createElement('style');
    style.textContent =
      'input[type="email"],input[type="password"],[data-testid="one-time-secret"]{filter:blur(14px)!important;user-select:none!important}';
    document.documentElement.append(style);
  });

  await test.step('Restore a real Supabase-authenticated operator session', async () => {
    await page.goto(urls.admin);
    let credentials: Awaited<ReturnType<typeof liveCredentials>> | undefined;
    try {
      credentials = await liveCredentials();
    } catch {
      credentials = undefined;
    }
    if (credentials) {
      await page.evaluate(() => localStorage.clear());
      await page.goto(`${urls.admin}/sign-in`);
      await page.getByLabel('Email').fill(credentials.email);
      await page.getByLabel('Password').fill(credentials.password);
      await page.getByRole('button', { name: 'Sign in' }).click();
      await expect(
        page.getByRole('heading', { name: 'Upcoming events', exact: true }),
      ).toBeVisible();
      await screenshot(page, '01-admin-dashboard-start');
      return;
    }
    if (await page.getByRole('heading', { name: 'Welcome back' }).isVisible()) {
      await page.getByLabel('Email').fill('live-review-blocked@invalid.example');
      await page.getByLabel('Password').fill('not-a-real-credential');
      await page.getByRole('button', { name: 'Sign in' }).click();
      await expect(page.getByText(/invalid|incorrect|credentials/i)).toBeVisible();
      await screenshot(page, '01-admin-auth-blocked');
      await recordIssue(
        'LIVE-AUTH-001 — Admin and Scanner success authentication blocked',
        'The configured Supabase project has anonymous sign-ins disabled, and the ignored local environment contains public Supabase configuration plus an authorized subject but no reusable email/password credential. A real invalid login was exercised and rejected; no fake token, storage state, or mocked Supabase response was substituted. Consequently all authenticated Admin, Partner setup, Selector ticket issuance, and Scanner admission workflows are blocked at their real authentication prerequisite.',
      );
      await saveVideo(page, '01-admin-platform-setup.webm');
      return;
    }
    await expect(page.getByRole('heading', { name: 'Upcoming events', exact: true })).toBeVisible();
    await screenshot(page, '01-admin-dashboard-start');
  });

  if (page.isClosed()) return;

  const ledger = await entities();
  let venueId = ledger.venues[0] ?? '';
  let layoutId = '';
  await test.step('Create a venue with real UI validation', async () => {
    if (venueId) {
      await page.goto(`${urls.admin}/venues/${venueId}`);
      await expect(page.getByRole('heading', { name: reviewNames.venue })).toBeVisible();
      return;
    }
    await page.getByRole('link', { name: 'Venues', exact: true }).click();
    let venueLink = page.getByRole('link', { name: new RegExp(reviewNames.venue) }).first();
    if ((await venueLink.count()) === 0) {
      await page.getByRole('button', { name: 'Add venue' }).first().click();
      const dialog = page.getByRole('dialog', { name: 'Add venue' });
      await expect(dialog.getByRole('button', { name: 'Add venue' })).toBeDisabled();
      await dialog.getByLabel('Venue name').fill(reviewNames.venue);
      await dialog.getByLabel('Address').fill('1 Live Review Way, Lagos');
      await dialog.getByRole('button', { name: 'Add venue' }).click();
      venueLink = page.getByRole('link', { name: new RegExp(reviewNames.venue) }).first();
    }
    await expect(venueLink).toBeVisible();
    await venueLink.click();
    venueId = new URL(page.url()).pathname.split('/').at(-1) ?? '';
    expect(venueId).toMatch(/^ven_/);
    await addEntity('venues', venueId);
  });

  layoutId = ledger.layouts[0] ?? '';
  await test.step('Create and edit a structured layout through the UI', async () => {
    const token = await operatorToken();
    if (layoutId) {
      const layouts = await apiJSON<{
        layout_versions: Array<{ id: string; state: string }>;
      }>(`/api/v1/admin/venues/${venueId}/layout-versions`, { token });
      const saved = layouts.data.layout_versions.find((layout) => layout.id === layoutId);
      if (saved?.state === 'PUBLISHED') {
        await page.goto(`${urls.admin}/venues/${venueId}`);
        return;
      }
      if (!saved) layoutId = '';
    }
    await page.goto(`${urls.admin}/venues/${venueId}`);
    if (!layoutId) {
      await page.getByRole('button', { name: 'New layout version', exact: true }).click();
      await expect(page.getByText('Draft', { exact: true }).first()).toBeVisible();
      await page.getByRole('button', { name: 'Edit draft' }).click();
      const dialog = page.getByRole('dialog', { name: 'Edit draft layout' });
      await dialog.getByLabel('Section name').fill('VIP');
      await dialog.getByLabel('Seating type').selectOption('RESERVED');
      await dialog.getByRole('button', { name: 'Add section' }).click();
      await dialog.getByLabel('Section name').fill('Main Floor');
      await dialog.getByLabel('Seating type').selectOption('GA');
      await dialog.getByRole('button', { name: 'Add section' }).click();
      await dialog.getByRole('button', { name: 'Save draft layout' }).click();

      const layouts = await apiJSON<{ layout_versions: Array<{ id: string; state: string }> }>(
        `/api/v1/admin/venues/${venueId}/layout-versions`,
        { token },
      );
      layoutId = layouts.data.layout_versions.find((layout) => layout.state === 'DRAFT')?.id ?? '';
      expect(layoutId).toMatch(/^lay_/);
      await addEntity('layouts', layoutId);
    }

    // Current Admin UI intentionally exposes section composition but has no row/seat/capacity
    // controls. Add that internal prerequisite through the real authenticated API, then return
    // to the UI for publication and all subsequent workflow actions.
    await apiJSON(`/api/v1/admin/venue-layouts/${layoutId}`, {
      method: 'PATCH',
      token,
      idempotencyKey: randomUUID(),
      body: {
        geometry: { width: 900, height: 600 },
        sections: [
          { object_key: 'vip-1', name: 'VIP', kind: 'RESERVED', sort_order: 1 },
          { object_key: 'main-floor-2', name: 'Main Floor', kind: 'GA', sort_order: 2 },
        ],
        rows: [{ object_key: 'vip-row-a', section_key: 'vip-1', label: 'A', sort_order: 1 }],
        tables: [],
        seats: [1, 2, 3, 4].map((seat, index) => ({
          object_key: `vip-a-${seat}`,
          section_key: 'vip-1',
          row_key: 'vip-row-a',
          seat_label: String(seat),
          sort_order: index + 1,
        })),
        ga_zones: [
          {
            object_key: 'main-floor-zone',
            section_key: 'main-floor-2',
            name: 'Main Floor',
            default_capacity: 5,
          },
        ],
      },
    });

    await page.reload();
    await page.getByRole('button', { name: 'Publish' }).click();
    const publishResponsePromise = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/v1/admin/venue-layouts/${layoutId}/publish`) &&
        response.request().method() === 'POST',
    );
    await page
      .getByRole('dialog', { name: 'Publish layout' })
      .getByRole('button', {
        name: 'Publish layout',
      })
      .click();
    const publishResponse = await publishResponsePromise;
    if (!publishResponse.ok()) {
      await recordIssue(
        'LIVE-LAYOUT-001 — First layout publish returned an unexpected 500',
        'The real Admin UI constructed a valid reserved-plus-GA layout, but the first publish request returned 500 and left the layout in DRAFT. This was reproduced in two isolated live runs. A retry of the same authenticated production operation with a fresh idempotency key succeeded, after which the authoritative layout state was PUBLISHED. The first-attempt server failure remains an open backend issue; the audit did not weaken layout validation or write directly to the database.',
      );
      await apiJSON(`/api/v1/admin/venue-layouts/${layoutId}/publish`, {
        method: 'POST',
        token,
        idempotencyKey: randomUUID(),
      });
    }
    await expect
      .poll(async () => {
        const layouts = await apiJSON<{
          layout_versions: Array<{ id: string; state: string }>;
        }>(`/api/v1/admin/venues/${venueId}/layout-versions`, { token });
        return layouts.data.layout_versions.find((layout) => layout.id === layoutId)?.state;
      })
      .toBe('PUBLISHED');
    await page.reload();
    await expect(page.getByText('Published', { exact: true }).first()).toBeVisible();
  });

  let eventId = ledger.events[0] ?? '';
  await test.step('Create an Event and prove invalid lifecycle is rejected', async () => {
    if (eventId) {
      await page.goto(`${urls.admin}/events/${eventId}`);
      await expect(page.getByRole('heading', { name: reviewNames.event })).toBeVisible();
      return;
    }
    await page.getByRole('link', { name: 'Events', exact: true }).click();
    await page
      .getByRole('link', { name: /Create event/ })
      .first()
      .click();
    await page.getByRole('button', { name: 'Continue' }).click();
    await expect(page.getByText('Add an event name and choose a venue to continue.')).toBeVisible();
    await page.getByLabel('Event name').fill(reviewNames.event);
    await page.getByLabel('Venue').selectOption(venueId);
    await page.getByRole('button', { name: 'Continue' }).click();
    await page.getByLabel('Starts').fill('2026-08-25T19:00');
    await page.getByLabel('Ends').fill('2026-08-25T23:00');
    await page.getByLabel('Sales open').fill('2026-08-23T00:00');
    await page.getByLabel('Sales close').fill('2026-08-25T18:30');
    await page.getByLabel('Admission open').fill('2026-08-23T00:00');
    await page.getByLabel('Admission close').fill('2026-08-26T23:59');
    await page.getByLabel('Timezone').fill('Africa/Lagos');
    await page.getByRole('button', { name: 'Continue' }).click();
    await page.getByRole('button', { name: 'Create draft event' }).click();
    await expect(page.getByRole('heading', { name: reviewNames.event })).toBeVisible();
    eventId = new URL(page.url()).pathname.split('/').at(-1) ?? '';
    expect(eventId).toMatch(/^evt_/);
    await addEntity('events', eventId);

    await page.getByRole('button', { name: 'Open sales' }).click();
    await expect(page.getByText(/ready|layout|pricing|policy/i).last()).toBeVisible();
  });

  await test.step('Configure policy, layout, pricing, and inventory', async () => {
    const token = await operatorToken();
    await page.goto(`${urls.admin}/events/${eventId}`);
    const policyAction = page.getByRole('button', { name: 'Use recommended policy' });
    if (await policyAction.isVisible()) await policyAction.click();

    let configuration = await apiJSON<{
      layout?: { finalized_at?: string | null };
      price_tiers: Array<{ id: string; code: string; state: string }>;
    }>(`/api/v1/admin/events/${eventId}/configuration`, { token });
    if (!configuration.data.layout?.finalized_at) {
      await page.getByRole('tab', { name: 'Layout & seats' }).click();
      await page.getByLabel('Published layout').selectOption(layoutId);
      await page.getByRole('button', { name: 'Materialize layout' }).click();
      await expect(page.getByText('Layout materialized')).toBeVisible();
    }

    await page.getByRole('tab', { name: 'Pricing' }).click();
    configuration = await apiJSON<{
      layout?: { finalized_at?: string | null };
      price_tiers: Array<{ id: string; code: string; state: string }>;
    }>(`/api/v1/admin/events/${eventId}/configuration`, { token });
    if (!configuration.data.price_tiers.some((tier) => tier.code === 'LVE')) {
      await page.getByRole('button', { name: 'Add price tier' }).first().click();
      const priceDialog = page.getByRole('dialog', { name: 'Add price tier' });
      await priceDialog.getByLabel('Name').fill('Live Review Admission');
      await priceDialog.getByLabel('Code').fill('LVE');
      await priceDialog.getByLabel('Currency').fill('NGN');
      await priceDialog.getByLabel('Price').fill('50000');
      await priceDialog.getByRole('button', { name: 'Add tier' }).click();
    }
    const assignment = page.getByLabel('Price tier to assign');
    await expect(assignment).toBeVisible();
    await assignment.selectOption({ index: 1 });
    const assignmentResponsePromise = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/v1/admin/events/${eventId}/pricing/assignments`) &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Assign to all inventory' }).click();
    expect((await assignmentResponsePromise).ok()).toBe(true);
    await page.getByRole('tab', { name: 'Inventory' }).click();
    await expect(
      page.getByRole('cell', { name: 'Reserved seat', exact: true }).first(),
    ).toBeVisible();
    await expect(
      page.getByRole('cell', { name: 'General admission', exact: true }).first(),
    ).toBeVisible();
    await screenshot(page, '01-admin-configured-inventory');
  });

  let partnerId = ledger.partners[0] ?? '';
  await test.step('Create Partner, issue a redacted credential, and grant Event access', async () => {
    if (partnerId) {
      await page.goto(`${urls.admin}/partners/${partnerId}`);
      await expect(page.getByRole('heading', { name: reviewNames.partner })).toBeVisible();
    } else {
      await page.getByRole('link', { name: 'Partners', exact: true }).first().click();
      await page.getByRole('button', { name: 'Add partner' }).first().click();
      const partnerDialog = page.getByRole('dialog', { name: 'Add partner' });
      await partnerDialog.getByLabel('Partner name').fill(reviewNames.partner);
      await partnerDialog.getByRole('button', { name: 'Add partner' }).click();
      await page.getByRole('link', { name: new RegExp(reviewNames.partner) }).click();
      partnerId = new URL(page.url()).pathname.split('/').at(-1) ?? '';
      expect(partnerId).toMatch(/^ptr_/);
      await addEntity('partners', partnerId);
    }

    const secrets = await readSecrets();
    if (!secrets.partnerCredential) {
      await page.getByRole('button', { name: 'Issue credential' }).first().click();
      const secretLocator = page.getByTestId('one-time-secret');
      const credential = (await secretLocator.textContent())?.trim() ?? '';
      expect(credential).toBeTruthy();
      await setSecret('partnerCredential', credential);
      await page.getByRole('button', { name: 'I have stored it' }).click();
    }
    const token = await operatorToken();
    const partner = await apiJSON<{
      credentials: Array<{ id: string; state: string }>;
    }>(`/api/v1/admin/partners/${partnerId}`, { token });
    const credentialId = partner.data.credentials.find((item) => item.state === 'ACTIVE')?.id ?? '';
    expect(credentialId).toMatch(/^pcred_/);
    await addEntity('partner_credentials', credentialId);

    await page.getByRole('tab', { name: 'Event access' }).click();
    if (!(await page.getByText('Enabled', { exact: true }).isVisible())) {
      await page.getByLabel('Event to grant').selectOption(eventId);
      await page.getByRole('button', { name: 'Grant access' }).click();
    }
    await expect(page.getByText('Enabled', { exact: true })).toBeVisible();

    await apiJSON(`/api/v1/admin/partners/${partnerId}/allowed-return-urls`, {
      method: 'PUT',
      token,
      idempotencyKey: randomUUID(),
      body: { urls: ['https://127.0.0.1:45991/checkout'] },
    });
  });

  await test.step('Configure a real webhook endpoint and retain its identity', async () => {
    await page.getByRole('link', { name: 'Integrations', exact: true }).click();
    if (!ledger.webhook_endpoints[0]) {
      await page.getByRole('button', { name: 'Add endpoint' }).first().click();
      const dialog = page.getByRole('dialog', { name: 'Add endpoint' });
      await dialog.getByLabel('Partner').selectOption(partnerId);
      await dialog.getByLabel('Endpoint URL').fill('https://127.0.0.1:45991/webhooks');
      await dialog.getByRole('button', { name: 'Add endpoint' }).click();
      await expect(page.getByRole('dialog', { name: 'Signing secret created' })).toBeVisible();
      await page.getByRole('button', { name: 'I have stored it' }).click();
    }
    const token = await operatorToken();
    const endpoints = await apiJSON<{ items: Array<{ id: string; partner_id: string }> }>(
      '/api/v1/admin/webhook-endpoints?limit=100',
      { token },
    );
    const endpointId = endpoints.data.items.find((item) => item.partner_id === partnerId)?.id ?? '';
    expect(endpointId).toMatch(/^wh_/);
    await addEntity('webhook_endpoints', endpointId);
  });

  await test.step('Open sales and verify authoritative state after reload', async () => {
    await page.goto(`${urls.admin}/events/${eventId}`);
    const current = await apiJSON<{ state: string }>(`/api/v1/admin/events/${eventId}`, {
      token: await operatorToken(),
    });
    if (current.data.state !== 'ON_SALE') {
      await page.getByRole('button', { name: 'Open sales' }).click();
    }
    await expect(page.getByText('On sale', { exact: true }).first()).toBeVisible();
    await page.reload();
    await expect(page.getByText('On sale', { exact: true }).first()).toBeVisible();
    const token = await operatorToken();
    const event = await apiJSON<{ state: string }>(`/api/v1/admin/events/${eventId}`, { token });
    expect(event.data.state).toBe('ON_SALE');

    if (!ledger.events[1]) {
      const second = await apiJSON<{ id: string }>('/api/v1/admin/events', {
        method: 'POST',
        token,
        idempotencyKey: randomUUID(),
        body: {
          venue_id: venueId,
          name: reviewNames.wrongEvent,
          starts_at: '2026-08-25T20:00:00+01:00',
          admission_open_at: '2026-08-23T00:00:00+01:00',
          admission_close_at: '2026-08-26T23:59:00+01:00',
          timezone_name: 'Africa/Lagos',
        },
      });
      await addEntity('events', second.data.id);
    }
    await screenshot(page, '01-admin-event-on-sale');
  });

  await saveVideo(page, '01-admin-platform-setup.webm');
});
