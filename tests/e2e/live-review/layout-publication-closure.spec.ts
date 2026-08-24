import { randomUUID } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import { reviewRoot, runId, urls } from './config';
import { apiJSON, operatorToken } from './state';

test.use({ trace: 'off' });

type PublishObservation = {
  iteration: number;
  layout_id: string;
  idempotency_key: string;
  first: { status: number; request_id: string | null; code: string; message: string };
  same_key: { status: number; request_id: string | null; code: string; message: string };
  fresh_key: { status: number; request_id: string | null; code: string; message: string };
  authoritative_state: string;
};

async function publish(token: string, layoutId: string, idempotencyKey: string) {
  const response = await fetch(`${urls.api}/api/v1/admin/venue-layouts/${layoutId}/publish`, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
    body: '{}',
  });
  const body = (await response.json()) as {
    error?: { code?: string; message?: string };
  };
  return {
    status: response.status,
    request_id: response.headers.get('x-request-id'),
    code: body.error?.code ?? 'OK',
    message: body.error?.message ?? '',
  };
}

test('20 first-attempt layout publications and idempotency replays', async () => {
  test.setTimeout(180_000);
  const token = await operatorToken();
  const observations: PublishObservation[] = [];

  for (let iteration = 1; iteration <= 20; iteration += 1) {
    const venue = await apiJSON<{ id: string }>('/api/v1/admin/venues', {
      method: 'POST',
      token,
      idempotencyKey: randomUUID(),
      body: {
        name: `E2E ${runId} Layout Reliability ${String(iteration).padStart(2, '0')}`,
        address_text: 'Isolated live-review record',
      },
    });
    const layout = await apiJSON<{ id: string }>(
      `/api/v1/admin/venues/${venue.data.id}/layout-versions`,
      { method: 'POST', token, idempotencyKey: randomUUID(), body: {} },
    );
    await apiJSON(`/api/v1/admin/venue-layouts/${layout.data.id}`, {
      method: 'PATCH',
      token,
      idempotencyKey: randomUUID(),
      body: {
        geometry: { width: 900, height: 600 },
        sections: [
          { object_key: 'reserved', name: 'Reserved', kind: 'RESERVED', sort_order: 1 },
          { object_key: 'floor', name: 'Main Floor', kind: 'GA', sort_order: 2 },
        ],
        rows: [{ object_key: 'row-a', section_key: 'reserved', label: 'A', sort_order: 1 }],
        tables: [],
        seats: [
          {
            object_key: 'seat-a-1',
            section_key: 'reserved',
            row_key: 'row-a',
            seat_label: '1',
            sort_order: 1,
          },
        ],
        ga_zones: [
          {
            object_key: 'floor-zone',
            section_key: 'floor',
            name: 'Main Floor',
            default_capacity: 5,
          },
        ],
      },
    });

    const idempotencyKey = randomUUID();
    const first = await publish(token, layout.data.id, idempotencyKey);
    const sameKey = await publish(token, layout.data.id, idempotencyKey);
    const freshKey = await publish(token, layout.data.id, randomUUID());
    const layouts = await apiJSON<{
      layout_versions: Array<{ id: string; state: string }>;
    }>(`/api/v1/admin/venues/${venue.data.id}/layout-versions`, { token });
    const authoritativeState =
      layouts.data.layout_versions.find((item) => item.id === layout.data.id)?.state ?? 'MISSING';

    observations.push({
      iteration,
      layout_id: layout.data.id,
      idempotency_key: idempotencyKey,
      first,
      same_key: sameKey,
      fresh_key: freshKey,
      authoritative_state: authoritativeState,
    });

    expect(first.status, `first publication ${iteration}: ${JSON.stringify(first)}`).toBe(200);
    expect(sameKey, `same-key replay ${iteration}`).toMatchObject({ status: 200, code: 'OK' });
    expect(freshKey, `fresh-key conflict ${iteration}`).toMatchObject({
      status: 400,
      code: 'VALIDATION_ERROR',
      message: 'only DRAFT layout versions may be published',
    });
    expect(authoritativeState).toBe('PUBLISHED');
  }

  await writeFile(
    path.join(reviewRoot, 'logs', 'layout-publication-reproduction.json'),
    JSON.stringify({ run_id: runId, attempts: observations.length, observations }, null, 2),
  );
});
