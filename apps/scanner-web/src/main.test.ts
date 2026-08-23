import { describe, expect, it } from 'vitest';
import { resultLabel, resultTone } from './features/scanning/outcome';

describe('scanner authoritative outcomes', () => {
  it('distinguishes admission, duplicate, invalid, and unavailable', () => {
    expect(resultTone()).toBe('neutral');
    expect(resultTone('ADMITTED')).toBe('success');
    expect(resultTone('TICKET_ALREADY_ADMITTED')).toBe('warning');
    expect(resultTone('CREDENTIAL_REVOKED')).toBe('danger');
    expect(resultLabel(undefined, 'network')).toBe('AUTHORITY UNAVAILABLE');
  });
});
