/* global Buffer, URL, console, process */
import { createServer } from 'node:http';

const port = Number(process.env.PORT || 48090);
const event = {
  id: 'evt_demo',
  name: 'Championship Night',
  state: 'ON_SALE',
  starts_at: '2027-09-12T18:00:00Z',
  venue_name: 'Meridian Arena',
  address_text: '12 Meridian Way, Lagos',
  server_time: new Date().toISOString(),
};
const reservation = {
  id: 'res_demo',
  event_id: event.id,
  status: 'HELD',
  currency: 'NGN',
  hold_expires_at: '2099-01-01T00:10:00Z',
  max_lifetime_at: '2099-01-01T00:20:00Z',
  server_time: '2099-01-01T00:00:00Z',
  items: [
    {
      id: 'ritem_demo',
      inventory_kind: 'RESERVED',
      inventory_id: 'inv_demo',
      quantity: 1,
      unit_amount_minor: 7500000,
      currency: 'NGN',
      price_tier_label: 'VIP Reserved',
      commercial_terms: {},
    },
  ],
  total: { amount_minor: 7500000, currency: 'NGN' },
};
function json(response, body, status = 200) {
  response.writeHead(status, { 'content-type': 'application/json' });
  response.end(JSON.stringify(body));
}
createServer((request, response) => {
  const url = new URL(request.url || '/', `http://127.0.0.1:${port}`);
  if (url.pathname === '/handoff') {
    response.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
    return response.end(
      `<form method="post" action="http://127.0.0.1:4181/checkout/return"><input type="hidden" name="reservation_id" value="res_demo"><input type="hidden" name="reservation_token" value="visual-reservation-token"><button>Open checkout fixture</button></form>`,
    );
  }
  if (url.pathname === '/api/v1/partner/events')
    return json(response, { items: [event], total: 1, server_time: new Date().toISOString() });
  if (url.pathname === `/api/v1/partner/events/${event.id}`) return json(response, event);
  if (url.pathname.endsWith('/availability'))
    return json(response, {
      reserved_units: [
        {
          inventory_id: 'inv_demo',
          offer: { price: { amount_minor: 7500000, currency: 'NGN' }, available_quantity: 1 },
        },
      ],
      ga_pools: [],
      server_time: new Date().toISOString(),
    });
  if (url.pathname.endsWith('/layout'))
    return json(response, {
      reserved_units: [
        {
          inventory_id: 'inv_demo',
          section_name: 'VIP Reserved',
          row: 'A',
          seat: '12',
          display_label: 'A12',
        },
      ],
      ga_pools: [],
    });
  if (url.pathname === '/api/v1/partner/reservations/res_demo') return json(response, reservation);
  if (url.pathname.endsWith('/checkout'))
    return json(response, {
      reservation_id: 'res_demo',
      status: 'COMMITTING',
      checkout_attempt: {
        id: 'chk_demo',
        status: 'ACTIVE',
        checkout_expires_at: '2099-01-01T00:15:00Z',
      },
      server_time: new Date().toISOString(),
    });
  if (url.pathname.endsWith('/confirm'))
    return json(response, {
      reservation_id: 'res_demo',
      status: 'CONFIRMED',
      sale: {
        id: 'sale_demo',
        confirmed_at: new Date().toISOString(),
        partner_order_ref: 'NS-DEMO',
        partner_payment_ref: 'DEMO-PAID',
      },
      tickets: [{ id: 'tkt_demo', status: 'ACTIVE', credential_id: 'cred_demo' }],
    });
  if (url.pathname.endsWith('/credential'))
    return json(response, {
      ticket_id: 'tkt_demo',
      credential_id: 'cred_demo',
      status: 'ACTIVE',
      qr_payload: 'qr1.demo',
      qr_url: `http://127.0.0.1:${port}/qr.svg`,
    });
  if (url.pathname === '/qr.svg') {
    response.writeHead(200, { 'content-type': 'image/svg+xml' });
    return response.end(
      `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="white"/><path d="M5 5h30v30H5zM65 5h30v30H65zM5 65h30v30H5zM45 45h10v10H45zM65 45h10v20H65zM45 75h20v20H45zM80 70h15v25H80z" fill="#172b27"/></svg>`,
    );
  }
  json(response, { error: { code: 'NOT_FOUND', message: url.pathname } }, 404);
}).listen(port, '127.0.0.1', () => console.log(`Reviewer demo fixture API on ${port}`));
