import { expect, test } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { urls, webhookReceiverLogPath } from './config';
import {
  apiJSON,
  configureEventPolicy,
  entities,
  getSecret,
  liveCredentials,
  operatorToken,
  queryDatabase,
  reviewNames,
  saveVideo,
  screenshot,
  setSecret,
} from './state';

type TicketDetail = {
  id: string;
  status: 'ACTIVE' | 'VOIDED';
  event_name: string;
  inventory_kind: 'RESERVED' | 'GA';
  display_label: string | null;
  credential_state: 'ACTIVE' | 'SUPERSEDED' | 'REVOKED' | null;
  admission_id?: string;
  admission_state: 'ACTIVE' | 'REVERSED' | null;
};

type InventoryReport = {
  total: {
    capacity: number;
    available: number;
    sold_current: number;
    voided_tickets: number;
  };
};

type ReceiverEntry = { event_type: string; response_status: number; signature_present: boolean };

async function receiverEntries() {
  const raw = await readFile(webhookReceiverLogPath, 'utf8');
  return raw
    .split('\n')
    .filter(Boolean)
    .map((line) => JSON.parse(line) as ReceiverEntry);
}

function parseCSV(text: string) {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let quoted = false;
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (character === '"' && quoted && text[index + 1] === '"') {
      field += '"';
      index += 1;
    } else if (character === '"') quoted = !quoted;
    else if (character === ',' && !quoted) {
      row.push(field);
      field = '';
    } else if (character === '\n' && !quoted) {
      row.push(field.replace(/\r$/, ''));
      rows.push(row);
      row = [];
      field = '';
    } else field += character;
  }
  if (field || row.length) rows.push([...row, field]);
  return rows;
}

test.use({ trace: 'off' });

test('04 Admin support and reporting', async ({ page }) => {
  test.setTimeout(240_000);
  await page.addInitScript(() => {
    const style = document.createElement('style');
    style.textContent =
      'input[type="email"],input[type="password"],#manual-ticket,[data-testid="one-time-secret"],.secret-value{filter:blur(14px)!important;user-select:none!important}';
    document.documentElement.append(style);
  });

  const ledger = await entities();
  const eventId = ledger.events[0];
  const ticketA = await getSecret('ticket1ID');
  const ticketB = await getSecret('ticket2ID');
  const token = await operatorToken();
  expect(eventId).toMatch(/^evt_/);
  expect(ticketA).toMatch(/^tkt_/);
  expect(ticketB).toMatch(/^tkt_/);

  await test.step('Prove real reservation webhook retry and eventual HTTPS delivery', async () => {
    await expect
      .poll(
        async () =>
          (await receiverEntries()).filter((entry) => entry.event_type === 'reservation.confirmed'),
        { timeout: 30_000 },
      )
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({ response_status: 503, signature_present: true }),
          expect.objectContaining({ response_status: 204, signature_present: true }),
        ]),
      );
    await expect
      .poll(() =>
        queryDatabase(
          "SELECT d.state || ':' || d.attempt_count FROM webhook_deliveries d JOIN partner_webhook_endpoints e ON e.id=d.webhook_endpoint_id WHERE e.url='https://webhook-receiver:9443/webhooks' ORDER BY d.created_at LIMIT 1",
        ),
      )
      .toMatch(/^DELIVERED:[2-9][0-9]*$/);
  });

  await test.step('Open the authenticated Dashboard and verify real sale activity', async () => {
    await page.goto(urls.admin);
    await expect(page.getByRole('heading', { name: 'Upcoming events', exact: true })).toBeVisible();
    await expect(page.getByText(reviewNames.event, { exact: true })).toBeVisible();
    await expect(page.getByText('Tickets sold', { exact: true })).toBeVisible();
    await screenshot(page, '04-admin-dashboard-live-activity');
  });

  let ticketBDetail = await apiJSON<TicketDetail>(`/api/v1/admin/tickets/${ticketB}`, { token });
  expect(ticketBDetail.data.display_label).toBeTruthy();

  await test.step('Find an active review ticket and reissue its credential through the real Admin UI', async () => {
    const reissueTicketId = ticketBDetail.data.status === 'ACTIVE' ? ticketB : ticketA;
    const reissueDetail =
      reissueTicketId === ticketB
        ? ticketBDetail
        : await apiJSON<TicketDetail>(`/api/v1/admin/tickets/${reissueTicketId}`, { token });
    expect(reissueDetail.data.status).toBe('ACTIVE');
    await page.getByRole('link', { name: 'Tickets', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Tickets' })).toBeVisible();
    await page.getByLabel('Filter tickets by event').selectOption(eventId);
    const row = page
      .getByRole('row')
      .filter({ hasText: reissueDetail.data.display_label ?? 'Admission ticket' })
      .first();
    await expect(row).toBeVisible();
    await row.click();
    const detail = page.getByRole('dialog', { name: 'Ticket detail' });
    await expect(detail).toContainText(reissueDetail.data.display_label!);
    await expect(detail).toContainText('Active');
    await detail.getByRole('button', { name: 'Reissue credential' }).click();
    const reissue = page.getByRole('dialog', { name: 'Reissue ticket credential' });
    await reissue.getByRole('button', { name: 'Reissue credential' }).click();
    await expect(reissue).toBeHidden();
    const persisted = await apiJSON<TicketDetail>(`/api/v1/admin/tickets/${reissueTicketId}`, {
      token,
    });
    expect(persisted.data.credential_state).toBe('ACTIVE');
    if (reissueTicketId === ticketB) {
      const currentCredential = await apiJSON<{ qr_payload: string }>(
        `/api/v1/admin/tickets/${ticketB}/credential`,
        { token },
      );
      await setSecret('ticketBQRBeforeVoid', currentCredential.data.qr_payload);
    }
    await screenshot(page, '04-admin-ticket-reissued');
  });

  await test.step('Rotate the signing secret before the next real webhook event', async () => {
    await page.getByRole('link', { name: 'Integrations', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible();
    const endpoint = page.locator('.panel').filter({ hasText: reviewNames.partner }).first();
    await endpoint.getByRole('button', { name: 'Rotate signing secret' }).click();
    const secretDialog = page.getByRole('dialog', { name: 'Signing secret rotated' });
    await expect(secretDialog).toBeVisible();
    await secretDialog.getByRole('button', { name: 'I have stored it' }).click();
    await expect(secretDialog).toBeHidden();
    await page.getByRole('link', { name: 'Tickets', exact: true }).click();
    await page.getByLabel('Filter tickets by event').selectOption(eventId);
  });

  const inventoryBeforeVoid = await apiJSON<InventoryReport>(
    `/api/v1/admin/events/${eventId}/reports/inventory`,
    { token },
  );

  await test.step('Void Ticket B, prove policy denial, then deliberately enable and re-release it', async () => {
    await configureEventPolicy(eventId, { allow_voided_inventory_rerelease: false });
    const row = page
      .getByRole('row')
      .filter({ hasText: ticketBDetail.data.display_label ?? 'Admission ticket' })
      .first();
    if (ticketBDetail.data.status === 'ACTIVE') {
      await row.click();
      await page
        .getByRole('dialog', { name: 'Ticket detail' })
        .getByRole('button', { name: 'Void ticket' })
        .click();
      const voidDialog = page.getByRole('dialog', { name: 'Void ticket' });
      await voidDialog.getByLabel('Reason').fill('Dedicated live review Ticket B completed');
      await voidDialog.getByRole('button', { name: 'Void ticket' }).click();
      await expect(voidDialog).toBeHidden();
      ticketBDetail = await apiJSON<TicketDetail>(`/api/v1/admin/tickets/${ticketB}`, { token });
    }
    await expect(row).toContainText('Voided');

    await row.click();
    const detail = page.getByRole('dialog', { name: 'Ticket detail' });
    await expect(detail).toContainText('Voided');
    await detail.getByRole('button', { name: 'Re-release inventory' }).click();
    const release = page.getByRole('dialog', { name: 'Re-release ticket inventory' });
    await release.getByLabel('Reason').fill('Return dedicated review capacity to sale');
    await release.getByRole('button', { name: 'Re-release inventory' }).click();
    await expect(release).toContainText('Event policy does not permit voided inventory re-release');
    await screenshot(page, '04-admin-policy-blocked-capacity-release');
    await release.getByRole('button', { name: 'Cancel' }).click();

    await configureEventPolicy(eventId, { allow_voided_inventory_rerelease: true });
    await detail.getByRole('button', { name: 'Re-release inventory' }).click();
    const permittedRelease = page.getByRole('dialog', { name: 'Re-release ticket inventory' });
    await permittedRelease.getByLabel('Reason').fill('Return dedicated review capacity to sale');
    await permittedRelease.getByRole('button', { name: 'Re-release inventory' }).click();
    await expect(permittedRelease).toBeHidden();

    await page.reload();
    await page.getByLabel('Filter tickets by event').selectOption(eventId);
    await expect(
      page
        .getByRole('row')
        .filter({ hasText: ticketBDetail.data.display_label ?? 'Admission ticket' })
        .first(),
    ).toContainText('Voided');
    const persisted = await apiJSON<TicketDetail>(`/api/v1/admin/tickets/${ticketB}`, { token });
    expect(persisted.data.status).toBe('VOIDED');
    const inventoryAfterRelease = await apiJSON<InventoryReport>(
      `/api/v1/admin/events/${eventId}/reports/inventory`,
      { token },
    );
    expect(inventoryAfterRelease.data.total.available).toBe(
      inventoryBeforeVoid.data.total.available + 1,
    );
    await screenshot(page, '04-admin-ticket-voided-and-capacity-released');
  });

  await test.step('Prove the rotated secret signs a later ticket webhook delivery', async () => {
    await expect
      .poll(async () =>
        (await receiverEntries()).filter((entry) => entry.event_type === 'ticket.voided'),
      )
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({ response_status: 204, signature_present: true }),
        ]),
      );
    await expect
      .poll(() =>
        queryDatabase(
          "SELECT d.state FROM webhook_deliveries d JOIN outbox_events o ON o.id=d.outbox_event_id JOIN partner_webhook_endpoints e ON e.id=d.webhook_endpoint_id WHERE e.url='https://webhook-receiver:9443/webhooks' AND o.fact_type='ticket.voided' ORDER BY d.created_at DESC LIMIT 1",
        ),
      )
      .toBe('DELIVERED');
  });

  await test.step('Reject the real credential after its ticket was voided', async () => {
    const scanner = await page.context().newPage();
    await scanner.setViewportSize({ width: 390, height: 844 });
    await scanner.addInitScript(() => {
      Object.defineProperty(navigator, 'userAgentData', {
        configurable: true,
        value: { mobile: true },
      });
    });
    await scanner.goto(urls.scanner);
    await expect(scanner.getByRole('heading', { name: 'Choose an event to scan' })).toBeVisible();
    await scanner.getByRole('button', { name: new RegExp(`Select ${reviewNames.event}`) }).click();
    await scanner.getByRole('button', { name: 'Enter code manually' }).first().click();
    const manual = scanner.getByRole('dialog', { name: 'Enter code manually' });
    await manual.getByLabel('Manual admission code').fill(await getSecret('ticketBQRBeforeVoid'));
    await manual.getByRole('button', { name: 'Check ticket' }).click();
    await expect(scanner.getByRole('heading', { name: 'Ticket not valid' })).toBeVisible();
    await expect(scanner.getByRole('heading', { name: 'Admit guest' })).toHaveCount(0);
    await screenshot(scanner, '04-scanner-voided-ticket-rejected');
    await scanner.close();
  });

  await test.step('Review live Scanner activity and reverse its active admission', async () => {
    await page.getByRole('link', { name: 'Admissions', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Admissions' })).toBeVisible();
    await page.getByLabel('Admission event').selectOption(eventId);
    await expect(page.getByText('Already admitted', { exact: true }).first()).toBeVisible();
    const reverseButton = page.getByRole('button', { name: 'Reverse' }).first();
    await expect(reverseButton).toBeVisible();
    await reverseButton.click();
    const reverse = page.getByRole('dialog', { name: 'Reverse admission' });
    await reverse.getByLabel('Reason').fill('Live gate workflow completed');
    await reverse.getByRole('button', { name: 'Reverse admission' }).click();
    await expect(reverse).toBeHidden();
    const ticketAState = await apiJSON<TicketDetail>(`/api/v1/admin/tickets/${ticketA}`, { token });
    expect(ticketAState.data.admission_state).toBe('REVERSED');
    await screenshot(page, '04-admin-admission-reversed');
  });

  await test.step('Verify inventory, sales, and admission reports from authoritative data', async () => {
    await page.getByRole('link', { name: 'Reports', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Reports' })).toBeVisible();
    await page.getByLabel('Report event').selectOption(eventId);
    await expect(page.getByText('Inventory position')).toBeVisible();
    await expect(page.getByText('Commercial performance')).toBeVisible();
    await expect(page.getByText('Admission outcomes')).toBeVisible();
    await expect(page.getByRole('row', { name: /Reserved seating/ })).toBeVisible();
    await expect(page.getByRole('row', { name: /General admission/ })).toBeVisible();
    await screenshot(page, '04-admin-authoritative-reports');
  });

  await test.step('Export and inspect the real accreditation CSV', async () => {
    await page.goto(`${urls.admin}/events/${eventId}`);
    await page.getByRole('tab', { name: 'Admissions' }).click();
    const downloadPromise = page.waitForEvent('download');
    const responsePromise = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/v1/admin/events/${eventId}/accreditation-export`) &&
        response.request().method() === 'GET',
    );
    await page.getByRole('button', { name: 'Export accreditation CSV' }).click();
    expect((await responsePromise).ok()).toBe(true);
    const download = await downloadPromise;
    const csv = await readFile(await download.path(), 'utf8');
    const rows = parseCSV(csv);
    expect(rows[0]).toEqual([
      'ticket',
      'attendee_name',
      'event',
      'section_or_area',
      'row',
      'table',
      'seat',
      'ticket_status',
      'admission_status',
      'admission_timestamp',
      'issued_at',
    ]);
    expect(rows.length).toBeGreaterThan(1);
    expect(rows.slice(1).some((row) => /^tkt_/.test(row[0] ?? ''))).toBe(true);
    expect(rows.slice(1).some((row) => row[2] === reviewNames.event)).toBe(true);
    expect(rows.slice(1).every((row) => Boolean(row[3] && row[7] && row[10]))).toBe(true);
    expect(rows.slice(1).some((row) => Boolean(row[4] || row[5] || row[6]))).toBe(true);
    expect(csv).not.toMatch(
      /qr1\.|reservation[_ -]?token|partner[_ -]?credential|signing[_ -]?secret/i,
    );
    expect(csv).not.toMatch(
      /[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/i,
    );
  });

  await test.step('Verify the live Partner webhook remains active', async () => {
    await page.getByRole('link', { name: 'Integrations', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible();
    const endpoint = page.locator('.panel').filter({ hasText: reviewNames.partner }).first();
    await expect(endpoint).toBeVisible();
    await expect(endpoint).toContainText('Active');
    await screenshot(page, '04-admin-live-integration-state');
  });

  await test.step('Inspect Account, sign out, and sign back in through real Supabase', async () => {
    await page.getByRole('button', { name: 'Account menu' }).click();
    await page.getByRole('link', { name: 'Account settings' }).click();
    await expect(page.getByRole('heading', { name: 'Account' })).toBeVisible();
    await expect(page.getByText('Authenticated operator')).toBeVisible();
    await screenshot(page, '04-admin-account');
    await page.locator('.session-panel').getByRole('button', { name: 'Sign out' }).click();
    await expect(page).toHaveURL(/\/sign-in$/);
    await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
    const credentials = await liveCredentials();
    await page.getByLabel('Email').fill(credentials.email);
    await page.getByLabel('Password').fill(credentials.password);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByRole('heading', { name: 'Upcoming events', exact: true })).toBeVisible();
  });

  await saveVideo(page, '04-admin-support-reporting.webm');
});
