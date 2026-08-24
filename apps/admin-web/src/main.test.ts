import { describe, expect, it } from 'vitest';
import { queryClient } from './app/queryClient';
import {
  admissionLabel,
  eventStateMeta,
  formatMoney,
  humanDomainLabel,
  humanGateReference,
  humanName,
  initials,
  optionalISO,
} from './lib/format';
import { isEmailAddress } from './features/users/validation';
import { passwordValidation } from './features/auth/password';

describe('admin interaction policy', () => {
  it('never automatically retries mutations', () => {
    expect(queryClient.getDefaultOptions().mutations?.retry).toBe(false);
  });

  it('uses operator-facing lifecycle copy', () => {
    expect(eventStateMeta.ON_SALE).toEqual({ label: 'On sale', tone: 'positive' });
    expect(eventStateMeta.SALES_CLOSED).toEqual({ label: 'Sales closed', tone: 'info' });
    expect(eventStateMeta.CANCELLED).toEqual({ label: 'Cancelled', tone: 'critical' });
  });

  it('transforms contract minor units into display currency', () => {
    expect(formatMoney(250_000, 'NGN')).toMatch(/2,500/);
  });

  it('normalizes an entered local date-time and omits a blank optional value', () => {
    expect(optionalISO('')).toBeUndefined();
    expect(optionalISO('2026-08-23T10:30')).toMatch(/^2026-08-23T/);
  });

  it('maps authoritative admission outcomes and session display values', () => {
    expect(admissionLabel('ALREADY_ADMITTED')).toBe('Already admitted');
    expect(admissionLabel('INVALID_CREDENTIAL')).toBe('Invalid');
    expect(initials('Amina Okafor')).toBe('AO');
  });

  it('accepts email addresses and rejects internal identity IDs for administrator provisioning', () => {
    expect(isEmailAddress('operator@example.com')).toBe(true);
    expect(isEmailAddress('3fd1a5af-18c4-4dcf-91a4-0ee6944011d9')).toBe(false);
  });

  it('requires a strong matching password for invitation and recovery completion', () => {
    expect(passwordValidation('short', 'short')).toContain('12');
    expect(passwordValidation('a secure passphrase', 'a different passphrase')).toContain(
      'do not match',
    );
    expect(passwordValidation('a secure passphrase', 'a secure passphrase')).toBe('');
  });

  it('removes machine identifiers from names and translates domain codes', () => {
    expect(humanName('Reservation Event 7467aa88-7976-4b27-b578-8b3268dc42a4')).toBe(
      'Reservation Event',
    );
    expect(humanName('Venue ven_h8YoyBSdSTCuO52AIH6pgA')).toBe('Venue');
    expect(humanDomainLabel('APP_USER')).toBe('Administrator');
    expect(humanDomainLabel('MANUAL_OVERRIDE_ADMITTED')).toBe('Admitted by manual override');
    expect(humanGateReference('0608a0dd-c148-4550-bf19-b0bbe4ea13f8')).toBe('Scanner device');
    expect(humanGateReference('North entrance')).toBe('North entrance');
  });
});
