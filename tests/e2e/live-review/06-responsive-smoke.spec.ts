import { randomUUID } from 'node:crypto';
import { expect, test, type Page } from '@playwright/test';
import { urls } from './config';
import { apiJSON, entities, getSecret, reviewNames, saveVideo, screenshot } from './state';

const viewports = [
  [1440, 900],
  [1280, 800],
  [1024, 768],
  [768, 1024],
  [430, 932],
  [390, 844],
  [360, 800],
] as const;

async function contained(page: Page) {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
}

test.use({ trace: 'off' });

test('06 Responsive live smoke', async ({ page }) => {
  test.setTimeout(300_000);
  const ledger = await entities();
  const eventId = ledger.events[0];
  const partnerCredential = await getSecret('partnerCredential');
  const selection = await apiJSON<{
    selection_session_id: string;
    selection_url: string;
    expires_at: string;
  }>('/api/v1/partner/selection-sessions', {
    method: 'POST',
    partnerCredential,
    idempotencyKey: randomUUID(),
    body: {
      event_id: eventId,
      return_url: 'https://127.0.0.1:45991/checkout',
      buyer_session_ref: `responsive-${randomUUID()}`,
    },
  });

  for (const [width, height] of viewports) {
    await page.setViewportSize({ width, height });

    await test.step(`Admin Dashboard at ${width}x${height}`, async () => {
      await page.goto(urls.admin);
      await expect(
        page.getByRole('heading', { name: 'Upcoming events', exact: true }),
      ).toBeVisible();
      await contained(page);
      await screenshot(page, `responsive-admin-dashboard-${width}x${height}`);
    });

    await test.step(`Admin Event detail at ${width}x${height}`, async () => {
      await page.goto(`${urls.admin}/events/${eventId}`);
      await expect(page.getByRole('heading', { name: reviewNames.event })).toBeVisible();
      await expect(
        page.getByRole('button', { name: /Pause sales|Close sales|Reopen sales/ }),
      ).toBeVisible();
      await contained(page);
      await screenshot(page, `responsive-admin-event-${width}x${height}`);
    });

    await test.step(`Developer Docs endpoint at ${width}x${height}`, async () => {
      await page.goto(`${urls.docs}/api/events/retrieve`);
      await expect(page.getByRole('heading', { name: 'Retrieve an Event' })).toBeVisible();
      await expect(
        page.getByRole('button', { name: /Set Test Credential|Set credential to send/ }).first(),
      ).toBeVisible();
      await contained(page);
      await screenshot(page, `responsive-docs-request-${width}x${height}`);
    });

    await test.step(`Active Selector at ${width}x${height}`, async () => {
      await page.goto(selection.data.selection_url);
      await expect(page.getByRole('heading', { name: reviewNames.event })).toBeVisible();
      await expect(page.locator('button.seat').first()).toBeVisible();
      if (width <= 768) {
        await expect(page.getByText('No tickets selected')).toBeVisible();
      } else {
        await expect(page.getByText('Choose your tickets', { exact: true }).last()).toBeVisible();
      }
      await contained(page);
      await screenshot(page, `responsive-selector-active-${width}x${height}`);
    });

    await test.step(`Scanner Event selection at ${width}x${height}`, async () => {
      await page.goto(urls.scanner);
      await expect(page.getByRole('heading', { name: 'Choose an event to scan' })).toBeVisible();
      await expect(
        page.getByRole('button', { name: new RegExp(`Select ${reviewNames.event}`) }),
      ).toBeVisible();
      await contained(page);
      await screenshot(page, `responsive-scanner-events-${width}x${height}`);
    });
  }

  await saveVideo(page, '06-responsive-smoke.webm');
});
