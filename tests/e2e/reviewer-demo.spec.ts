import { expect, test } from '@playwright/test';

test('Reviewer Hub presents one complete assessment walkthrough in order', async ({ page }) => {
  await page.goto('http://127.0.0.1:4177');
  await expect(
    page.getByRole('heading', { name: 'Review the complete TktSync workflow in order.' }),
  ).toBeVisible();
  await expect(page.getByText(/follow Steps 1–6/i)).toBeVisible();
  await expect(page.getByText(/Quick review|Full review/)).toHaveCount(0);

  const steps = page.locator('.steps > li');
  await expect(steps).toHaveCount(6);
  await expect(steps.nth(0)).toContainText('Review Admin Console');
  await expect(steps.nth(1)).toContainText('Review Demo Partner');
  await expect(steps.nth(2)).toContainText('Review TktSync Selector');
  await expect(steps.nth(3)).toContainText('Review Partner Checkout + Ticket');
  await expect(steps.nth(4)).toContainText('Review Scanner');
  await expect(steps.nth(5)).toContainText('Developer / Technical Review');

  await expect(steps.nth(1).getByText('Not part of TktSync', { exact: true })).toBeVisible();
  await expect(
    steps.nth(2).getByRole('link', { name: 'Continue through Demo Partner' }),
  ).toHaveAttribute('href', 'http://127.0.0.1:4180');
  await expect(page.getByRole('link', { name: 'Source code' }).first()).toHaveAttribute(
    'href',
    'https://github.com/Kahmyl/tktsync',
  );

  const adminCTA = steps.nth(0).getByRole('link', { name: 'Open Admin Console' });
  await expect(adminCTA).toHaveAttribute('href', 'https://admin.example.test');
  expect(await adminCTA.evaluate((element) => getComputedStyle(element).paddingInline)).not.toBe(
    '0px',
  );
});

test('Reviewer Hub and Partner Demo have no document overflow at mobile widths', async ({
  page,
}) => {
  await page.setViewportSize({ width: 360, height: 800 });
  for (const url of ['http://127.0.0.1:4177', 'http://127.0.0.1:4180']) {
    if (url.endsWith('4180'))
      await page.route('**/demo-api/events', (route) =>
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ items: [] }),
        }),
      );
    await page.goto(url);
    const width = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      client: document.documentElement.clientWidth,
    }));
    expect(width.scroll).toBeLessThanOrEqual(width.client);
  }
});

test('Demo Partner renders real-contract Events and starts selection through its BFF', async ({
  page,
}) => {
  let selectionBody = '';
  await page.route('**/demo-api/events', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        items: [
          {
            id: 'evt_demo',
            name: 'Championship Night',
            state: 'ON_SALE',
            starts_at: '2027-08-26T18:00:00Z',
            venue_name: 'Meridian Arena',
            starting_price: { amount_minor: 2500000, currency: 'NGN' },
          },
        ],
      }),
    }),
  );
  await page.route('**/demo-api/events/evt_demo', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'evt_demo',
        name: 'Championship Night',
        state: 'ON_SALE',
        starts_at: '2027-08-26T18:00:00Z',
        venue_name: 'Meridian Arena',
        address_text: 'Lagos',
        starting_price: { amount_minor: 2500000, currency: 'NGN' },
      }),
    }),
  );
  await page.route('**/demo-api/events/evt_demo/selection', async (route) => {
    selectionBody = route.request().postData() || '';
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({ selection_url: 'https://selector.example.test/s#opaque' }),
    });
  });
  await page.goto('http://127.0.0.1:4180');
  await expect(page.getByText('Demo Partner Application')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Championship Night' })).toBeVisible();
  await page.getByRole('link', { name: 'View event →' }).click();
  await expect(page.getByRole('button', { name: 'Choose tickets' })).toBeVisible();
  await page.getByRole('button', { name: 'Choose tickets' }).click();
  await expect.poll(() => selectionBody).toBe('{}');
});

test('Demo Partner renders checkout, real lifecycle calls, ticket details and hosted QR', async ({
  page,
}) => {
  const calls: string[] = [];
  const order = {
    event: {
      id: 'evt_demo',
      name: 'Championship Night',
      state: 'ON_SALE',
      starts_at: '2027-08-26T18:00:00Z',
      venue_name: 'Meridian Arena',
    },
    reservation: {
      id: 'res_demo',
      status: 'HELD',
      hold_expires_at: '2099-01-01T00:10:00Z',
      server_time: '2099-01-01T00:00:00Z',
      items: [
        {
          id: 'ritem_demo',
          inventory_kind: 'RESERVED',
          inventory_id: 'inv_demo',
          quantity: 1,
          unit_amount_minor: 7_500_000,
          currency: 'NGN',
          price_tier_label: 'VIP Reserved',
          display: { section: 'VIP Reserved', row: 'A', seat: '12' },
        },
      ],
      total: { amount_minor: 7_500_000, currency: 'NGN' },
    },
  };
  await page.route('**/demo-api/checkout', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(order) }),
  );
  await page.route('**/demo-api/checkout/begin', async (route) => {
    calls.push('begin');
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ checkout_attempt: { id: 'chk_demo' } }),
    });
  });
  await page.route('**/demo-api/checkout/confirm', async (route) => {
    calls.push('confirm');
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'CONFIRMED' }),
    });
  });
  await page.route('**/demo-api/ticket', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...order,
        confirmation: {
          sale: {
            id: 'sale_demo',
            confirmed_at: '2027-08-26T18:02:00Z',
            partner_order_ref: 'NS-DEMO',
          },
          tickets: [{ id: 'tkt_demo', status: 'ACTIVE' }],
        },
        credentials: [
          { ticket_id: 'tkt_demo', status: 'ACTIVE', qr_url: 'https://api.example.test/qr' },
        ],
        scanner_url: 'https://scanner.example.test',
      }),
    }),
  );

  await page.goto('http://127.0.0.1:4180/checkout');
  await expect(page.getByRole('heading', { name: 'Review your order' })).toBeVisible();
  expect(page.url()).not.toContain('reservation_token');
  await page.getByRole('button', { name: 'Continue to payment' }).click();
  await expect(page.getByRole('heading', { name: 'Demo payment' })).toBeVisible();
  await page.getByRole('button', { name: 'Simulate successful payment' }).click();
  await expect(page.getByRole('heading', { name: 'Your ticket is ready.' })).toBeVisible();
  await expect(page.getByText('tkt_demo')).toBeVisible();
  await expect(page.getByRole('img', { name: /Entry QR code/ })).toHaveAttribute(
    'src',
    'https://api.example.test/qr',
  );
  expect(calls).toEqual(['begin', 'confirm']);
  expect(await page.evaluate(() => Object.keys(localStorage))).toEqual([]);
});
