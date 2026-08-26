import { expect, test, type Page } from '@playwright/test';

const urls = {
  reviewer: process.env.LIVE_REVIEWER_URL ?? 'http://127.0.0.1:4178',
  partner: process.env.LIVE_PARTNER_DEMO_URL ?? 'https://127.0.0.1:45991',
  scanner: process.env.LIVE_SCANNER_URL ?? 'http://localhost:54472',
  docs: process.env.LIVE_DOCS_URL ?? 'http://localhost:54473',
  api: process.env.LIVE_API_URL ?? 'http://localhost:58480',
};
const eventName = process.env.LIVE_REVIEW_EVENT_NAME ?? 'Championship Night · acceptance';
const partnerCredential = process.env.PARTNER_DEMO_CREDENTIAL ?? '';

async function manualScan(page: Page, payload: string) {
  await page.getByRole('button', { name: 'Enter code manually' }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Enter code manually' });
  await dialog.getByLabel('Ticket code').fill(payload);
  await dialog.getByRole('button', { name: 'Check ticket' }).click();
}

test('Reviewer Hub to real Partner checkout, hosted QR, Scanner and Docs', async ({ page }) => {
  expect(partnerCredential, 'PARTNER_DEMO_CREDENTIAL must be supplied server-side').toBeTruthy();

  await test.step('Enter through the configured Reviewer Hub', async () => {
    await page.goto(urls.reviewer);
    await expect(
      page.getByRole('heading', {
        name: 'One inventory truth across multiple ticketing platforms.',
      }),
    ).toBeVisible();
    await page.getByRole('link', { name: 'Start guided review' }).click();
    const partnerLink = page.getByRole('link', { name: 'Open Demo Partner →' });
    await expect(partnerLink).toHaveAttribute('href', urls.partner);
    await page.goto(urls.partner);
  });

  await test.step('Discover the real Event and open the TktSync Selector', async () => {
    const eventCard = page.locator('.event-card').filter({ hasText: eventName });
    await expect(eventCard).toBeVisible();
    await eventCard.getByRole('link', { name: 'View event →' }).click();
    await expect(page.getByRole('heading', { name: eventName })).toBeVisible();
    await page.getByRole('button', { name: 'Choose tickets' }).click();
    await expect(page.getByRole('heading', { name: eventName })).toBeVisible();
    await expect(page).toHaveURL(/localhost:54471\/s$/);
  });

  await test.step('Hold real inventory and return by secure form POST', async () => {
    const seat = page.locator('button.seat.available').first();
    await expect(seat).toBeVisible();
    await seat.click();
    await page.getByRole('button', { name: 'Hold tickets' }).click();
    await expect(page.getByRole('heading', { name: 'Your tickets are held' })).toBeVisible();
    await page.getByRole('button', { name: 'Continue to checkout' }).click();
    await expect(page.getByRole('heading', { name: 'Review your order' })).toBeVisible();
    expect(page.url()).not.toMatch(/reservation|token/i);
  });

  let ticketId = '';
  await test.step('Begin checkout, simulate Partner payment and confirm the real Reservation', async () => {
    await page.getByRole('button', { name: 'Continue to payment' }).click();
    await expect(page.getByRole('heading', { name: 'Demo payment' })).toBeVisible();
    await page.getByRole('button', { name: 'Simulate successful payment' }).click();
    await expect(page.getByRole('heading', { name: 'Your ticket is ready.' })).toBeVisible();
    const publicID = page.locator('.ticket-info code').first();
    ticketId = (await publicID.innerText()).trim();
    expect(ticketId).toMatch(/^tkt_/);
    const hostedURL = await page.getByRole('img', { name: /Entry QR code/ }).getAttribute('src');
    expect(hostedURL).toMatch(/^http:\/\/localhost:58480\/api\/v1\/ticket-qr\/tqp1_/);
    const hosted = await page.request.get(hostedURL!);
    expect(hosted.status()).toBe(200);
    expect(hosted.headers()['content-type']).toBe('image/svg+xml');
  });

  const credentialResponse = await fetch(
    `${urls.api}/api/v1/partner/tickets/${encodeURIComponent(ticketId)}/credential`,
    { headers: { Authorization: `Bearer ${partnerCredential}` } },
  );
  expect(credentialResponse.status).toBe(200);
  const credential = (await credentialResponse.json()) as { qr_payload: string };
  expect(credential.qr_payload).toMatch(/^qr1\./);

  await test.step('Admit once and reject the duplicate in the real Scanner', async () => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgentData', {
        configurable: true,
        value: { mobile: true },
      });
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(urls.scanner);
    await expect(page.getByRole('heading', { name: 'Choose an event to scan' })).toBeVisible();
    await page.getByRole('button', { name: new RegExp(`Select ${eventName}`) }).click();
    await manualScan(page, credential.qr_payload);
    await expect(page.getByRole('heading', { name: 'Admit guest' })).toBeVisible();
    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await manualScan(page, credential.qr_payload);
    await expect(page.getByRole('heading', { name: 'Already checked in' })).toBeVisible();
  });

  await test.step('Return to the Hub and open Developer Docs', async () => {
    await page.goto(urls.reviewer);
    await page.getByRole('button', { name: /Full review/ }).click();
    const docsLink = page.getByRole('link', { name: 'Open Developer Docs →' });
    await expect(docsLink).toHaveAttribute('href', urls.docs);
    await page.goto(urls.docs);
    await expect(page.getByText('Partner API', { exact: true }).first()).toBeVisible();
  });
});
