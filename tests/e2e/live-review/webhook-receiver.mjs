import { appendFileSync, readFileSync } from 'node:fs';
import { createServer } from 'node:https';

const key = readFileSync(process.env.TLS_KEY_PATH);
const cert = readFileSync(process.env.TLS_CERT_PATH);
const logPath = process.env.RECEIVER_LOG_PATH;
const failFirst = process.env.FAIL_FIRST_DELIVERY === 'true';
let deliveries = 0;

createServer({ key, cert }, (request, response) => {
  if (request.method === 'GET' && request.url === '/health') {
    response.writeHead(200);
    response.end('ok');
    return;
  }

  const chunks = [];
  request.on('data', (chunk) => chunks.push(chunk));
  request.on('end', () => {
    deliveries += 1;
    let eventType = 'unreadable';
    try {
      eventType = JSON.parse(Buffer.concat(chunks).toString('utf8')).type ?? 'missing';
    } catch {}
    const signature = request.headers['tktsync-signature'] ?? '';
    const responseStatus = failFirst && deliveries === 1 ? 503 : 204;
    appendFileSync(
      logPath,
      `${JSON.stringify({
        received_at: new Date().toISOString(),
        method: request.method,
        path: request.url,
        content_type: request.headers['content-type'] ?? '',
        event_type: eventType,
        signature_present: Boolean(signature),
        timestamp_present: /^t=\d+,/.test(String(signature)),
        event_id_present: Boolean(request.headers['tktsync-event-id']),
        delivery_id_present: Boolean(request.headers['tktsync-delivery-id']),
        response_status: responseStatus,
      })}\n`,
      { mode: 0o600 },
    );
    response.writeHead(responseStatus);
    response.end();
  });
}).listen(9443, '0.0.0.0');
