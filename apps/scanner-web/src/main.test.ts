import { describe, expect, it } from 'vitest';
import { label, tone } from './main';

describe('scanner authoritative outcomes', () => {
  it('distinguishes admission, duplicate, invalid, and unavailable', () => {
    expect(tone()).toBe('neutral');
    expect(tone('ADMITTED')).toBe('success');
    expect(tone('TICKET_ALREADY_ADMITTED')).toBe('warning');
    expect(tone('CREDENTIAL_REVOKED')).toBe('danger');
    expect(label(undefined, 'network')).toBe('AUTHORITY UNAVAILABLE');
  });
});
