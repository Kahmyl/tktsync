import { randomUUID } from 'node:crypto';
import { expect, test, type Page } from '@playwright/test';
import { urls } from './config';
import {
  addEntity,
  apiJSON,
  operatorToken,
  saveVideo,
  screenshot,
  setSecret,
  writeRealTicketCameraFixture,
} from './state';

type SelectionSession = {
  selection_url: string;
};

type Hold = {
  id: string;
  reservation_token: string;
};

type Confirmation = {
  status: 'CONFIRMED';
  tickets: Array<{ id: string; credential_id: string; status: string }>;
};

type Credential = {
  ticket_id: string;
  credential_id: string;
  status: 'ACTIVE';
  qr_payload: string;
  qr_url: string;
};

type ScanResponse = {
  result: string;
  admission_id?: string;
};

async function chooseEvent(page: Page, eventName: string) {
  const option = page.getByRole('button', {
    name: new RegExp(`Select ${eventName}`),
  });
  await expect(option).toBeVisible();
  await option.click();
}

async function manualScan(page: Page, credential: string) {
  await page.getByRole('button', { name: 'Enter code manually' }).first().click();
  const sheet = page.getByRole('dialog', { name: 'Enter code manually' });
  await sheet.getByLabel('Ticket code').fill(credential);
  await sheet.getByRole('button', { name: 'Check ticket' }).click();
}

test.use({ trace: 'off' });

test('07 TktSync-hosted ticket QR delivery', async ({ page, context }) => {
  test.setTimeout(240_000);

  const adminToken = await operatorToken();
  const events = await apiJSON<{
    items: Array<{
      id: string;
      name: string;
      state: string;
      venue_id: string;
      capacity: number;
      sold: number;
    }>;
  }>('/api/v1/admin/events?state=ON_SALE&limit=100&offset=0', {
    token: adminToken,
  });
  const sourceEvent = events.data.items.find((item) => item.capacity > item.sold);
  expect(sourceEvent).toBeDefined();
  const sourceConfiguration = await apiJSON<{
    layout?: { id: string; finalized_at?: string | null };
  }>(`/api/v1/admin/events/${sourceEvent!.id}/configuration`, { token: adminToken });
  expect(sourceConfiguration.data.layout?.id).toMatch(/^lay_/);

  const eventName = `Hosted QR Acceptance ${randomUUID().slice(0, 8)}`;
  const createdEvent = await apiJSON<{ id: string }>('/api/v1/admin/events', {
    method: 'POST',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: {
      venue_id: sourceEvent!.venue_id,
      name: eventName,
      starts_at: new Date(Date.now() + 48 * 60 * 60 * 1000).toISOString(),
      ends_at: new Date(Date.now() + 52 * 60 * 60 * 1000).toISOString(),
      sales_open_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
      sales_close_at: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
      admission_open_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
      admission_close_at: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
      timezone_name: 'Africa/Lagos',
    },
  });
  const eventID = createdEvent.data.id;
  expect(eventID).toMatch(/^evt_/);
  await addEntity('events', eventID);

  await apiJSON(`/api/v1/admin/events/${eventID}/materialize-layout`, {
    method: 'POST',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: { layout_id: sourceConfiguration.data.layout!.id },
  });
  const priceTier = await apiJSON<{ id: string }>(`/api/v1/admin/events/${eventID}/price-tiers`, {
    method: 'POST',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: {
      code: 'QRV',
      name: 'Hosted QR Validation',
      amount_minor: 1000,
      currency: 'NGN',
    },
  });
  const inventory = await apiJSON<{
    inventory: Array<{
      kind: 'RESERVED' | 'GA';
      snapshot_object_key: string;
      section_object_key: string;
    }>;
  }>(`/api/v1/admin/events/${eventID}/inventory`, { token: adminToken });
  const reservedSections = [
    ...new Set(
      inventory.data.inventory
        .filter((item) => item.kind === 'RESERVED')
        .map((item) => item.section_object_key),
    ),
  ];
  const gaPools = inventory.data.inventory
    .filter((item) => item.kind === 'GA')
    .map((item) => item.snapshot_object_key);
  await apiJSON(`/api/v1/admin/events/${eventID}/pricing/assignments`, {
    method: 'POST',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: {
      price_tier_id: priceTier.data.id,
      section_object_keys: reservedSections,
      ga_pool_object_keys: gaPools,
    },
  });
  await apiJSON(`/api/v1/admin/events/${eventID}/open-sales`, {
    method: 'POST',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: {},
  });

  const partners = await apiJSON<{
    items: Array<{ id: string; state: string }>;
  }>('/api/v1/admin/partners?state=ACTIVE&limit=100&offset=0', {
    token: adminToken,
  });
  const partnerID = partners.data.items[0]?.id ?? '';
  expect(partnerID).toMatch(/^ptr_/);
  await addEntity('partners', partnerID);
  await apiJSON(`/api/v1/admin/events/${eventID}/partners/${partnerID}/access`, {
    method: 'POST',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: {},
  });
  await apiJSON(`/api/v1/admin/partners/${partnerID}/allowed-return-urls`, {
    method: 'PUT',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: { urls: ['https://127.0.0.1:45991/checkout'] },
  });
  const issuedCredential = await apiJSON<{
    id: string;
    credential: string;
  }>(`/api/v1/admin/partners/${partnerID}/credentials`, {
    method: 'POST',
    token: adminToken,
    idempotencyKey: randomUUID(),
    body: {},
  });
  const partnerCredential = issuedCredential.data.credential;
  expect(issuedCredential.data.id).toMatch(/^pcred_/);
  expect(partnerCredential).toBeTruthy();
  await addEntity('partner_credentials', issuedCredential.data.id);
  await setSecret('partnerCredential', partnerCredential);

  const selection = await apiJSON<SelectionSession>('/api/v1/partner/selection-sessions', {
    method: 'POST',
    partnerCredential,
    idempotencyKey: randomUUID(),
    body: {
      event_id: eventID,
      return_url: 'https://127.0.0.1:45991/checkout',
      buyer_session_ref: `hosted-qr-${randomUUID()}`,
    },
  });

  let hold: Hold;
  await test.step('Create a real selection and hold through the Selector UI', async () => {
    await page.goto(selection.data.selection_url);
    await expect(page.getByRole('heading', { name: eventName })).toBeVisible();
    const seat = page.locator('button.seat.available').first();
    await expect(seat).toBeVisible();
    await seat.click();
    const responsePromise = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/v1/selection/reservations') &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Hold tickets' }).click();
    const response = await responsePromise;
    expect(response.status()).toBe(201);
    hold = (await response.json()) as Hold;
    expect(hold.id).toMatch(/^res_/);
    expect(hold.reservation_token).toBeTruthy();
    await addEntity('reservations', hold.id);
    await expect(page.getByRole('heading', { name: 'Your tickets are held' })).toBeVisible();
    await screenshot(page, '07-hosted-qr-real-hold');
  });

  let ticketID = '';
  let original: Credential;
  await test.step('Confirm the real hold and retrieve both QR delivery surfaces', async () => {
    const checkout = await apiJSON<{
      checkout_attempt: { id: string };
    }>(`/api/v1/partner/reservations/${hold.id}/checkout`, {
      method: 'POST',
      partnerCredential,
      reservationToken: hold.reservation_token,
      idempotencyKey: randomUUID(),
      body: {},
    });
    const confirmed = await apiJSON<Confirmation>(
      `/api/v1/partner/reservations/${hold.id}/confirm`,
      {
        method: 'POST',
        partnerCredential,
        reservationToken: hold.reservation_token,
        idempotencyKey: randomUUID(),
        body: {
          checkout_attempt_id: checkout.data.checkout_attempt.id,
          partner_order_ref: `hosted-qr-order-${randomUUID()}`,
          partner_payment_ref: `hosted-qr-payment-${randomUUID()}`,
        },
      },
    );
    expect(confirmed.data.status).toBe('CONFIRMED');
    expect(confirmed.data.tickets).toHaveLength(1);
    ticketID = confirmed.data.tickets[0].id;
    await addEntity('tickets', ticketID);

    original = (
      await apiJSON<Credential>(`/api/v1/partner/tickets/${ticketID}/credential`, {
        partnerCredential,
      })
    ).data;
    expect(original.ticket_id).toBe(ticketID);
    expect(original.qr_payload).toMatch(/^qr1\./);
    const hosted = new URL(original.qr_url);
    expect(hosted.origin).toBe(urls.api);
    expect(hosted.pathname).toMatch(/^\/api\/v1\/ticket-qr\/tqp1_[A-Za-z0-9_-]+$/);
    expect(original.qr_url).not.toContain('qr1');
    expect(original.qr_url).not.toContain(ticketID);
    expect(original.qr_url).not.toContain(partnerCredential);
    expect(original.qr_url).not.toContain(hold.reservation_token);
    await setSecret('hostedQRURL', original.qr_url);

    const authenticated = await fetch(`${urls.api}/api/v1/partner/tickets/${ticketID}/qr`, {
      headers: { Authorization: `Bearer ${partnerCredential}` },
    });
    expect(authenticated.status).toBe(200);
    expect(authenticated.headers.get('content-type')).toBe('image/svg+xml');
    expect(authenticated.headers.get('cache-control')).toBe('no-store');
    expect(authenticated.headers.get('x-content-type-options')).toBe('nosniff');
  });

  let originalSVG = '';
  await test.step('Open and visibly verify the buyer-usable hosted QR image', async () => {
    const response = await page.goto(original.qr_url);
    expect(response?.status()).toBe(200);
    expect(response?.headers()['content-type']).toBe('image/svg+xml');
    expect(response?.headers()['cache-control']).toBe('no-store');
    await expect(page.locator('svg')).toBeVisible();
    await expect(page.locator('svg path')).toBeVisible();
    const box = await page.locator('svg').boundingBox();
    expect(box?.width ?? 0).toBeGreaterThan(100);
    expect(box?.height ?? 0).toBeGreaterThan(100);
    originalSVG = await page.content();
    expect(originalSVG).not.toContain(original.qr_payload);
    await screenshot(page, '07-hosted-qr-rendered');
  });

  await test.step('Scan the same authoritative credential through the real Scanner UI', async () => {
    await writeRealTicketCameraFixture(original.qr_payload);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgentData', {
        configurable: true,
        value: { mobile: true },
      });
    });
    await context.grantPermissions(['camera'], { origin: urls.scanner });
    await page.goto(urls.scanner);
    await expect(page.getByRole('heading', { name: 'Choose an event to scan' })).toBeVisible();
    await chooseEvent(page, eventName);

    const scanResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/v1/admission/scans') &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Open camera' }).click();
    const admitted = (await (await scanResponse).json()) as ScanResponse;
    expect(admitted.result).toBe('ADMITTED');
    expect(admitted.admission_id).toMatch(/^adm_/);
    await addEntity('admissions', admitted.admission_id!);
    await expect(page.getByRole('heading', { name: 'Admit guest' })).toBeVisible();
    await screenshot(page, '07-hosted-qr-scanner-admitted');

    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await expect(page.getByRole('heading', { name: 'Already checked in' })).toBeVisible();
    await screenshot(page, '07-hosted-qr-scanner-duplicate');
  });

  await test.step('Reissue while preserving the hosted URL and replacing its QR content', async () => {
    await apiJSON(`/api/v1/partner/tickets/${ticketID}/credentials/reissue`, {
      method: 'POST',
      partnerCredential,
      idempotencyKey: randomUUID(),
      body: {},
    });
    const replacement = (
      await apiJSON<Credential>(`/api/v1/partner/tickets/${ticketID}/credential`, {
        partnerCredential,
      })
    ).data;
    expect(replacement.credential_id).not.toBe(original.credential_id);
    expect(replacement.qr_payload).not.toBe(original.qr_payload);
    expect(replacement.qr_url).toBe(original.qr_url);

    const response = await fetch(replacement.qr_url);
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toBe('image/svg+xml');
    const replacementSVG = await response.text();
    expect(replacementSVG).not.toBe(originalSVG);
    expect(replacementSVG).not.toContain(replacement.qr_payload);

    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await manualScan(page, original.qr_payload);
    await expect(page.getByRole('heading', { name: 'Ticket not valid' })).toBeVisible();

    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await manualScan(page, replacement.qr_payload);
    await expect(page.getByRole('heading', { name: 'Already checked in' })).toBeVisible();
    await screenshot(page, '07-hosted-qr-reissue-current-valid');

    await page.goto(replacement.qr_url);
    await expect(page.locator('svg')).toBeVisible();
    await screenshot(page, '07-hosted-qr-rendered-after-reissue');
  });

  await saveVideo(page, '07-hosted-ticket-qr.webm');
});
