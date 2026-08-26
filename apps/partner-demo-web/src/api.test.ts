import { afterEach, describe, expect, it, vi } from 'vitest';
import { demoAPI } from './api';
afterEach(() => vi.unstubAllGlobals());
describe('browser API boundary', () => {
  it('calls only the same-origin demo BFF and never supplies a Partner credential', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [] }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    );
    vi.stubGlobal('fetch', fetcher);
    await demoAPI('/events');
    expect(fetcher).toHaveBeenCalledWith(
      '/demo-api/events',
      expect.objectContaining({ headers: { 'content-type': 'application/json' } }),
    );
    expect(JSON.stringify(fetcher.mock.calls)).not.toMatch(/authorization|partner.*credential/i);
  });
});
