import type { ScanResult } from './types';

export const resultTone = (outcome?: string) =>
  !outcome
    ? 'neutral'
    : outcome === 'ADMITTED' || outcome === 'MANUAL_OVERRIDE_ADMITTED'
      ? 'success'
      : outcome === 'TICKET_ALREADY_ADMITTED'
        ? 'warning'
        : 'danger';

export const resultLabel = (result?: ScanResult, error?: string) =>
  error ? 'AUTHORITY UNAVAILABLE' : (result?.result?.replaceAll('_', ' ') ?? 'READY TO SCAN');
