import { describe, expect, it } from 'vitest';
import { queryClient } from './features/scanning/queryClient';
import { outcomePresentation } from './features/scanning/outcome';

describe('scanner authoritative outcomes', () => {
  it('keeps authority mutations non-retrying', () => {
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
