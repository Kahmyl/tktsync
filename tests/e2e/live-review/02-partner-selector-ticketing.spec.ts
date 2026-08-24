import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { createServer, type Server } from 'node:https';
import { expect, test } from '@playwright/test';
import { tlsCertPath, tlsKeyPath, urls } from './config';
import {
  addEntity,
  apiJSON,
  entities,
  getSecret,
  operatorToken,
  reviewNames,
  saveVideo,
  screenshot,
  setSecret,
  writeRealTicketCameraFixture,
} from './state';

type SelectionSession = {
  selection_session_id: string;
  selection_url: string;
  expires_at: string;
};

type Hold = {
  id: string;
  reservation_token: string;
  hold_expires_at: string;
  items: Array<{ id: string; inventory_id: string; quantity: number }>;
};

type ConfirmedReservation = {
  reservation_id: string;
  status: 'CONFIRMED';
  tickets: Array<{ id: string; status: string; credential_id: string }>;
};

type HandoffObservation = {
  method: string;
  path: string;
  reservationId: string;
  hadReservationToken: boolean;
};

let receiver: Server | undefined;
let checkoutObservation: HandoffObservation | undefined;

test.use({ trace: 'off' });

test.beforeAll(async () => {
  execFileSync(
    'openssl',
    [
      'req',
      '-x509',
      '-newkey',
      'rsa:2048',
      '-nodes',
      '-days',
      '1',
      '-subj',
      '/CN=127.0.0.1',
      '-addext',
      'subjectAltName=IP:127.0.0.1',
      '-keyout',
      tlsKeyPath,
      '-out',
      tlsCertPath,
    ],
    { stdio: 'ignore' },
  );

  receiver = createServer(
    { key: readFileSync(tlsKeyPath), cert: readFileSync(tlsCertPath) },
    (request, response) => {
      const chunks: Buffer[] = [];
      request.on('data', (chunk: Buffer) => chunks.push(chunk));
      request.on('end', () => {
        if (request.url === '/checkout') {
          const form = new URLSearchParams(Buffer.concat(chunks).toString('utf8'));
          checkoutObservation = {
            method: request.method ?? '',
            path: request.url,
            reservationId: form.get('reservation_id') ?? '',
            hadReservationToken: Boolean(form.get('reservation_token')),
          };
          response.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
          response.end(
            '<!doctype html><title>Checkout received</title><h1>Checkout handoff received</h1><p>Your reservation is ready.</p>',
          );
          return;
        }
        // The same local TLS endpoint is also the configured webhook receiver. Only the
        // request target is observed; webhook bodies and signatures are never persisted.
        response.writeHead(204);
        response.end();
      });
    },
  );
  await new Promise<void>((resolve, reject) => {
    receiver?.once('error', reject);
    receiver?.listen(45991, '127.0.0.1', () => resolve());
  });
});

test.afterAll(async () => {
  if (!receiver) return;
  await new Promise<void>((resolve, reject) =>
    receiver?.close((error) => (error ? reject(error) : resolve())),
  );
});

test('02 Partner Docs to Selector ticketing', async ({ page }) => {
  test.setTimeout(240_000);
  await page.addInitScript(() => {
    const style = document.createElement('style');
    style.textContent =
      'input[type="password"],.request-console pre,.result pre,.response-body,[data-sensitive],[data-e2e-sensitive]{filter:blur(14px)!important;user-select:none!important}';
    document.documentElement.append(style);
  });

  const ledger = await entities();
  const eventId = ledger.events[0];
  expect(eventId).toMatch(/^evt_/);
  const partnerCredential = await getSecret('partnerCredential');
  let selectionSession: SelectionSession | undefined;

  await test.step('Execute authenticated Partner requests in the real Developer Docs', async () => {
    await page.goto(`${urls.docs}/api/events/retrieve`);
    await expect(page.getByRole('heading', { name: 'Retrieve an Event' })).toBeVisible();
    await expect(page.getByLabel('API environment')).toHaveValue('local');
    await page.getByRole('button', { name: 'Set Test Credential' }).click();
    const dialog = page.getByRole('dialog', { name: 'Connect the request console' });
    await dialog.getByLabel('Partner API key').fill(partnerCredential);
    await dialog.getByRole('button', { name: 'Use for this tab' }).click();
    await page.getByLabel(/event_id/).fill(eventId);
    await page.getByRole('button', { name: 'Send request' }).click();
    await expect(page.locator('.result-head')).toContainText('200');
    await expect(page.locator('.response-body')).toContainText(reviewNames.event);
    expect(await page.evaluate(() => Object.keys(localStorage))).toEqual([]);
    expect(await page.evaluate(() => Object.keys(sessionStorage))).toEqual([]);
    await screenshot(page, '02-docs-real-authenticated-event');

    await page.goto(`${urls.docs}/api/selection-sessions/create`);
    await page.getByRole('button', { name: 'Set Test Credential' }).click();
    const sessionDialog = page.getByRole('dialog', { name: 'Connect the request console' });
    await sessionDialog.getByLabel('Partner API key').fill(partnerCredential);
    await sessionDialog.getByRole('button', { name: 'Use for this tab' }).click();
    const bodyFields = page.locator('.body-fields');
    await bodyFields
      .getByText('event_id', { exact: true })
      .locator('..')
      .getByRole('textbox')
      .fill(eventId);
    await bodyFields
      .getByText('return_url', { exact: true })
      .locator('..')
      .getByRole('textbox')
      .fill('https://127.0.0.1:45991/checkout');
    await page.getByRole('button', { name: 'Send request' }).click();
    await expect(page.locator('.result-head')).toContainText('201');
    selectionSession = JSON.parse(
      await page.locator('.response-body').innerText(),
    ) as SelectionSession;
    expect(selectionSession.selection_session_id).toMatch(/^sel_/);
    expect(selectionSession.selection_url).toContain('#');
    await setSecret('selectionURL', selectionSession.selection_url);
    await screenshot(page, '02-docs-selection-session-created');
  });

  await test.step('Bare and invalid Selector links fail closed without revealing the Event', async () => {
    await page.goto(urls.selector);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
    await expect(page.locator('body')).not.toContainText(reviewNames.event);
    await screenshot(page, '02-selector-bare-fails-closed');

    await page.goto(urls.docs);
    await page.goto(`${urls.selector}/#not-a-real-capability`);
    await expect(page).toHaveURL(`${urls.selector}/`);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
    await expect(page.url()).not.toContain('not-a-real-capability');
  });

  let hold: Hold | undefined;
  await test.step('Select one reserved seat and one GA ticket, then create a real hold', async () => {
    expect(selectionSession).toBeDefined();
    await page.goto(selectionSession!.selection_url);
    await expect(page).toHaveURL(`${urls.selector}/s`);
    expect(page.url()).not.toContain('#');
    await expect(page.getByRole('heading', { name: reviewNames.event })).toBeVisible();

    const reservedSeats = page.locator('button.seat.available');
    await expect(reservedSeats.first()).toBeVisible();
    await reservedSeats.first().click();
    const addGA = page.getByRole('button', { name: /Add one Main Floor ticket/ });
    await addGA.click();
    await expect(page.getByText('2 tickets', { exact: true }).first()).toBeVisible();
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
    expect(hold.items).toHaveLength(2);
    expect(hold.items.every((item) => item.quantity === 1)).toBe(true);
    await addEntity('reservations', hold.id);
    await setSecret('reservationToken', hold.reservation_token);
    await expect(page.getByRole('heading', { name: 'Your tickets are held' })).toBeVisible();
    await expect(page.getByText('Time remaining')).toBeVisible();
    await screenshot(page, '02-selector-real-hold');
  });

  await test.step('Perform the secure form POST handoff without putting its token in the URL', async () => {
    expect(hold).toBeDefined();
    await page.getByRole('button', { name: 'Continue to checkout' }).click();
    await expect(page.getByRole('heading', { name: 'Checkout handoff received' })).toBeVisible();
    expect(checkoutObservation).toEqual({
      method: 'POST',
      path: '/checkout',
      reservationId: hold!.id,
      hadReservationToken: true,
    });
    expect(page.url()).not.toContain(hold!.reservation_token);
    await screenshot(page, '02-checkout-secure-post-received');
  });

  await test.step('Complete checkout through the real Partner API and issue two real tickets', async () => {
    expect(hold).toBeDefined();
    const begin = await apiJSON<{
      reservation_id: string;
      checkout_attempt: { id: string };
    }>(`/api/v1/partner/reservations/${hold!.id}/checkout`, {
      method: 'POST',
      partnerCredential,
      reservationToken: hold!.reservation_token,
      idempotencyKey: randomUUID(),
      body: {},
    });
    expect(begin.data.checkout_attempt.id).toMatch(/^chk_/);
    const confirmed = await apiJSON<ConfirmedReservation>(
      `/api/v1/partner/reservations/${hold!.id}/confirm`,
      {
        method: 'POST',
        partnerCredential,
        reservationToken: hold!.reservation_token,
        idempotencyKey: randomUUID(),
        body: {
          checkout_attempt_id: begin.data.checkout_attempt.id,
          partner_order_ref: `order-${randomUUID()}`,
          partner_payment_ref: `payment-${randomUUID()}`,
        },
      },
    );
    expect(confirmed.data.status).toBe('CONFIRMED');
    expect(confirmed.data.tickets).toHaveLength(2);
    const issued: Array<{
      id: string;
      inventoryKind: 'RESERVED' | 'GA';
      qrPayload: string;
    }> = [];
    for (const ticket of confirmed.data.tickets) {
      await addEntity('tickets', ticket.id);
      const credential = await apiJSON<{ qr_payload: string }>(
        `/api/v1/partner/tickets/${ticket.id}/credential`,
        { partnerCredential },
      );
      expect(credential.data.qr_payload).toMatch(/^qr1\./);
      const detail = await apiJSON<{ inventory_kind: 'RESERVED' | 'GA' }>(
        `/api/v1/admin/tickets/${ticket.id}`,
        { token: await operatorToken() },
      );
      issued.push({
        id: ticket.id,
        inventoryKind: detail.data.inventory_kind,
        qrPayload: credential.data.qr_payload,
      });
    }
    const cameraTicket = issued.find((ticket) => ticket.inventoryKind === 'GA');
    const supportTicket = issued.find((ticket) => ticket.inventoryKind === 'RESERVED');
    expect(cameraTicket).toBeDefined();
    expect(supportTicket).toBeDefined();
    await setSecret('ticket1ID', cameraTicket!.id);
    await setSecret('ticket1QR', cameraTicket!.qrPayload);
    await setSecret('ticket2ID', supportTicket!.id);
    await setSecret('ticket2QR', supportTicket!.qrPayload);
    await writeRealTicketCameraFixture(cameraTicket!.qrPayload);
  });

  await test.step('Verify the real mobile review sheet and body containment', async () => {
    const mobileSession = await apiJSON<SelectionSession>('/api/v1/partner/selection-sessions', {
      method: 'POST',
      partnerCredential,
      idempotencyKey: randomUUID(),
      body: {
        event_id: eventId,
        return_url: 'https://127.0.0.1:45991/checkout',
        buyer_session_ref: `mobile-${randomUUID()}`,
      },
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(mobileSession.data.selection_url);
    await expect(page.getByRole('heading', { name: reviewNames.event })).toBeVisible();
    await page.locator('button.seat.available').first().click();
    await page.getByRole('button', { name: 'Review' }).click();
    await expect(page.getByRole('dialog', { name: 'Your selection' })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
      ),
    ).toBeLessThanOrEqual(0);
    await screenshot(page, '02-selector-mobile-review-sheet');
  });

  await saveVideo(page, '02-partner-selector-ticketing.webm');
});
