export type BrowserTelemetryOptions = {
  endpoint?: string;
  application: 'admin-web' | 'scanner-web' | 'selector-web';
};

type ClientTelemetry = {
  kind: 'api' | 'api-error';
  operation: string;
  status?: number;
  duration_ms: number;
  error_name?: string;
};

export function installBrowserTelemetry(options: BrowserTelemetryOptions) {
  const endpoint = safeEndpoint(options.endpoint);
  if (!endpoint || typeof globalThis.addEventListener !== 'function') {
    return () => undefined;
  }

  const listener = (event: Event) => {
    const detail = event instanceof CustomEvent ? sanitizeClientTelemetry(event.detail) : undefined;
    if (!detail) return;
    const body = JSON.stringify({
      schema_version: 1,
      application: options.application,
      sent_at: new Date().toISOString(),
      ...detail,
    });
    try {
      if (
        typeof navigator !== 'undefined' &&
        typeof navigator.sendBeacon === 'function' &&
        navigator.sendBeacon(endpoint, new Blob([body], { type: 'application/json' }))
      ) {
        return;
      }
      void fetch(endpoint, {
        method: 'POST',
        body,
        headers: { 'content-type': 'application/json' },
        credentials: 'omit',
        keepalive: true,
        referrerPolicy: 'no-referrer',
      }).catch(() => undefined);
    } catch {
      // Telemetry is advisory and must never affect a product workflow.
    }
  };

  globalThis.addEventListener('tktsync:client-telemetry', listener);
  return () => globalThis.removeEventListener('tktsync:client-telemetry', listener);
}

export function sanitizeClientTelemetry(value: unknown): ClientTelemetry | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const candidate = value as Record<string, unknown>;
  if (candidate.kind !== 'api' && candidate.kind !== 'api-error') return undefined;
  if (
    typeof candidate.operation !== 'string' ||
    candidate.operation.length > 160 ||
    !/^(GET|POST|PUT|PATCH|DELETE) \/api\/v1\/[A-Za-z0-9_/{}/-]+$/.test(candidate.operation)
  ) {
    return undefined;
  }
  if (typeof candidate.duration_ms !== 'number' || !Number.isFinite(candidate.duration_ms)) {
    return undefined;
  }
  const sanitized: ClientTelemetry = {
    kind: candidate.kind,
    operation: candidate.operation,
    duration_ms: Math.min(300_000, Math.max(0, candidate.duration_ms)),
  };
  if (typeof candidate.status === 'number' && Number.isInteger(candidate.status)) {
    sanitized.status = Math.min(599, Math.max(100, candidate.status));
  }
  if (
    typeof candidate.error_name === 'string' &&
    /^[A-Za-z][A-Za-z0-9_.-]{0,63}$/.test(candidate.error_name)
  ) {
    sanitized.error_name = candidate.error_name;
  }
  return sanitized;
}

function safeEndpoint(raw: string | undefined) {
  const value = raw?.trim();
  if (!value) return undefined;
  try {
    const url = new URL(value, globalThis.location?.origin ?? 'https://local.invalid');
    if (url.protocol !== 'https:' && !(url.protocol === 'http:' && isLocalHost(url.hostname))) {
      return undefined;
    }
    if (url.username || url.password || url.search || url.hash) return undefined;
    return value;
  } catch {
    return undefined;
  }
}

function isLocalHost(host: string) {
  return host === 'localhost' || host === '127.0.0.1' || host === '::1';
}
