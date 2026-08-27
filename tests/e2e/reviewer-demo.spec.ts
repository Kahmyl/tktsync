import { expect, test } from '@playwright/test';

test('Reviewer Hub presents one complete assessment walkthrough in order', async ({ page }) => {
  await page.goto('http://127.0.0.1:4177');
  await expect(
    page.getByRole('heading', { name: 'Follow one ticket from setup to the gate.' }),
  ).toBeVisible();
  await expect(page.getByText('1 of 29').first()).toBeVisible();
  await expect(
    page.getByRole('complementary', { name: 'Review phases' }).getByRole('button'),
  ).toHaveCount(7);
  await expect(page.getByRole('link', { name: 'Source code' }).first()).toHaveAttribute(
    'href',
    'https://github.com/Kahmyl/tktsync',
  );

  await page.getByRole('button', { name: '1 Admin setup' }).click();
  await expect(page.getByRole('heading', { name: 'Review the Dashboard' })).toBeVisible();
  const adminCTA = page.getByRole('link', { name: 'Open Admin Console' });
  await expect(adminCTA).toHaveAttribute('href', 'https://admin.example.test');
  await page.getByRole('button', { name: 'Next →' }).click();
  await expect(page.getByRole('heading', { name: 'Create the venue' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open Venues' })).toHaveAttribute(
    'href',
    'https://admin.example.test/venues',
  );
  await page.getByRole('button', { name: '2 Partner' }).click();
  await expect(page.getByText('Not part of TktSync', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Choose the Partner storefront' })).toBeVisible();
});

test('Reviewer Hub and Partner Demo have no document overflow at mobile widths', async ({
  page,
}) => {
  await page.setViewportSize({ width: 360, height: 800 });
  for (const url of ['http://127.0.0.1:4177', 'http://127.0.0.1:4180']) {
    await page.goto(url);
    const width = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      client: document.documentElement.clientWidth,
    }));
    expect(width.scroll).toBeLessThanOrEqual(width.client);
  }
});

test('Demo Partner distinguishes deployment configuration from invalid credentials', async ({
  page,
}) => {
  await page.goto('http://127.0.0.1:4180/?connection=configuration');
  await expect(
    page.getByText(
      'This is a deployment configuration issue, not a problem with your Partner credential.',
    ),
  ).toBeVisible();
  await page.goto('http://127.0.0.1:4180/?connection=invalid');
  await expect(page.getByText('The TktSync API rejected that credential.')).toBeVisible();
});

test('Demo Partner renders real-contract Events and starts selection through its BFF', async ({
  page,
}) => {
  let selectionBody = '';
  await page.route('**/demo-api/connections', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        items: [{ id: 'ptr_demo', name: 'Demo Partner', active: true }],
      }),
    }),
  );
  await page.route('**/demo-api/events', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        connection: { id: 'ptr_demo', name: 'Demo Partner' },
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
  await expect(
    page.getByRole('heading', { name: 'Choose the Partner for this storefront.' }),
  ).toBeVisible();
  await expect(page.getByText('Demo Partner Application')).toHaveCount(0);
  await expect(page.getByText(/Northstar/i)).toHaveCount(0);
  await page.goto('http://127.0.0.1:4180/events');
  await expect(page.getByText('Demo Partner Application')).toBeVisible();
  await expect(
    page.getByRole('link', { name: /Demo Partner Demo ticket storefront/ }),
  ).toBeVisible();
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
  await page.route('**/demo-api/connections', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        items: [{ id: 'ptr_demo', name: 'Demo Partner', active: true }],
      }),
    }),
  );
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
  await expect(page.getByText('VIP Reserved · Row A · Seat 12')).toBeVisible();
  await expect(page.getByText('Meridian Arena')).toBeVisible();
  await expect(page.getByText('DEMO', { exact: true })).toBeVisible();
  await expect(page.getByText('tkt_demo')).toBeVisible();
  await expect(page.getByRole('img', { name: /Entry QR code/ })).toHaveAttribute(
    'src',
    'https://api.example.test/qr',
  );
  expect(calls).toEqual(['begin', 'confirm']);
  expect(await page.evaluate(() => Object.keys(localStorage))).toEqual([]);
});
