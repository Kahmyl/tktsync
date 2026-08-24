import { expect, test } from '@playwright/test';
import { recordIssue, saveVideo, screenshot } from './state';
import { urls } from './config';

test('04 Admin support and reporting', async ({ page }) => {
  test.setTimeout(90_000);
  for (const route of ['/tickets', '/admissions', '/reports', '/integrations', '/account']) {
    await test.step(`Protected Admin route ${route} remains behind real authentication`, async () => {
      await page.goto(`${urls.admin}${route}`);
      await expect(page).toHaveURL(/\/sign-in$/);
      await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
    });
  }
  await screenshot(page, '04-admin-operations-auth-boundary');
  await recordIssue(
    'LIVE-OPERATIONS-001 — Post-sale Admin operations not reachable',
    'Tickets, admissions, reports, integrations, and account routes all correctly redirected the unauthenticated live browser to sign-in. Without a real reusable operator credential and without tickets created by the blocked upstream workflow, their read/write behavior cannot be truthfully certified in this run.',
  );
  await saveVideo(page, '04-admin-support-reporting.webm');
});
