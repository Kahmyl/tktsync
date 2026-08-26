/* global Buffer, URL, URLSearchParams, console, fetch, process, setInterval */
import { createReadStream, existsSync, readFileSync } from 'node:fs';
import { stat } from 'node:fs/promises';
import { createServer as createHTTPServer } from 'node:http';
import { createServer as createHTTPSServer } from 'node:https';
import { extname, join, normalize } from 'node:path';
import { randomUUID } from 'node:crypto';

const port = Number(process.env.PORT || 8080);
const host = process.env.HOST || '0.0.0.0';
const apiBase = (process.env.API_PUBLIC_URL || 'http://localhost:58480').replace(/\/$/, '');
const publicURL = (process.env.PARTNER_DEMO_PUBLIC_URL || `http://localhost:${port}`).replace(
  /\/$/,
  '',
);
const returnURL = process.env.PARTNER_DEMO_RETURN_URL || `${publicURL}/checkout/return`;
const partnerCredential = process.env.PARTNER_DEMO_CREDENTIAL || '';
const scannerURL = process.env.SCANNER_PUBLIC_URL || 'http://localhost:54472';
const reviewerURL = process.env.REVIEWER_PUBLIC_URL || 'http://localhost:54475';
const tlsKey = process.env.PARTNER_DEMO_TLS_KEY || '';
const tlsCert = process.env.PARTNER_DEMO_TLS_CERT || '';
const dist = join(import.meta.dirname, 'dist');
const sessions = new Map();
const sessionTTL = 30 * 60 * 1000;

function headers(extra = {}) {
  return {
    'cache-control': 'no-store',
    'content-security-policy':
      "default-src 'self'; img-src 'self' data: http: https:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self' http: https:",
    'referrer-policy': 'no-referrer',
    'x-content-type-options': 'nosniff',
    'x-frame-options': 'DENY',
    ...extra,
  };
}

function json(response, status, body, extra = {}) {
  response.writeHead(
    status,
    headers({ 'content-type': 'application/json; charset=utf-8', ...extra }),
  );
  response.end(JSON.stringify(body));
}

function redirect(response, location, cookie) {
  response.writeHead(303, headers({ location, ...(cookie ? { 'set-cookie': cookie } : {}) }));
  response.end();
}

function cookieFor(id) {
  const secure = publicURL.startsWith('https:') ? '; Secure' : '';
  return `northstar_checkout=${id}; Path=/; HttpOnly; SameSite=Lax; Max-Age=1800${secure}`;
}

function sessionID(request) {
  const cookie = request.headers.cookie || '';
  return (
    cookie
      .split(';')
      .map((part) => part.trim())
      .find((part) => part.startsWith('northstar_checkout='))
      ?.split('=')[1] || ''
  );
}

function currentSession(request) {
  const id = sessionID(request);
  const value = sessions.get(id);
  if (!value || Date.now() - value.createdAt > sessionTTL) {
    if (id) sessions.delete(id);
    return null;
  }
  return value;
}

async function body(request, limit = 16_384) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > limit) throw new Error('REQUEST_TOO_LARGE');
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString('utf8');
}

async function partner(path, options = {}) {
  if (!partnerCredential) throw new Error('DEMO_NOT_CONFIGURED');
  const response = await fetch(`${apiBase}${path}`, {
    ...options,
    headers: {
      authorization: `Bearer ${partnerCredential}`,
      accept: 'application/json',
      ...(options.body ? { 'content-type': 'application/json' } : {}),
      ...(options.idempotency ? { 'idempotency-key': options.idempotency } : {}),
      ...(options.reservationToken
        ? { 'x-tktsync-reservation-token': options.reservationToken }
        : {}),
    },
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(data?.error?.code || 'UPSTREAM_UNAVAILABLE');
    error.status = response.status;
    throw error;
  }
  return data;
}

function friendlyError(error) {
  const code = error instanceof Error ? error.message : '';
  if (['HOLD_EXPIRED', 'RESERVATION_EXPIRED'].includes(code))
    return {
      status: 409,
      code: 'expired',
      message: 'This ticket hold has expired. Please choose tickets again.',
    };
  if (['INVENTORY_UNAVAILABLE', 'INSUFFICIENT_GA_QUANTITY'].includes(code))
    return {
      status: 409,
      code: 'unavailable',
      message: 'One of these tickets is no longer available. Please choose again.',
    };
  if (code === 'EVENT_NOT_ON_SALE')
    return {
      status: 409,
      code: 'sales-closed',
      message: 'Ticket sales are not open for this Event.',
    };
  if (code === 'DEMO_NOT_CONFIGURED')
    return {
      status: 503,
      code: 'configuration',
      message: 'The demonstration connection is not configured yet.',
    };
  if (code === 'ALREADY_CONFIRMED')
    return { status: 409, code: 'completed', message: 'This checkout has already been completed.' };
  return {
    status: 503,
    code: 'temporary',
    message: 'The ticket service is temporarily unavailable. Please try again.',
  };
}

function sendError(response, error) {
  const safe = friendlyError(error);
  json(response, safe.status, { error: safe });
}

async function eventWithPrice(event) {
  try {
    const availability = await partner(
      `/api/v1/partner/events/${encodeURIComponent(event.id)}/availability`,
    );
    const prices = [
      ...(availability.reserved_units || []).flatMap((item) =>
        item.offer ? [item.offer.price] : [],
      ),
      ...(availability.ga_pools || []).flatMap((pool) =>
        (pool.offers || []).map((offer) => offer.price),
      ),
    ].filter(Boolean);
    prices.sort((a, b) => a.amount_minor - b.amount_minor);
    return { ...event, starting_price: prices[0] || null };
  } catch {
    return { ...event, starting_price: null };
  }
}

async function orderView(session) {
  const [reservation, event, layout] = await Promise.all([
    partner(`/api/v1/partner/reservations/${encodeURIComponent(session.reservationId)}`),
    partner(`/api/v1/partner/events/${encodeURIComponent(session.eventId)}`),
    partner(`/api/v1/partner/events/${encodeURIComponent(session.eventId)}/layout`).catch(() => ({
      reserved_units: [],
      ga_pools: [],
    })),
  ]);
  const inventory = new Map();
  for (const item of layout.reserved_units || [])
    inventory.set(item.inventory_id, {
      section: item.section_name,
      row: item.row,
      seat: item.seat,
      table: item.table,
      label: item.display_label,
    });
  for (const item of layout.ga_pools || [])
    inventory.set(item.inventory_id, { section: item.section_name, label: item.name });
  return {
    reservation: {
      ...reservation,
      items: reservation.items.map((item) => ({
        ...item,
        display: inventory.get(item.inventory_id) || {},
      })),
    },
    event,
  };
}

async function api(request, response, url) {
  try {
    if (request.method === 'GET' && url.pathname === '/demo-api/config')
      return json(response, 200, { scanner_url: scannerURL, reviewer_url: reviewerURL });
    if (request.method === 'GET' && url.pathname === '/demo-api/events') {
      const result = await partner('/api/v1/partner/events');
      return json(response, 200, {
        ...result,
        items: await Promise.all(result.items.map(eventWithPrice)),
      });
    }
    const eventMatch = url.pathname.match(/^\/demo-api\/events\/([^/]+)$/);
    if (request.method === 'GET' && eventMatch)
      return json(
        response,
        200,
        await eventWithPrice(
          await partner(`/api/v1/partner/events/${encodeURIComponent(eventMatch[1])}`),
        ),
      );
    const selectMatch = url.pathname.match(/^\/demo-api\/events\/([^/]+)\/selection$/);
    if (request.method === 'POST' && selectMatch) {
      const created = await partner('/api/v1/partner/selection-sessions', {
        method: 'POST',
        idempotency: randomUUID(),
        body: JSON.stringify({
          event_id: selectMatch[1],
          buyer_session_ref: `northstar-${randomUUID()}`,
          return_url: returnURL,
        }),
      });
      return json(response, 201, created);
    }
    const session = currentSession(request);
    if (request.method === 'GET' && url.pathname === '/demo-api/checkout') {
      if (!session)
        return json(response, 410, {
          error: {
            code: 'missing',
            message: 'This checkout session is no longer available. Please choose tickets again.',
          },
        });
      return json(response, 200, await orderView(session));
    }
    if (request.method === 'POST' && url.pathname === '/demo-api/checkout/begin') {
      if (!session)
        return json(response, 410, {
          error: { code: 'missing', message: 'This checkout session has expired.' },
        });
      const result = await partner(
        `/api/v1/partner/reservations/${encodeURIComponent(session.reservationId)}/checkout`,
        {
          method: 'POST',
          idempotency: session.beginKey,
          reservationToken: session.reservationToken,
          body: '{}',
        },
      );
      session.checkoutAttemptId = result.checkout_attempt.id;
      return json(response, 200, result);
    }
    if (request.method === 'POST' && url.pathname === '/demo-api/checkout/fail') {
      if (!session?.checkoutAttemptId)
        return json(response, 409, {
          error: { code: 'not-started', message: 'Start the payment step first.' },
        });
      const result = await partner(
        `/api/v1/partner/reservations/${encodeURIComponent(session.reservationId)}/payment-failure`,
        {
          method: 'POST',
          idempotency: session.failureKey,
          reservationToken: session.reservationToken,
          body: JSON.stringify({
            checkout_attempt_id: session.checkoutAttemptId,
            partner_payment_ref: `demo-failed-${session.id}`,
            failure_code: 'DEMO_DECLINED',
            requested_disposition: 'RETRY',
          }),
        },
      );
      return json(response, 200, result);
    }
    if (request.method === 'POST' && url.pathname === '/demo-api/checkout/confirm') {
      if (!session?.checkoutAttemptId)
        return json(response, 409, {
          error: { code: 'not-started', message: 'Start the payment step first.' },
        });
      if (!session.confirmation)
        session.confirmation = await partner(
          `/api/v1/partner/reservations/${encodeURIComponent(session.reservationId)}/confirm`,
          {
            method: 'POST',
            idempotency: session.confirmKey,
            reservationToken: session.reservationToken,
            body: JSON.stringify({
              checkout_attempt_id: session.checkoutAttemptId,
              partner_order_ref: `NS-${session.id.slice(0, 8).toUpperCase()}`,
              partner_payment_ref: `DEMO-${session.id.slice(-8).toUpperCase()}`,
            }),
          },
        );
      session.credentials = await Promise.all(
        session.confirmation.tickets.map((ticket) =>
          partner(`/api/v1/partner/tickets/${encodeURIComponent(ticket.id)}/credential`),
        ),
      );
      return json(response, 200, session.confirmation);
    }
    if (request.method === 'GET' && url.pathname === '/demo-api/ticket') {
      if (!session?.confirmation)
        return json(response, 404, {
          error: { code: 'not-found', message: 'Complete checkout before opening the ticket.' },
        });
      const order = await orderView(session);
      return json(response, 200, {
        ...order,
        confirmation: session.confirmation,
        credentials: session.credentials,
        scanner_url: scannerURL,
      });
    }
    return json(response, 404, { error: { code: 'not-found', message: 'Page not found.' } });
  } catch (error) {
    return sendError(response, error);
  }
}

async function checkoutReturn(request, response) {
  try {
    const form = new URLSearchParams(await body(request));
    const reservationId = form.get('reservation_id') || '';
    const reservationToken = form.get('reservation_token') || '';
    if (!/^res_/.test(reservationId) || !reservationToken)
      return json(response, 400, {
        error: {
          code: 'invalid-handoff',
          message: 'The ticket handoff was incomplete. Please choose tickets again.',
        },
      });
    const reservation = await partner(
      `/api/v1/partner/reservations/${encodeURIComponent(reservationId)}`,
    );
    const id = randomUUID();
    sessions.set(id, {
      id,
      createdAt: Date.now(),
      eventId: reservation.event_id,
      reservationId,
      reservationToken,
      beginKey: randomUUID(),
      failureKey: randomUUID(),
      confirmKey: randomUUID(),
    });
    redirect(response, `${publicURL}/checkout`, cookieFor(id));
  } catch (error) {
    sendError(response, error);
  }
}

async function staticFile(response, pathname) {
  const clean = normalize(decodeURIComponent(pathname)).replace(/^(\.\.[/\\])+/, '');
  let file = join(dist, clean === '/' ? 'index.html' : clean);
  if (!file.startsWith(dist) || !existsSync(file) || (await stat(file)).isDirectory())
    file = join(dist, 'index.html');
  const types = {
    '.html': 'text/html; charset=utf-8',
    '.js': 'text/javascript; charset=utf-8',
    '.css': 'text/css; charset=utf-8',
    '.svg': 'image/svg+xml',
  };
  response.writeHead(
    200,
    headers({
      'content-type': types[extname(file)] || 'application/octet-stream',
      'cache-control':
        extname(file) === '.html' ? 'no-store' : 'public, max-age=31536000, immutable',
    }),
  );
  createReadStream(file).pipe(response);
}

setInterval(() => {
  const cutoff = Date.now() - sessionTTL;
  for (const [id, value] of sessions) if (value.createdAt < cutoff) sessions.delete(id);
}, 60_000).unref();

const listener = async (request, response) => {
  const url = new URL(request.url || '/', publicURL);
  if (request.method === 'POST' && url.pathname === '/checkout/return')
    return checkoutReturn(request, response);
  if (url.pathname.startsWith('/demo-api/')) return api(request, response, url);
  if (!['GET', 'HEAD'].includes(request.method || ''))
    return json(response, 405, { error: { code: 'method', message: 'Method not allowed.' } });
  if (!existsSync(dist))
    return json(response, 503, {
      error: { code: 'not-built', message: 'Build the Partner Demo frontend before serving it.' },
    });
  return staticFile(response, url.pathname);
};

const server =
  tlsKey && tlsCert
    ? createHTTPSServer({ key: readFileSync(tlsKey), cert: readFileSync(tlsCert) }, listener)
    : createHTTPServer(listener);

server.listen(port, host, () =>
  console.log(`Northstar Demo listening on ${host}:${port}${tlsKey && tlsCert ? ' with TLS' : ''}`),
);
