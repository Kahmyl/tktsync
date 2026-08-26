/* global console, fetch, process */
import { randomUUID } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';

const api = (process.env.API_PUBLIC_URL || 'http://localhost:58480').replace(/\/$/, '');
const token = process.env.REVIEW_SEED_ADMIN_TOKEN || '';
const returnURL = process.env.PARTNER_DEMO_RETURN_URL || '';
const generation = (process.env.REVIEW_DEMO_GENERATION || 'primary').trim();
const secretFile = '.review-demo.env';

if (!token)
  throw new Error('REVIEW_SEED_ADMIN_TOKEN is required (a temporary platform-admin access token).');
if (!returnURL.startsWith('https://'))
  throw new Error('PARTNER_DEMO_RETURN_URL must be the deployed HTTPS /checkout/return URL.');

async function request(path, { method = 'GET', body, idempotency = false } = {}) {
  const response = await fetch(`${api}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${token}`,
      accept: 'application/json',
      ...(body ? { 'content-type': 'application/json' } : {}),
      ...(idempotency ? { 'idempotency-key': randomUUID() } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new Error(
      `${method} ${path}: ${data?.error?.code || response.status} ${data?.error?.message || ''}`,
    );
  return data;
}

function layout() {
  const sections = [
    { object_key: 'vip', name: 'VIP Reserved', kind: 'RESERVED', sort_order: 1 },
    { object_key: 'tables', name: 'Championship Tables', kind: 'TABLE', sort_order: 2 },
    { object_key: 'floor', name: 'Arena Floor', kind: 'GA', sort_order: 3 },
  ];
  const rows = [];
  const tables = [];
  const seats = [];
  for (let row = 0; row < 8; row += 1) {
    const label = String.fromCharCode(65 + row);
    rows.push({
      object_key: `vip-row-${label.toLowerCase()}`,
      section_key: 'vip',
      label,
      sort_order: row,
    });
    for (let seat = 1; seat <= 12; seat += 1)
      seats.push({
        object_key: `vip-${label.toLowerCase()}-${seat}`,
        section_key: 'vip',
        row_key: `vip-row-${label.toLowerCase()}`,
        seat_label: String(seat),
        sort_order: seat,
      });
  }
  for (let table = 1; table <= 10; table += 1) {
    tables.push({ object_key: `table-${table}`, section_key: 'tables', label: `Table ${table}` });
    for (let seat = 1; seat <= 6; seat += 1)
      seats.push({
        object_key: `table-${table}-seat-${seat}`,
        section_key: 'tables',
        table_key: `table-${table}`,
        seat_label: String(seat),
        sort_order: seat,
      });
  }
  return {
    geometry: {
      canvas: { width: 1000, height: 650 },
      objects: [
        {
          object_key: 'stage',
          type: 'RING',
          label: 'Championship Ring',
          x: 375,
          y: 220,
          width: 250,
          height: 180,
          rotation: 0,
        },
        {
          object_key: 'vip',
          type: 'RESERVED',
          label: 'VIP Reserved',
          x: 40,
          y: 130,
          width: 280,
          height: 390,
          rotation: 0,
        },
        {
          object_key: 'tables',
          type: 'TABLE',
          label: 'Championship Tables',
          x: 680,
          y: 130,
          width: 280,
          height: 390,
          rotation: 0,
        },
        {
          object_key: 'floor',
          type: 'GA',
          label: 'Arena Floor',
          x: 330,
          y: 440,
          width: 340,
          height: 160,
          rotation: 0,
        },
      ],
    },
    sections,
    rows,
    tables,
    seats,
    ga_zones: [
      {
        object_key: 'arena-floor-zone',
        section_key: 'floor',
        name: 'Arena Floor',
        default_capacity: 500,
      },
    ],
  };
}

async function storedCredential() {
  if (process.env.PARTNER_DEMO_CREDENTIAL) return process.env.PARTNER_DEMO_CREDENTIAL;
  try {
    const raw = await readFile(secretFile, 'utf8');
    return raw.match(/^PARTNER_DEMO_CREDENTIAL=(.+)$/m)?.[1]?.trim() || '';
  } catch {
    return '';
  }
}

const venueName = 'Meridian Arena';
const eventName =
  generation === 'primary' ? 'Championship Night' : `Championship Night · ${generation}`;
const partnerName = 'Demo Partner';

const venueList = await request('/api/v1/admin/venues');
let venue = venueList.venues.find((item) => item.name === venueName);
if (!venue)
  venue = await request('/api/v1/admin/venues', {
    method: 'POST',
    idempotency: true,
    body: { name: venueName, address_text: '12 Meridian Way, Lagos' },
  });

let layouts = await request(`/api/v1/admin/venues/${venue.id}/layout-versions`);
let published = layouts.layout_versions.find((item) => item.state === 'PUBLISHED');
if (!published) {
  let draft = layouts.layout_versions.find((item) => item.state === 'DRAFT');
  if (!draft)
    draft = await request(`/api/v1/admin/venues/${venue.id}/layout-versions`, {
      method: 'POST',
      idempotency: true,
      body: {},
    });
  await request(`/api/v1/admin/venue-layouts/${draft.id}`, {
    method: 'PATCH',
    idempotency: true,
    body: layout(),
  });
  published = await request(`/api/v1/admin/venue-layouts/${draft.id}/publish`, {
    method: 'POST',
    idempotency: true,
    body: {},
  });
}

const events = await request(
  `/api/v1/admin/events?query=${encodeURIComponent(eventName)}&limit=100`,
);
let event = events.items.find((item) => item.name === eventName);
if (!event) {
  const now = new Date();
  const starts = new Date(now.getTime() + 14 * 86400_000);
  const ends = new Date(starts.getTime() + 4 * 3600_000);
  event = await request('/api/v1/admin/events', {
    method: 'POST',
    idempotency: true,
    body: {
      venue_id: venue.id,
      name: eventName,
      starts_at: starts.toISOString(),
      ends_at: ends.toISOString(),
      sales_open_at: new Date(now.getTime() - 86400_000).toISOString(),
      sales_close_at: new Date(starts.getTime() - 30 * 60_000).toISOString(),
      admission_open_at: new Date(now.getTime() - 3600_000).toISOString(),
      admission_close_at: new Date(ends.getTime() + 2 * 3600_000).toISOString(),
      timezone_name: 'Africa/Lagos',
    },
  });
}

let configuration = await request(`/api/v1/admin/events/${event.id}/configuration`);
if (!configuration.layout?.finalized_at)
  await request(`/api/v1/admin/events/${event.id}/materialize-layout`, {
    method: 'POST',
    idempotency: true,
    body: { layout_id: published.id },
  });
configuration = await request(`/api/v1/admin/events/${event.id}/configuration`);
const tiers = [
  { code: 'VIP', name: 'VIP Reserved', amount_minor: 7500000, currency: 'NGN' },
  { code: 'TBL', name: 'Championship Table', amount_minor: 5000000, currency: 'NGN' },
  { code: 'GA', name: 'Arena Floor', amount_minor: 2500000, currency: 'NGN' },
];
for (const tier of tiers)
  if (!configuration.price_tiers.some((item) => item.code === tier.code))
    await request(`/api/v1/admin/events/${event.id}/price-tiers`, {
      method: 'POST',
      idempotency: true,
      body: tier,
    });
configuration = await request(`/api/v1/admin/events/${event.id}/configuration`);
const inventory = await request(`/api/v1/admin/events/${event.id}/inventory`);
for (const tier of tiers) {
  const target = configuration.price_tiers.find((item) => item.code === tier.code);
  const section =
    tier.code === 'VIP'
      ? 'VIP Reserved'
      : tier.code === 'TBL'
        ? 'Championship Tables'
        : 'Arena Floor';
  const sectionKeys = [
    ...new Set(
      inventory.inventory
        .filter((item) => item.kind === 'RESERVED' && item.section_name === section)
        .map((item) => item.section_object_key),
    ),
  ];
  const gaKeys = inventory.inventory
    .filter((item) => item.kind === 'GA' && item.section_name === section)
    .map((item) => item.snapshot_object_key);
  if (
    [...sectionKeys, ...gaKeys].length &&
    !inventory.inventory
      .filter((item) => item.section_name === section)
      .every((item) => item.price_tier_id === target.id)
  )
    await request(`/api/v1/admin/events/${event.id}/pricing/assignments`, {
      method: 'POST',
      idempotency: true,
      body: {
        price_tier_id: target.id,
        section_object_keys: sectionKeys,
        ga_pool_object_keys: gaKeys,
      },
    });
}

const partners = await request(
  `/api/v1/admin/partners?query=${encodeURIComponent(partnerName)}&limit=100`,
);
let demoPartner = partners.items.find((item) => item.name === partnerName);
if (!demoPartner)
  demoPartner = await request('/api/v1/admin/partners', {
    method: 'POST',
    idempotency: true,
    body: { name: partnerName },
  });
await request(`/api/v1/admin/events/${event.id}/partners/${demoPartner.id}/access`, {
  method: 'POST',
  idempotency: true,
  body: {},
}).catch((error) => {
  if (!String(error).includes('ALREADY')) throw error;
});
await request(`/api/v1/admin/partners/${demoPartner.id}/allowed-return-urls`, {
  method: 'PUT',
  idempotency: true,
  body: { urls: [returnURL] },
});

let credential = await storedCredential();
if (!credential) {
  const issued = await request(`/api/v1/admin/partners/${demoPartner.id}/credentials`, {
    method: 'POST',
    idempotency: true,
    body: {},
  });
  credential = issued.credential;
  await writeFile(
    secretFile,
    `# Generated by pnpm demo:seed; do not commit.\nPARTNER_DEMO_CREDENTIAL=${credential}\n`,
    { mode: 0o600 },
  );
}

const state = await request(`/api/v1/admin/events/${event.id}`);
if (
  state.state === 'DRAFT' &&
  (!state.admission_open_at || new Date(state.admission_open_at) > new Date())
) {
  await request(`/api/v1/admin/events/${event.id}`, {
    method: 'PATCH',
    idempotency: true,
    body: { admission_open_at: new Date(Date.now() - 3600_000).toISOString() },
  });
}
if (state.state === 'DRAFT')
  await request(`/api/v1/admin/events/${event.id}/open-sales`, {
    method: 'POST',
    idempotency: true,
    body: {},
  });
else if (state.state === 'PAUSED')
  await request(`/api/v1/admin/events/${event.id}/resume-sales`, {
    method: 'POST',
    idempotency: true,
    body: {},
  });

console.log(
  JSON.stringify(
    {
      venue: { id: venue.id, name: venueName },
      event: { id: event.id, name: eventName },
      partner: { id: demoPartner.id, name: partnerName },
      return_url: returnURL,
      credential_file: secretFile,
      reset:
        'Run make reset-review-demo to create a fresh inventory generation without deleting history.',
    },
    null,
    2,
  ),
);
