import { describe, expect, it } from 'vitest';
import { queryClient } from './features/scanning/queryClient';
import { resultLabel, resultTone } from './features/scanning/outcome';

describe('scanner authoritative outcomes', () => {
  it('keeps authority mutations non-retrying', () => {
    expect(queryClient.getDefaultOptions().mutations?.retry).toBe(false);
  });
  it('distinguishes admission, duplicate, invalid, and unavailable', () => {
    expect(resultTone()).toBe('neutral');
    expect(resultTone('ADMITTED')).toBe('success');
    expect(resultTone('TICKET_ALREADY_ADMITTED')).toBe('warning');
    expect(resultTone('CREDENTIAL_REVOKED')).toBe('danger');
    expect(resultLabel(undefined, 'network')).toBe('AUTHORITY UNAVAILABLE');
  });
});
