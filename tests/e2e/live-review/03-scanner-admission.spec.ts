import { expect, test } from '@playwright/test';
import { recordIssue, saveVideo, screenshot } from './state';
import { urls } from './config';

test('03 Scanner admission', async ({ page }) => {
  test.setTimeout(90_000);

  await test.step('Verify desktop detection does not pretend to be a phone', async () => {
    await page.goto(urls.scanner);
    await expect(page.getByRole('heading', { name: 'Scanner sign in' })).toBeVisible();
    await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toBeVisible();
    await expect(page.getByText(/rear camera is required/)).toBeVisible();
    await screenshot(page, '03-scanner-desktop-phone-guidance');
  });

  await test.step('Verify phone detection is based on device identity, not narrow width', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();
    await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toBeVisible();

    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgentData', {
        configurable: true,
        value: { mobile: true },
      });
    });
    await page.reload();
    await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toHaveCount(0);
  });

  await test.step('Use real Supabase and preserve its authentication failure', async () => {
    await page.getByLabel('Email').fill('live-review-blocked@invalid.example');
    await page.getByLabel('Password').fill('not-a-real-credential');
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('alert')).toContainText(/incorrect|credential/i);
    await screenshot(page, '03-scanner-real-auth-blocked');
  });

  await recordIssue(
    'LIVE-SCANNER-001 — Camera and admission proof blocked before Event selection',
    'Desktop and phone-device detection branches were exercised in the real Scanner. The configured Supabase project rejected the real login attempt, and no reusable operator credential exists locally. Therefore authorized Event selection, rear-camera media capture, actual QR decoding, admission/duplicate/wrong-Event/void outcomes, recent scans, and fail-closed API outage proof are blocked at authentication. No admission response or QR decoder was mocked.',
  );
  await saveVideo(page, '03-scanner-admission.webm');
});
