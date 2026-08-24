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
