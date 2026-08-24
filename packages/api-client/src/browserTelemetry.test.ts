import { describe, expect, it } from 'vitest';

import { installBrowserTelemetry, sanitizeClientTelemetry } from './browserTelemetry.js';

describe('browser telemetry', () => {
  it('exports only the bounded API telemetry allowlist', () => {
    expect(
      sanitizeClientTelemetry({
        kind: 'api-error',
        operation: 'POST /api/v1/selection/reservations',
        duration_ms: 42,
        error_name: 'TypeError',
        authorization: 'Bearer selection-secret',
        reservation_token: 'reservation-secret',
        body: { credential: 'qr-secret' },
      }),
    ).toEqual({
      kind: 'api-error',
      operation: 'POST /api/v1/selection/reservations',
      duration_ms: 42,
      error_name: 'TypeError',
    });
  });

  it('rejects operations containing concrete URLs or uncontrolled text', () => {
    expect(
      sanitizeClientTelemetry({
        kind: 'api',
        operation: 'GET https://api.example/api/v1/selection/session?token=secret',
        duration_ms: 1,
      }),
    ).toBeUndefined();
  });

  it('is a no-op when no endpoint is configured', () => {
    expect(() => installBrowserTelemetry({ application: 'selector-web' })()).not.toThrow();
  });
});
