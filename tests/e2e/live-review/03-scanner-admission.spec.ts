import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { expect, test, type Page } from '@playwright/test';
import { composeEnvPath, repoRoot, urls } from './config';
import {
  addEntity,
  apiJSON,
  entities,
  getSecret,
  liveCredentials,
  operatorToken,
  reviewNames,
  saveVideo,
  screenshot,
  setSecret,
} from './state';

type ScanResponse = {
  result: string;
  scan_attempt_id: string;
  admission_id?: string;
};

async function manualScan(page: Page, ticketCode: string) {
  await page.getByRole('button', { name: 'Enter code manually' }).first().click();
  const sheet = page.getByRole('dialog', { name: 'Enter code manually' });
  await sheet.getByLabel('Manual admission code').fill(ticketCode);
  await sheet.getByRole('button', { name: 'Check ticket' }).click();
}

async function chooseEvent(page: Page, eventName: string) {
  const option = page.getByRole('button', { name: new RegExp(`Select ${eventName}`) });
  await expect(option).toBeVisible();
  await option.click();
}

async function changeEvent(page: Page) {
  await page.getByRole('button', { name: 'Scanner settings' }).click();
  await page
    .getByRole('dialog', { name: 'Scanner settings' })
    .getByRole('button', { name: /Change event/ })
    .click();
  await expect(page.getByRole('heading', { name: 'Choose an event to scan' })).toBeVisible();
}

test.use({ trace: 'off' });

test('03 Scanner admission', async ({ page, context }) => {
  test.setTimeout(240_000);
  await page.addInitScript(() => {
    const style = document.createElement('style');
    style.textContent =
      'input[type="email"],input[type="password"],#ticket-code,video{filter:blur(16px)!important;user-select:none!important}';
    document.documentElement.append(style);
  });

  await test.step('Verify desktop detection requires a phone and never offers a fake camera', async () => {
    await page.goto(urls.scanner);
    await page.evaluate(() => {
      localStorage.clear();
      sessionStorage.clear();
    });
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Scanner sign in' })).toBeVisible();
    await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toBeVisible();
    await expect(page.getByText(/rear camera is required/)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Open camera' })).toHaveCount(0);
    await screenshot(page, '03-scanner-desktop-phone-guidance');
  });

  await test.step('Sign in through real Supabase on a browser identified as a phone', async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgentData', {
        configurable: true,
        value: { mobile: true },
      });
    });
    await context.grantPermissions(['camera'], { origin: urls.scanner });
    await page.reload();
    await expect(page.getByText('Use a phone to scan tickets', { exact: true })).toHaveCount(0);
    const credentials = await liveCredentials();
    await page.getByLabel('Email').fill(credentials.email);
    await page.getByLabel('Password').fill(credentials.password);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('heading', { name: 'Choose an event to scan' })).toBeVisible();
    const retry = page.getByRole('button', { name: 'Try again' });
    for (let attempt = 0; attempt < 8; attempt += 1) {
      if (
        await page
          .getByRole('heading', { name: 'Your events' })
          .isVisible()
          .catch(() => false)
      )
        break;
      if (await retry.isVisible().catch(() => false)) await retry.click();
      await page.waitForTimeout(2_000);
    }
    await expect(page.getByRole('heading', { name: 'Your events' })).toBeVisible();
    await expect(page.getByLabel('Event ID')).toHaveCount(0);
    await expect(page.locator('body')).not.toContainText(
      /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i,
    );
    await screenshot(page, '03-scanner-real-event-picker');
  });

  const ticket1 = await getSecret('ticket1ID');
  const ticket2 = await getSecret('ticket2ID');
  const ticket2QR = await getSecret('ticket2QR');
  const ledger = await entities();
  const primaryEvent = ledger.events[0];
  const wrongEvent = ledger.events[1];
  expect(primaryEvent).toMatch(/^evt_/);
  expect(wrongEvent).toMatch(/^evt_/);

  const ticket1State = await apiJSON<{
    admission_id?: string;
    admission_state: 'ACTIVE' | 'REVERSED' | null;
  }>(`/api/v1/admin/tickets/${ticket1}`, { token: await operatorToken() });
  if (ticket1State.data.admission_state === 'ACTIVE' && ticket1State.data.admission_id) {
    await apiJSON(`/api/v1/admin/admissions/${ticket1State.data.admission_id}/reverse`, {
      method: 'POST',
      token: await operatorToken(),
      idempotencyKey: randomUUID(),
      body: { reason: 'Live review retry reset' },
    });
  }

  await test.step('Decode a real issued ticket from the real camera feed and admit the guest', async () => {
    await chooseEvent(page, reviewNames.event);
    await expect(page.getByText('Point the camera at the ticket QR code')).toBeVisible();
    const scanResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith('/api/v1/admission/scans') &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Open camera' }).click();
    const response = await scanResponse;
    expect(response.status()).toBe(200);
    const decision = (await response.json()) as ScanResponse;
    expect(decision.result).toBe('ADMITTED');
    expect(decision.admission_id).toMatch(/^adm_/);
    await addEntity('admissions', decision.admission_id!);
    await expect(page.getByRole('heading', { name: 'Admit guest' })).toBeVisible();
    await expect(page.locator('.decision-overlay')).toContainText('Main Floor');
    await screenshot(page, '03-scanner-camera-admitted');
  });

  await test.step('Repeat the ticket and preserve the real duplicate decision', async () => {
    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await expect(page.getByRole('heading', { name: 'Already checked in' })).toBeVisible();
    await expect(page.locator('.decision-overlay')).toContainText('First admitted at');
    await screenshot(page, '03-scanner-already-admitted');
  });

  await test.step('Reject a malformed ticket through the real admission service', async () => {
    await changeEvent(page);
    await chooseEvent(page, reviewNames.event);
    await manualScan(page, 'not-a-valid-ticket-code');
    await expect(page.getByRole('heading', { name: 'Ticket not valid' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Admit guest' })).toHaveCount(0);
  });

  await test.step('Remain fail-closed during a real browser network outage', async () => {
    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    execFileSync('docker', ['compose', '--env-file', composeEnvPath, 'stop', 'api'], {
      cwd: repoRoot,
      stdio: 'ignore',
    });
    try {
      await manualScan(page, ticket2QR);
      await expect(page.getByRole('heading', { name: "Can't verify ticket" })).toBeVisible();
      await expect(
        page.getByText('Do not admit this guest until the ticket can be verified.', {
          exact: false,
        }),
      ).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Admit guest' })).toHaveCount(0);
      await screenshot(page, '03-scanner-network-fail-closed');
    } finally {
      execFileSync(
        'docker',
        [
          'compose',
          '--env-file',
          composeEnvPath,
          'up',
          '-d',
          '--wait',
          '--wait-timeout',
          '60',
          'api',
        ],
        { cwd: repoRoot, stdio: 'ignore' },
      );
    }
  });

  await test.step('Return a real wrong-Event decision with human-readable Event copy', async () => {
    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await changeEvent(page);
    const otherEvent = page
      .getByRole('region', { name: 'Your events' })
      .getByRole('button', { name: /Select / })
      .filter({ hasNotText: reviewNames.event })
      .first();
    await expect(otherEvent).toBeVisible();
    const otherEventName = (await otherEvent.locator('.event-card-title').innerText()).trim();
    await otherEvent.click();
    await manualScan(page, ticket2QR);
    await expect(page.getByRole('heading', { name: 'Wrong event' })).toBeVisible();
    await expect(page.getByText(`This ticket is not valid for ${otherEventName}.`)).toBeVisible();
    await expect(page.locator('body')).not.toContainText(primaryEvent);
    await expect(page.locator('body')).not.toContainText(wrongEvent);
    await screenshot(page, '03-scanner-wrong-event');
  });

  await test.step('Reject a genuinely superseded credential and keep recent outcomes readable', async () => {
    const partnerCredential = await getSecret('partnerCredential');
    expect(ticket2).toMatch(/^tkt_/);
    await apiJSON(`/api/v1/partner/tickets/${ticket2}/credentials/reissue`, {
      method: 'POST',
      partnerCredential,
      idempotencyKey: randomUUID(),
      body: {},
    });
    const replacement = await apiJSON<{ qr_payload: string }>(
      `/api/v1/partner/tickets/${ticket2}/credential`,
      { partnerCredential },
    );
    await setSecret('ticket2QRActive', replacement.data.qr_payload);

    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await changeEvent(page);
    await chooseEvent(page, reviewNames.event);
    await manualScan(page, ticket2QR);
    await expect(page.getByRole('heading', { name: 'Ticket not valid' })).toBeVisible();
    await page.getByRole('button', { name: 'Scan next ticket' }).click();
    await page.getByRole('button', { name: 'Recent scans' }).click();
    const recent = page.getByRole('dialog', { name: 'Recent scans' });
    await expect(recent.getByText('Admit guest')).toBeVisible();
    await expect(recent.getByText('Already checked in')).toBeVisible();
    await expect(recent.getByText("Can't verify ticket")).toBeVisible();
    await expect(recent).not.toContainText(/\b(?:evt|tkt|adm|scan)_[a-z0-9_-]+\b/i);
    await screenshot(page, '03-scanner-recent-human-outcomes');
    await recent.getByRole('button', { name: 'Close Recent scans' }).click();
  });

  await test.step('Sign out through Scanner settings', async () => {
    await page.getByRole('button', { name: 'Scanner settings' }).click();
    await page
      .getByRole('dialog', { name: 'Scanner settings' })
      .getByRole('button', {
        name: 'Sign out',
      })
      .click();
    await expect(page.getByRole('heading', { name: 'Scanner sign in' })).toBeVisible();
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });

  await saveVideo(page, '03-scanner-admission.webm');
});
