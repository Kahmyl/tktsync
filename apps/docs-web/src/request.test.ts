import { describe, expect, it, vi } from 'vitest';
import { buildRequest, codeSample, initialRequest, materialEdit } from './request';

const operation = {
  method: 'POST',
  path: '/api/v1/partner/reservations/{reservation_id}',
  parameters: [
    { name: 'reservation_id', in: 'path', required: true, example: 'res_example' },
    { name: 'Idempotency-Key', in: 'header', required: true, example: 'ignored' },
  ],
  body: { example: { quantity: 1 } },
} as const;

describe('request model', () => {
  it('uses the same normalized model for execution and samples', () => {
    const state = initialRequest(operation);
    const request = buildRequest(operation, state, 'secret');
    expect(request.url).toBe('/__docs-exec/api/v1/partner/reservations/res_example');
    expect(request.headers.Authorization).toBe('Bearer secret');
    expect(codeSample('curl', operation, state, 'http://localhost:58480')).toContain(
      state.idempotencyKey,
    );
  });
  it('uses the declared successful response media type for SVG and binary responses', () => {
    const svgOperation = {
      method: 'GET',
      path: '/api/v1/partner/tickets/{ticket_id}/qr',
      successMediaType: 'image/svg+xml',
      parameters: [{ name: 'ticket_id', in: 'path', required: true, example: 'tkt_example' }],
    } as const;
    const svgState = initialRequest(svgOperation);
    expect(buildRequest(svgOperation, svgState, 'secret').headers.Accept).toBe('image/svg+xml');
    expect(codeSample('javascript', svgOperation, svgState, 'https://api.example')).toContain(
      'await response.text()',
    );

    const binaryOperation = { ...svgOperation, successMediaType: 'application/pdf' } as const;
    expect(codeSample('javascript', binaryOperation, svgState, 'https://api.example')).toContain(
      'await response.arrayBuffer()',
    );
  });
  it('rotates automatic keys after material edits and preserves manual keys', () => {
    vi.spyOn(crypto, 'randomUUID')
      .mockReturnValueOnce('00000000-0000-4000-8000-000000000001')
      .mockReturnValueOnce('00000000-0000-4000-8000-000000000002');
    const initial = initialRequest(operation);
    expect(materialEdit(initial, { body: { quantity: 2 } }).idempotencyKey).not.toBe(
      initial.idempotencyKey,
    );
    const manual = materialEdit({ ...initial, idempotencyEdited: true }, { body: { quantity: 2 } });
    expect(manual.idempotencyKey).toBe(initial.idempotencyKey);
    expect(manual.idempotencyIntentChanged).toBe(true);
  });
});
