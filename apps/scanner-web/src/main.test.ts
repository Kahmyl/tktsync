import { describe, expect, it } from 'vitest';
import { isPhoneDevice } from './features/scanning/device';
import { queryClient } from './features/scanning/queryClient';
import { humanLabel, outcomePresentation, ticketLocation } from './features/scanning/outcome';

describe('scanner outcomes', () => {
  it('keeps scan submissions non-retrying', () => {
    expect(queryClient.getDefaultOptions().mutations?.retry).toBe(false);
  });
  it('distinguishes admission, duplicate, invalid, and unavailable', () => {
    expect(outcomePresentation(undefined, false, 'Final').tone).toBe('ready');
    expect(outcomePresentation({ result: 'ADMITTED' }, false, 'Final').title).toBe('Admit guest');
    expect(outcomePresentation({ result: 'TICKET_ALREADY_ADMITTED' }, false, 'Final').tone).toBe(
      'warning',
    );
    expect(outcomePresentation({ result: 'CREDENTIAL_REVOKED' }, false, 'Final').tone).toBe(
      'danger',
    );
    expect(outcomePresentation(undefined, true, 'Final').title).toBe("Can't verify ticket");
  });
});

describe('scanner device and display labels', () => {
  it('distinguishes phones from resized laptop and tablet browsers', () => {
    expect(isPhoneDevice({ userAgentData: { mobile: true }, userAgent: 'Chromium' })).toBe(true);
    expect(isPhoneDevice({ userAgentData: { mobile: false }, userAgent: 'Chromium' })).toBe(false);
    expect(isPhoneDevice({ userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0)' })).toBe(true);
    expect(isPhoneDevice({ userAgent: 'Mozilla/5.0 (iPad; CPU OS 18_0)' })).toBe(false);
  });

  it('never turns backend identifiers into operator-facing labels', () => {
    expect(humanLabel('evt_01abc', 'Untitled event')).toBe('Untitled event');
    expect(humanLabel('5b52b1d2-3a33-4b50-96cd-80fc6d09d22a', 'Gate operator')).toBe(
      'Gate operator',
    );
    expect(
      ticketLocation({
        result: 'ADMITTED',
        ticket: { display: { section: 'sec_private', row: 'row_private', seat: 'seat_private' } },
      }),
    ).toBe('');
  });
});
