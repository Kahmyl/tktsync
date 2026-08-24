import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { urls } from './config';
import { apiJSON, entities, getSecret, reviewNames, saveVideo, screenshot } from './state';

type SelectionSession = {
  selection_session_id: string;
  selection_url: string;
  expires_at: string;
};

test.use({ trace: 'off' });

test('05 Security, device, failure modes, and realtime', async ({ page, context }) => {
  test.setTimeout(240_000);
  await context.addInitScript(() => {
    const style = document.createElement('style');
    style.textContent =
      'input[type="password"],.response-body,.result pre,video{filter:blur(14px)!important;user-select:none!important}';
    document.documentElement.append(style);
  });

  await test.step('Admin blocks unauthenticated protected navigation', async () => {
    await page.goto(urls.admin);
    await page.evaluate(() => localStorage.clear());
    await page.goto(`${urls.admin}/events`);
    await expect(page).toHaveURL(/\/sign-in$/);
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
  });

  await test.step('Selector bare and invalid capabilities fail closed without Event leakage', async () => {
    await page.goto(urls.selector);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
    await expect(page.locator('body')).not.toContainText(reviewNames.event);
    await page.goto(urls.docs);
    await page.goto(`${urls.selector}/#expired-or-invalid-live-review-capability`);
    await expect(page).toHaveURL(`${urls.selector}/`);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
    expect(page.url()).not.toContain('expired-or-invalid');
    await screenshot(page, '05-selector-invalid-fails-closed');
  });

  await test.step('Docs proxy rejects a wrong Partner credential and blocks Admin proxying', async () => {
    await page.goto(`${urls.docs}/api/events/retrieve`);
    const result = await page.evaluate(async () => {
      const partner = await fetch('/__docs-exec/api/v1/partner/events/evt_AAAAAAAAAAAAAAAAAAAAAA', {
        headers: { Authorization: 'Bearer tkp_live_review_intentionally_invalid' },
      });
      const admin = await fetch('/__docs-exec/api/v1/admin/dashboard', {
        headers: { Authorization: 'Bearer tkp_live_review_intentionally_invalid' },
      });
      return { partner: partner.status, admin: admin.status };
    });
    expect(result.partner).toBe(401);
    expect(result.admin < 200 || result.admin >= 300).toBe(true);
    expect(await page.getByLabel('API environment').locator('option').allTextContents()).toEqual([
      'Local',
    ]);
  });

  const ledger = await entities();
  const eventId = ledger.events[0];
  const partnerCredential = await getSecret('partnerCredential');
  const createSelection = (buyerSessionRef: string) =>
    apiJSON<SelectionSession>('/api/v1/partner/selection-sessions', {
      method: 'POST',
      partnerCredential,
      idempotencyKey: randomUUID(),
      body: {
        event_id: eventId,
        return_url: 'https://127.0.0.1:45991/checkout',
        buyer_session_ref: buyerSessionRef,
      },
    });

  await test.step('Use live Selector state to prove disabled inventory, bounded GA, and realtime', async () => {
    const [sessionA, sessionB] = await Promise.all([
      createSelection(`realtime-a-${randomUUID()}`),
      createSelection(`realtime-b-${randomUUID()}`),
    ]);
    await page.goto(sessionA.data.selection_url);
    await expect(page).toHaveURL(`${urls.selector}/s`);
    await expect(page.getByRole('heading', { name: reviewNames.event })).toBeVisible();
    const unavailable = page.locator('button.seat.unavailable');
    await expect(unavailable.first()).toBeVisible();
    await expect(unavailable.first()).toBeDisabled();

    const addGA = page.getByRole('button', { name: /Add one Main Floor ticket/ });
    let additions = 0;
    while ((await addGA.isEnabled()) && additions < 10) {
      await addGA.click();
      additions += 1;
    }
    expect(additions).toBeGreaterThan(0);
    await expect(addGA).toBeDisabled();
    const removeGA = page.getByRole('button', { name: /Remove one Main Floor ticket/ });
    while ((await removeGA.isEnabled()) && additions > 0) {
      await removeGA.click();
      additions -= 1;
    }
    await expect(removeGA).toBeDisabled();

    const buyerB = await context.newPage();
    await buyerB.goto(sessionB.data.selection_url);
    await expect(buyerB.getByRole('heading', { name: reviewNames.event })).toBeVisible();
    const allA = page.locator('button.seat');
    const availableIndex = await allA.evaluateAll((seats) =>
      seats.findIndex((seat) => seat.classList.contains('available')),
    );
    expect(availableIndex).toBeGreaterThanOrEqual(0);
    const seatA = allA.nth(availableIndex);
    const seatB = buyerB.locator('button.seat').nth(availableIndex);

    await seatB.click();
    await buyerB.getByRole('button', { name: 'Hold tickets' }).click();
    await expect(buyerB.getByRole('heading', { name: 'Your tickets are held' })).toBeVisible();
    await expect(seatA).toBeDisabled({ timeout: 30_000 });
    await expect(page.getByText('That seat is no longer available.')).toHaveCount(0);
    await screenshot(page, '05-selector-realtime-unselected-seat-disabled');

    await buyerB.getByRole('button', { name: 'Change selection' }).click();
    await expect(seatA).toBeEnabled({ timeout: 30_000 });
    await seatA.click();
    await seatB.click();
    await buyerB.getByRole('button', { name: 'Hold tickets' }).click();
    await expect(buyerB.getByRole('heading', { name: 'Your tickets are held' })).toBeVisible();
    await expect(
      page.getByText('That seat is no longer available. Choose another seat to continue.'),
    ).toBeVisible({ timeout: 30_000 });
    await screenshot(page, '05-selector-realtime-selected-seat-conflict');
    await buyerB.getByRole('button', { name: 'Change selection' }).click();
    await buyerB.close();
  });

  await test.step('Desktop Scanner remains manual-only before authenticated Event scope', async () => {
    await page.goto(urls.scanner);
    await page.evaluate(() => {
      localStorage.clear();
      sessionStorage.clear();
    });
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Scanner sign in' })).toBeVisible();
    await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Open camera' })).toHaveCount(0);
    await screenshot(page, '05-security-device-failure-modes');
  });

  await saveVideo(page, '05-security-device-failure-modes.webm');
});
