import { expect, test } from '@playwright/test';

const base = 'http://127.0.0.1:4176';

test('search navigates to one deep Partner endpoint page', async ({ page }) => {
  await page.goto(base);
  await expect(page.getByRole('heading', { name: 'Partner API' })).toBeVisible();
  await page.getByRole('button', { name: /Search documentation/ }).click();
  await page.getByPlaceholder(/Search endpoints/).fill('availability');
  await page
    .locator('.search-results')
    .getByRole('button', { name: /Retrieve availability/ })
    .click();
  await expect(page).toHaveURL(/\/api\/events\/availability$/);
  await expect(
    page.getByText('/api/v1/partner/events/{event_id}/availability', { exact: true }).first(),
  ).toBeVisible();
});

test('documents event discovery and Partner-facing return URL onboarding', async ({ page }) => {
  await page.goto(`${base}/api/events`);
  await expect(page.getByRole('heading', { name: 'List accessible events' })).toBeVisible();
  await expect(page.getByText(/discover event IDs before retrieving layout/i)).toBeVisible();

  await page.goto(`${base}/guides/embedded-selector`);
  await page
    .locator('.left-nav')
    .getByRole('button', { name: /Create a selection session/ })
    .click();
  await expect(
    page.locator('.lede').getByText(/registered during Partner integration onboarding/),
  ).toBeVisible();
  await expect(
    page.locator('input[value="https://partner.example/checkout/return"]'),
  ).toBeVisible();
});

test('client navigation from a bodyless endpoint to selection creation does not blank', async ({
  page,
}) => {
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.goto(`${base}/api/events`);
  await page
    .locator('.left-nav')
    .getByRole('button', { name: /Create a selection session/ })
    .click();

  await expect(page).toHaveURL(/\/api\/selection-sessions\/create$/);
  await expect(page.getByRole('heading', { name: 'Create a selection session' })).toBeVisible();
  await expect(page.locator('.body-fields')).toContainText('return_url');
  expect(pageErrors).toEqual([]);
});

test('teaches the complete Selector checkout and ticket-delivery workflows', async ({ page }) => {
  await page.goto(`${base}/workflows`);
  await expect(
    page.getByRole('heading', { name: 'Build ticket sales with TktSync' }),
  ).toBeVisible();
  await expect(page.getByText(/Your website or app sells tickets for Events/)).toBeVisible();
  await expect(page.getByText(/Show Events for sale/)).toBeVisible();
  await expect(page.getByText(/Customer chooses an Event/)).toBeVisible();
  await page.getByRole('button', { name: /Recommended: use the TktSync Selector/ }).click();
  await expect(
    page.getByRole('heading', { name: 'Add ticket selection and checkout' }),
  ).toBeVisible();
  await expect(page.getByText(/reservation_id and reservation_token/).first()).toBeVisible();
  await expect(page.getByText(/application\/x-www-form-urlencoded fields/)).toBeVisible();
  await expect(page.getByText(/token is intentionally absent from the browser URL/)).toBeVisible();
  await expect(page.locator('.workflow-flow')).toHaveCount(1);

  await page.getByRole('button', { name: /Retrieve and deliver each ticket/ }).click();
  await expect(page).toHaveURL(/\/workflows\/tickets$/);
  await expect(page.getByRole('heading', { name: 'Deliver a visible ticket' })).toBeVisible();
  await expect(
    page.getByText(/Do not create a QR code whose contents are the qr_url/),
  ).toBeVisible();

  await page.getByRole('button', { name: 'Retrieve credential and hosted URL' }).click();
  await expect(page).toHaveURL(/\/api\/tickets\/retrieve$/);
  await expect(page.getByRole('heading', { name: 'Retrieve a ticket' })).toBeVisible();
});

for (const [width, height] of [
  [360, 800],
  [390, 844],
  [430, 932],
] as const) {
  test(`workflow guide remains readable at ${width}x${height}`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.goto(`${base}/workflows/selector`);
    await expect(
      page.getByRole('heading', { name: 'Add ticket selection and checkout' }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      ),
    ).toBe(true);
  });
}

test('credential remains memory-only and a mocked request preserves response status', async ({
  page,
}) => {
  const credential = 'partner-secret-never-persist';
  await page.route('**/__docs-exec/api/v1/partner/events/*', async (route) => {
    expect(route.request().headers().authorization).toBe(`Bearer ${credential}`);
    await route.fulfill({
      status: 200,
      headers: { 'content-type': 'application/json', 'x-request-id': 'req_docs' },
      body: JSON.stringify({ id: 'evt_example', status: 'ON_SALE' }),
    });
  });
  await page.goto(`${base}/api/events/retrieve`);
  await page.getByRole('button', { name: 'Set credential to send' }).click();
  await page.getByLabel('Partner API key').fill(credential);
  await page.getByRole('button', { name: 'Use for this tab' }).click();
  await page.getByRole('button', { name: 'Send request' }).click();
  await expect(page.getByText('200 OK')).toBeVisible();
  await expect(page.getByText(/evt_example/).last()).toBeVisible();
  expect(
    await page.evaluate(() => ({
      local: Object.keys(localStorage),
      session: Object.keys(sessionStorage),
      cookie: document.cookie,
      href: location.href,
    })),
  ).toEqual({ local: [], session: [], cookie: '', href: `${base}/api/events/retrieve` });
});

test('code languages share request values and the selection persists across endpoints', async ({
  page,
}) => {
  await page.goto(`${base}/api/reservations/create`);
  await page.getByRole('button', { name: 'Code' }).click();
  await page.getByRole('button', { name: 'Node.js' }).click();
  await expect(page.locator('.code-view pre')).toContainText('fetch');
  await page
    .locator('.left-nav')
    .getByRole('button', { name: /Begin checkout/ })
    .click();
  await expect(page.getByRole('button', { name: 'Node.js' })).toHaveClass(/active/);
  await page.getByRole('button', { name: 'Go' }).click();
  await expect(page.locator('.code-view pre')).toContainText('http.NewRequest');
});

test('QR samples request and read SVG instead of assuming JSON', async ({ page }) => {
  await page.goto(`${base}/api/tickets/retrieve-qr`);
  await page.getByRole('button', { name: 'Code' }).click();
  await page.getByRole('button', { name: 'Node.js' }).click();
  await expect(page.locator('.code-view pre')).toContainText('"Accept": "image/svg+xml"');
  await expect(page.locator('.code-view pre')).toContainText('await response.text()');
  await expect(page.locator('.code-view pre')).not.toContainText('await response.json()');
});

test('destructive execution requires explicit confirmation', async ({ page }) => {
  await page.route('**/__docs-exec/api/v1/partner/tickets/**', (route) =>
    route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: '{"code":"UNAUTHORIZED"}',
    }),
  );
  await page.goto(`${base}/api/tickets/void`);
  await page.getByRole('button', { name: 'Set credential to send' }).click();
  await page.getByLabel('Partner API key').fill('test-key');
  await page.getByRole('button', { name: 'Use for this tab' }).click();
  await page.getByRole('button', { name: 'Send request' }).click();
  await expect(page.getByText('Confirm state-changing request')).toBeVisible();
  await page.getByRole('button', { name: 'I understand, send request' }).click();
  await expect(page.getByText(/401/)).toBeVisible();
});

for (const [width, height] of [
  [1600, 1000],
  [1180, 820],
  [1024, 768],
  [900, 800],
  [768, 1024],
  [600, 900],
  [430, 932],
  [390, 844],
  [360, 800],
] as const) {
  test(`responsive endpoint has no document overflow at ${width}x${height}`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.goto(`${base}/api/reservations/create`);
    await expect(page.getByRole('heading', { name: 'Create a reservation' })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      ),
    ).toBe(true);
    if (width <= 760) await expect(page.locator('.workbench')).toHaveCSS('position', 'relative');
  });
}
