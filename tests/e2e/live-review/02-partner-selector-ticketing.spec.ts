import { expect, test } from '@playwright/test';
import { recordIssue, saveVideo, screenshot } from './state';
import { urls } from './config';

test('02 Partner Docs to Selector ticketing', async ({ page }) => {
  test.setTimeout(120_000);
  await page.addStyleTag({
    content:
      'input[type="password"], [data-sensitive], [data-e2e-sensitive]{filter:blur(12px)!important}',
  });

  await test.step('Use the real Developer Docs request workbench against Local', async () => {
    await page.goto(urls.docs);
    await page.getByRole('button', { name: /Search documentation/ }).click();
    await page.getByPlaceholder(/Search endpoints/).fill('Retrieve an Event');
    await page
      .locator('.search-results')
      .getByRole('button', { name: /Retrieve an event/i })
      .click();
    await expect(page.getByRole('heading', { name: 'Retrieve an Event' })).toBeVisible();
    await expect(page.getByLabel('API environment')).toHaveValue('local');
    await page.getByRole('button', { name: /Set Test Credential/ }).click();
    const dialog = page.getByRole('dialog', { name: 'Connect the request console' });
    await dialog.getByLabel('Partner API key').fill('tkp_live_review_intentionally_invalid');
    await dialog.getByRole('button', { name: 'Use for this tab' }).click();
    expect(await page.evaluate(() => Object.keys(localStorage))).toEqual([]);
    expect(await page.evaluate(() => Object.keys(sessionStorage))).toEqual([]);
    await page.getByLabel(/event_id/).fill('evt_AAAAAAAAAAAAAAAAAAAAAA');
    await page.getByRole('button', { name: 'Send request' }).click();
    await expect(page.getByText(/401/)).toBeVisible();
    await screenshot(page, '02-docs-real-unauthorized-response');
  });

  await test.step('Prove selection-session creation cannot be fabricated without real Partner setup', async () => {
    await page.goto(`${urls.docs}/api/selection-sessions/create`);
    await page.getByRole('button', { name: /Set Test Credential/ }).click();
    const dialog = page.getByRole('dialog', { name: 'Connect the request console' });
    await dialog.getByLabel('Partner API key').fill('tkp_live_review_intentionally_invalid');
    await dialog.getByRole('button', { name: 'Use for this tab' }).click();
    const bodyFields = page.locator('.body-fields');
    await bodyFields
      .getByText('event_id', { exact: true })
      .locator('..')
      .getByRole('textbox')
      .fill('evt_AAAAAAAAAAAAAAAAAAAAAA');
    await bodyFields
      .getByText('return_url', { exact: true })
      .locator('..')
      .getByRole('textbox')
      .fill('https://127.0.0.1:45991/checkout');
    await page.getByRole('button', { name: 'Send request' }).click();
    await expect(page.getByText(/401/)).toBeVisible();
  });

  await test.step('Selector bare and invalid links fail closed against the live API', async () => {
    await page.goto(urls.selector);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
    await expect(page.locator('body')).not.toContainText('Championship Night');
    await screenshot(page, '02-selector-bare-fails-closed');

    // Force a new document load. A bare Selector followed only by a hash change is a
    // same-document navigation and intentionally does not remount the React application.
    await page.goto(urls.docs);
    await page.goto(`${urls.selector}/#not-a-real-capability`);
    await expect(page).toHaveURL(`${urls.selector}/`);
    await expect(
      page.getByRole('heading', { name: 'This ticket selection link is no longer available.' }),
    ).toBeVisible();
    await expect(page.url()).not.toContain('not-a-real-capability');
    await screenshot(page, '02-selector-invalid-capability-consumed');
  });

  await recordIssue(
    'LIVE-TICKETING-001 — Cross-application ticket lifecycle blocked by real operator authentication',
    'Developer Docs executed real local requests and a syntactically valid but incorrect Partner credential received a real 401. A Partner, credential, Event access, and selection session cannot be created without the blocked Admin authentication prerequisite. Bare and invalid Selector links were still verified fail-closed against the real app/API; no fake selection URL, hold, reservation, ticket, or checkout token was created.',
  );
  await saveVideo(page, '02-partner-selector-ticketing.webm');
});
