import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

describe('Vercel deployment routes', () => {
  const config = JSON.parse(readFileSync(new URL('../vercel.json', import.meta.url), 'utf8')) as {
    rewrites: Array<{ source: string; destination: string }>;
  };

  it('routes the BFF and secure checkout return before SPA fallbacks', () => {
    expect(config.rewrites.slice(0, 2)).toEqual([
      { source: '/demo-api/:path*', destination: '/api/index?path=/demo-api/:path*' },
      { source: '/checkout/return', destination: '/api/index?path=/checkout/return' },
    ]);
  });

  it('keeps every browser route refreshable', () => {
    expect(config.rewrites.map((item) => item.source)).toEqual(
      expect.arrayContaining(['/connections', '/events/:path*', '/checkout', '/ticket']),
    );
  });
});
