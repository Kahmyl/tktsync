import { expect, test } from '@playwright/test';
import { saveVideo, screenshot } from './state';
import { urls } from './config';

test('05 Security, device, and failure modes', async ({ page }) => {
  test.setTimeout(120_000);

  await test.step('Admin blocks unauthenticated protected navigation', async () => {
    await page.goto(`${urls.admin}/events`);
    await expect(page).toHaveURL(/\/sign-in$/);
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
  });

  await test.step('Selector leaks no Event data for bare or invalid capability', async () => {
    await page.goto(urls.selector);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
    await expect(page.locator('body')).not.toContainText(/E2E .* Event/);
    await page.goto(urls.docs);
    await page.goto(`${urls.selector}/#expired-or-invalid-live-review-capability`);
    await expect(page).toHaveURL(`${urls.selector}/`);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
  });

  await test.step('Docs proxy rejects wrong Partner credential and cannot expose Admin data', async () => {
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
    const environments = await page
      .getByLabel('API environment')
      .locator('option')
      .allTextContents();
    expect(environments).toEqual(['Local']);
  });

  await test.step('Desktop Scanner remains manual-only before authenticated Event scope', async () => {
    await page.goto(urls.scanner);
    await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Open camera' })).toHaveCount(0);
    await screenshot(page, '05-security-device-failure-modes');
  });

  await saveVideo(page, '05-security-device-failure-modes.webm');
});
