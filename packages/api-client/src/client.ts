import createClient from 'openapi-fetch';

import type { paths } from './generated/schema.js';

export function createTktSyncClient(baseUrl: string) {
  const client = createClient<paths>({ baseUrl });
  const started = new Map<string, number>();
  client.use({
    onRequest({ request, id }) {
      started.set(id, performance.now());
      if (!request.headers.has('X-Request-ID')) {
        request.headers.set('X-Request-ID', crypto.randomUUID());
      }
      return request;
    },
    onResponse({ response, schemaPath, request, id }) {
      emitTelemetry({
        kind: 'api',
        operation: `${request.method} ${schemaPath}`,
        status: response.status,
        duration_ms: elapsed(started, id),
      });
    },
    onError({ error, schemaPath, request, id }) {
      emitTelemetry({
        kind: 'api-error',
        operation: `${request.method} ${schemaPath}`,
        duration_ms: elapsed(started, id),
        error_name: error instanceof Error ? error.name : 'NetworkError',
      });
    },
  });
  return client;
}

function elapsed(started: Map<string, number>, id: string) {
  const value = started.get(id);
  started.delete(id);
  return value === undefined ? 0 : Math.max(0, performance.now() - value);
}

function emitTelemetry(detail: Record<string, string | number>) {
  if (typeof globalThis.dispatchEvent === 'function' && typeof CustomEvent !== 'undefined') {
    globalThis.dispatchEvent(new CustomEvent('tktsync:client-telemetry', { detail }));
  }
}

export type TktSyncClient = ReturnType<typeof createTktSyncClient>;
