import { describe, expect, it } from 'vitest';
import { queryClient } from './app/queryClient';
import { admissionLabel, eventStateMeta, formatMoney, initials, optionalISO } from './lib/format';

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
});
