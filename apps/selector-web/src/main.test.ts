import { describe, expect, it, vi } from 'vitest';
import { queryClient } from './features/selection/queryClient';
import { consumeCapability } from './features/selection/capability';
import { clearIntentKey, getIntentKey } from './features/selection/idempotency';
import { humanLabel, remaining, serverOffset } from './features/selection/presentation';

describe('selector security and timer', () => {
  it('uses short-lived server state and non-retrying mutations', () => {
    expect(queryClient.getDefaultOptions().queries?.staleTime).toBe(2_000);
    expect(queryClient.getDefaultOptions().mutations?.retry).toBe(false);
  });
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
  it('does not present machine identifiers as buyer-facing names', () => {
    expect(humanLabel('Summer Show 7467aa88-7976-4b27-b578-8b3268dc42a4', 'Event')).toBe(
      'Summer Show',
    );
    expect(humanLabel('Seat seat_Wo9YQvZ', 'Seat')).toBe('Seat');
  });
});

describe('selector authority semantics', () => {
  it('aligns countdown calculations to server time', () => {
    const clientNow = Date.parse('2026-08-22T10:00:00Z');

    expect(serverOffset('2026-08-22T10:00:05Z', clientNow)).toBe(5000);

    expect(remaining('2026-08-22T10:02:05Z', clientNow + 5000)).toBe('02:00');
  });

  it('reuses one idempotency key for one ambiguous intent', () => {
    const store = new Map<string, string>();
    let sequence = 0;

    const factory = () => {
      sequence += 1;
      return `key-${sequence}`;
    };

    expect(getIntentKey(store, 'reserve:offer-a:1', factory)).toBe('key-1');

    expect(getIntentKey(store, 'reserve:offer-a:1', factory)).toBe('key-1');

    expect(getIntentKey(store, 'reserve:offer-a:2', factory)).toBe('key-2');

    clearIntentKey(store, 'reserve:offer-a:1');

    expect(getIntentKey(store, 'reserve:offer-a:1', factory)).toBe('key-3');
  });
});
