import { describe, expect, it, vi } from 'vitest';
import { consumeCapability, remaining } from './main';

describe('selector security and timer', () => {
  it('consumes the fragment and removes it from the visible URL', () => {
    const replaceState = vi.fn();
    expect(
      consumeCapability(
        { hash: '#sel1.secret', pathname: '/s', search: '?brand=one' },
        { replaceState },
      ),
    ).toBe('sel1.secret');
    expect(replaceState).toHaveBeenCalledWith(null, '', '/s?brand=one');
  });
  it('uses server-aligned absolute expiry countdown semantics', () => {
    expect(remaining('2026-08-22T10:02:05Z', Date.parse('2026-08-22T10:00:00Z'))).toBe('02:05');
  });
});
