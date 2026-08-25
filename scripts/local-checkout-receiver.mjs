import { readFileSync } from 'node:fs';
import { createServer } from 'node:https';

const host = process.env.CHECKOUT_RECEIVER_HOST ?? '127.0.0.1';
const port = Number(process.env.CHECKOUT_RECEIVER_PORT ?? '45991');
const keyPath = process.env.CHECKOUT_RECEIVER_TLS_KEY;
const certPath = process.env.CHECKOUT_RECEIVER_TLS_CERT;

if (!keyPath || !certPath) {
  throw new Error(
    'Set CHECKOUT_RECEIVER_TLS_KEY and CHECKOUT_RECEIVER_TLS_CERT to local TLS file paths.',
  );
}

const escapeHTML = (value) =>
  value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');

const page = (reservationId, reservationToken) => `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>TktSync checkout handoff</title>
    <style>
      :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, sans-serif; color: #172b2d; background: #f3f8f8; }
      * { box-sizing: border-box; }
      body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 32px; }
      main { width: min(680px, 100%); background: white; border: 1px solid #d8e3e3; border-radius: 24px; box-shadow: 0 18px 55px rgba(18, 62, 65, .12); padding: 40px; }
      .mark { width: 58px; height: 58px; display: grid; place-items: center; border-radius: 18px; background: #e2f3f3; color: #087f83; font-size: 28px; }
      h1 { margin: 22px 0 8px; font-size: clamp(28px, 5vw, 40px); line-height: 1.1; }
      .lead { margin: 0 0 30px; color: #617174; font-size: 17px; line-height: 1.55; }
      label { display: block; margin-top: 20px; color: #526467; font-size: 13px; font-weight: 750; letter-spacing: .06em; text-transform: uppercase; }
      .field { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; margin-top: 8px; }
      input { width: 100%; border: 1px solid #bdcdce; border-radius: 12px; padding: 13px 14px; color: #172b2d; background: #f9fbfb; font: 14px ui-monospace, SFMono-Regular, Menlo, monospace; }
      button { border: 1px solid #087f83; border-radius: 12px; padding: 0 18px; background: white; color: #087f83; font-weight: 750; cursor: pointer; }
      button:hover { background: #eaf7f7; }
      .warning { margin: 28px 0 0; padding: 15px 17px; border-radius: 14px; background: #fff7e8; color: #714f0d; line-height: 1.45; }
      code { overflow-wrap: anywhere; }
    </style>
  </head>
  <body>
    <main>
      <div class="mark" aria-hidden="true">✓</div>
      <h1>Checkout handoff received</h1>
      <p class="lead">TktSync securely posted the reservation to this local Partner checkout receiver. Copy these values into the next Partner API request.</p>

      <label for="reservation-id">Reservation ID</label>
      <div class="field">
        <input id="reservation-id" readonly value="${escapeHTML(reservationId)}">
        <button type="button" data-copy="reservation-id">Copy</button>
      </div>

      <label for="reservation-token">Reservation token</label>
      <div class="field">
        <input id="reservation-token" type="password" readonly value="${escapeHTML(reservationToken)}">
        <button type="button" data-copy="reservation-token">Copy</button>
      </div>

      <p class="warning"><strong>Keep the token private.</strong> It is shown only in this local page, is not placed in the URL, and is not written to the receiver log or disk.</p>
    </main>
    <script>
      document.querySelectorAll('[data-copy]').forEach((button) => {
        button.addEventListener('click', async () => {
          const input = document.getElementById(button.dataset.copy);
          await navigator.clipboard.writeText(input.value);
          button.textContent = 'Copied';
          setTimeout(() => { button.textContent = 'Copy'; }, 1500);
        });
      });
    </script>
  </body>
</html>`;

const server = createServer(
  { key: readFileSync(keyPath), cert: readFileSync(certPath) },
  (request, response) => {
    if (request.method !== 'POST' || request.url !== '/checkout') {
      response.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' });
      response.end('Not found');
      return;
    }

    const chunks = [];
    request.on('data', (chunk) => chunks.push(chunk));
    request.on('end', () => {
      const form = new URLSearchParams(Buffer.concat(chunks).toString('utf8'));
      const reservationId = form.get('reservation_id') ?? '';
      const reservationToken = form.get('reservation_token') ?? '';

      if (!reservationId || !reservationToken) {
        response.writeHead(400, { 'content-type': 'text/plain; charset=utf-8' });
        response.end('Missing reservation handoff fields');
        return;
      }

      response.writeHead(200, {
        'cache-control': 'no-store',
        'content-security-policy': "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'",
        'content-type': 'text/html; charset=utf-8',
        'referrer-policy': 'no-referrer',
        'x-content-type-options': 'nosniff',
        'x-frame-options': 'DENY',
      });
      response.end(page(reservationId, reservationToken));
    });
  },
);

server.listen(port, host, () => {
  console.log(`Local checkout receiver listening at https://${host}:${port}/checkout`);
});
