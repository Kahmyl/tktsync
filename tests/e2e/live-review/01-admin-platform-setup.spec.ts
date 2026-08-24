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
      const retry = page.getByRole('button', { name: 'Try again' });
      for (let attempt = 0; attempt < 8; attempt += 1) {
        if (
          await page
            .getByRole('heading', { name: 'Upcoming events', exact: true })
            .isVisible()
            .catch(() => false)
        )
          break;
        if (await retry.isVisible().catch(() => false)) await retry.click();
        await page.waitForTimeout(2_000);
      }
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
      await page.getByRole('button', { name: 'Reserved section' }).click();
      await page.getByLabel('Name').fill('VIP');
      await page.getByLabel('Rows').fill('3');
      await page.getByLabel('Seats per row').fill('6');
      await page.getByLabel('Starting seat number').fill('10');
      await page.getByRole('button', { name: 'General admission' }).click();
      await page.getByLabel('Name').fill('Main Floor');
      await page.getByLabel('Capacity').fill('100');
      for (let move = 0; move < 12; move += 1)
        await page.locator('g.layout-object.selected').press('Shift+ArrowRight');
      await page.getByRole('button', { name: 'Table area' }).click();
      await page.getByLabel('Name').fill('Banquet');
      await page.getByLabel('Tables').fill('3');
      await page.getByLabel('Seats per table').fill('4');
      for (let move = 0; move < 20; move += 1)
        await page.locator('g.layout-object.selected').press('Shift+ArrowRight');
      await page.getByRole('button', { name: 'Stage', exact: true }).click();
      await page.getByLabel('Name').fill('Main Stage');
      await page.getByLabel('Rotation').selectOption('180');
      for (let move = 0; move < 12; move += 1)
        await page.locator('g.layout-object.selected').press('Shift+ArrowDown');
      await page.getByRole('button', { name: 'Save draft' }).click();

      const layouts = await apiJSON<{ layout_versions: Array<{ id: string; state: string }> }>(
        `/api/v1/admin/venues/${venueId}/layout-versions`,
        { token },
      );
      layoutId = layouts.data.layout_versions.find((layout) => layout.state === 'DRAFT')?.id ?? '';
      expect(layoutId).toMatch(/^lay_/);
      await addEntity('layouts', layoutId);
      await page.getByRole('button', { name: 'Close builder' }).click();
    }
    await page.reload();
    await page.getByRole('button', { name: 'Edit draft' }).click();
    await expect(page.getByText('18 seats')).toBeVisible();
    await expect(page.getByText('12 seats')).toBeVisible();
    await expect(page.getByText('100 capacity')).toBeVisible();
    await page.locator('g.layout-object').filter({ hasText: 'VIP' }).click();
    await expect(page.getByLabel('Rows')).toHaveValue('3');
    await expect(page.getByLabel('Seats per row')).toHaveValue('6');
    await expect(page.getByLabel('Starting seat number')).toHaveValue('10');
    const publishResponsePromise = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/v1/admin/venue-layouts/${layoutId}/publish`) &&
        response.request().method() === 'POST',
    );
    page.once('dialog', (dialog) => dialog.accept());
    await page.getByRole('button', { name: 'Publish', exact: true }).click();
    const publishResponse = await publishResponsePromise;
    expect(publishResponse.ok()).toBe(true);
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
    let configuration = await apiJSON<{
      layout?: { finalized_at?: string | null };
      price_tiers: Array<{ id: string; code: string; state: string }>;
      transaction_policy?: { hold_duration_seconds: number } | null;
    }>(`/api/v1/admin/events/${eventId}/configuration`, { token });
    const policyAction = page.getByRole('button', { name: 'Use recommended policy' });
    if (!configuration.data.transaction_policy) {
      await expect(policyAction).toBeVisible();
      const policyResponse = page.waitForResponse(
        (response) =>
          response.url().endsWith(`/api/v1/admin/events/${eventId}/transaction-policy`) &&
          response.request().method() === 'PUT',
      );
      await policyAction.click();
      expect((await policyResponse).ok()).toBe(true);
    }

    configuration = await apiJSON<{
      layout?: { finalized_at?: string | null };
      price_tiers: Array<{ id: string; code: string; state: string }>;
      transaction_policy?: { hold_duration_seconds: number } | null;
    }>(`/api/v1/admin/events/${eventId}/configuration`, { token });
    expect(configuration.data.transaction_policy?.hold_duration_seconds).toBeGreaterThan(0);
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
    configuration = await apiJSON<{
      layout?: { finalized_at?: string | null };
      price_tiers: Array<{ id: string; code: string; state: string }>;
    }>(`/api/v1/admin/events/${eventId}/configuration`, { token });
    if (!configuration.data.price_tiers.some((tier) => tier.code === 'GAF')) {
      await page.getByRole('button', { name: 'Add price tier' }).first().click();
      const priceDialog = page.getByRole('dialog', { name: 'Add price tier' });
      await priceDialog.getByLabel('Name').fill('Main Floor Admission');
      await priceDialog.getByLabel('Code').fill('GAF');
      await priceDialog.getByLabel('Currency').fill('NGN');
      await priceDialog.getByLabel('Price').fill('35000');
      await priceDialog.getByRole('button', { name: 'Add tier' }).click();
    }
    configuration = await apiJSON<{
      price_tiers: Array<{ id: string; code: string; state: string }>;
    }>(`/api/v1/admin/events/${eventId}/configuration`, { token });
    const vipTier = configuration.data.price_tiers.find((tier) => tier.code === 'LVE');
    const gaTier = configuration.data.price_tiers.find((tier) => tier.code === 'GAF');
    expect(vipTier?.id).toMatch(/^price_/);
    expect(gaTier?.id).toMatch(/^price_/);
    const pricingInventory = await apiJSON<{
      inventory: Array<{
        kind: 'RESERVED' | 'GA';
        section_object_key: string;
        section_name: string;
        snapshot_object_key: string;
      }>;
    }>(`/api/v1/admin/events/${eventId}/inventory`, { token });
    const vipSectionKey = pricingInventory.data.inventory.find(
      (item) => item.kind === 'RESERVED' && item.section_name === 'VIP',
    )?.section_object_key;
    const mainFloorPoolKey = pricingInventory.data.inventory.find(
      (item) => item.kind === 'GA' && item.section_name === 'Main Floor',
    )?.snapshot_object_key;
    const banquetSectionKey = pricingInventory.data.inventory.find(
      (item) => item.kind === 'RESERVED' && item.section_name === 'Banquet',
    )?.section_object_key;
    expect(vipSectionKey).toBeTruthy();
    expect(mainFloorPoolKey).toBeTruthy();
    expect(banquetSectionKey).toBeTruthy();
    const assignment = page.getByLabel('Price tier to assign');
    await expect(assignment).toBeVisible();
    await assignment.selectOption(vipTier!.id);
    await page.getByLabel('Apply to').selectOption('section');
    await page.getByLabel('Section or area').selectOption({ label: 'VIP' });
    const vipAssignmentPromise = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/v1/admin/events/${eventId}/pricing/assignments`) &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Review pricing assignment' }).click();
    await page
      .getByRole('dialog', { name: 'Apply pricing?' })
      .getByRole('button', { name: 'Apply pricing' })
      .click();
    const vipAssignment = await vipAssignmentPromise;
    expect(vipAssignment.ok()).toBe(true);
    expect(vipAssignment.request().postDataJSON()).toMatchObject({
      price_tier_id: vipTier!.id,
      section_object_keys: [vipSectionKey],
      ga_pool_object_keys: [],
    });

    await assignment.selectOption(gaTier!.id);
    await page.getByLabel('Section or area').selectOption({ label: 'Main Floor' });
    const gaAssignmentPromise = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/v1/admin/events/${eventId}/pricing/assignments`) &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Review pricing assignment' }).click();
    await page
      .getByRole('dialog', { name: 'Apply pricing?' })
      .getByRole('button', { name: 'Apply pricing' })
      .click();
    const gaAssignment = await gaAssignmentPromise;
    expect(gaAssignment.ok()).toBe(true);
    const gaBody = gaAssignment.request().postDataJSON() as {
      section_object_keys: string[];
      ga_pool_object_keys: string[];
    };
    expect(gaBody.section_object_keys).toEqual([]);
    expect(gaBody.ga_pool_object_keys).toEqual([mainFloorPoolKey]);

    await assignment.selectOption(vipTier!.id);
    await page.getByLabel('Section or area').selectOption({ label: 'Banquet' });
    const banquetAssignmentPromise = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/v1/admin/events/${eventId}/pricing/assignments`) &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Review pricing assignment' }).click();
    await page
      .getByRole('dialog', { name: 'Apply pricing?' })
      .getByRole('button', { name: 'Apply pricing' })
      .click();
    const banquetAssignment = await banquetAssignmentPromise;
    expect(banquetAssignment.ok()).toBe(true);
    expect(banquetAssignment.request().postDataJSON()).toMatchObject({
      price_tier_id: vipTier!.id,
      section_object_keys: [banquetSectionKey],
      ga_pool_object_keys: [],
    });

    const authoritativeInventory = await apiJSON<{
      inventory: Array<{
        kind: 'RESERVED' | 'GA';
        section_object_key: string;
        snapshot_object_key: string;
        price_tier_id: string | null;
      }>;
    }>(`/api/v1/admin/events/${eventId}/inventory`, { token });
    expect(
      authoritativeInventory.data.inventory
        .filter((item) => item.kind === 'RESERVED' && item.section_object_key === vipSectionKey)
        .every((item) => item.price_tier_id === vipTier!.id),
    ).toBe(true);
    expect(
      authoritativeInventory.data.inventory.find(
        (item) => item.kind === 'GA' && item.snapshot_object_key === mainFloorPoolKey,
      )?.price_tier_id,
    ).toBe(gaTier!.id);
    await page.getByRole('tab', { name: 'Inventory' }).click();
    await expect(
      page.getByRole('cell', { name: 'Reserved seat', exact: true }).first(),
    ).toBeVisible();
    await expect(
      page.getByRole('cell', { name: 'General admission', exact: true }).first(),
    ).toBeVisible();
    await screenshot(page, '01-admin-configured-inventory');
  });

  await test.step('Create and release reserved/GA blocks and an internal allocation through Admin UI', async () => {
    const token = await operatorToken();
    await page.goto(`${urls.admin}/events/${eventId}`);
    await page.getByRole('tab', { name: 'Inventory' }).click();

    const createRestriction = async ({
      kind,
      purpose,
      search,
      quantity,
    }: {
      kind: 'block' | 'allocation';
      purpose: string;
      search: string;
      quantity?: number;
    }) => {
      await page
        .getByRole('button', { name: kind === 'block' ? 'Block inventory' : 'Create allocation' })
        .click();
      const dialog = page.getByRole('dialog', {
        name: kind === 'block' ? 'Block inventory' : 'Create allocation',
      });
      if (kind === 'block') await dialog.getByLabel('Purpose').selectOption(purpose);
      else await dialog.getByLabel('Purpose').fill(purpose);
      await dialog.getByLabel('Apply to').selectOption('seats');
      await dialog.getByLabel('Find inventory').fill(search);
      const choice = dialog.locator('.inventory-choice-list label').first();
      await expect(choice).toBeVisible();
      await choice.locator('input[type="checkbox"]').check();
      if (quantity !== undefined)
        await choice.getByRole('spinbutton', { name: /quantity/ }).fill(String(quantity));
      const responsePromise = page.waitForResponse(
        (response) =>
          response
            .url()
            .endsWith(
              `/api/v1/admin/events/${eventId}/${kind === 'block' ? 'blocks' : 'allocations'}`,
            ) && response.request().method() === 'POST',
      );
      await dialog
        .getByRole('button', { name: kind === 'block' ? 'Create block' : 'Create allocation' })
        .click();
      const response = await responsePromise;
      expect(response.ok()).toBe(true);
      return (await response.json()) as { id: string; state: string };
    };

    const releaseRestriction = async (id: string, purpose: string) => {
      const item = page.locator('.restriction-list article').filter({ hasText: purpose }).first();
      await expect(item).toContainText('Active');
      page.once('dialog', (dialog) => dialog.accept());
      const responsePromise = page.waitForResponse(
        (response) =>
          response.url().includes(`/api/v1/admin/`) &&
          response.url().endsWith(`/${id}/release`) &&
          response.request().method() === 'POST',
      );
      await item.getByRole('button', { name: 'Release' }).click();
      expect((await responsePromise).ok()).toBe(true);
      await expect(item).toContainText('Released');
    };

    const reservedBlock = await createRestriction({
      kind: 'block',
      purpose: 'VIP',
      search: 'VIP',
    });
    await releaseRestriction(reservedBlock.id, 'VIP');

    const gaBlock = await createRestriction({
      kind: 'block',
      purpose: 'Sponsor',
      search: 'Main Floor',
      quantity: 10,
    });
    await releaseRestriction(gaBlock.id, 'Sponsor');

    const allocation = await createRestriction({
      kind: 'allocation',
      purpose: 'Internal review allocation',
      search: 'VIP',
    });
    await releaseRestriction(allocation.id, 'Internal review allocation');

    const restrictions = await apiJSON<{
      items: Array<{
        id: string;
        state: string;
        reserved_quantity: number;
        ga_quantity: number;
        mode: string | null;
      }>;
    }>(`/api/v1/admin/events/${eventId}/restrictions`, { token });
    expect(restrictions.data.items).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ id: reservedBlock.id, state: 'RELEASED', reserved_quantity: 1 }),
        expect.objectContaining({ id: gaBlock.id, state: 'RELEASED', ga_quantity: 10 }),
        expect.objectContaining({
          id: allocation.id,
          state: 'RELEASED',
          mode: 'NON_PUBLIC',
          reserved_quantity: 1,
        }),
      ]),
    );
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
      await dialog.getByLabel('Endpoint URL').fill('https://webhook-receiver:9443/webhooks');
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
