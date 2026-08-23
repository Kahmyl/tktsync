import { describe, expect, it } from 'vitest';
import { workflows } from './features/workflows/catalog';

describe('admin workflow catalog', () => {
  it('covers administration, lifecycle, and Reporting operational workflows with contract paths', () => {
    expect(workflows.map((item) => item.title)).toEqual([
      'Venues',
      'Venue layouts',
      'Events',
      'Pricing',
      'Inventory',
      'Blocks',
      'Allocations',
      'Partner configuration',
      'Sales lifecycle',
      'Pause sales',
      'Resume sales',
      'Close sales',
      'Cancel event',
      'Complete event',
      'Ticket operations',
      'Admission operations',
      'Inventory reporting',
      'Commercial reporting',
      'Admission reporting',
      'Audit explorer',
      'Accreditation export',
      'Metrics and alerts',
    ]);
    expect(workflows.every((item) => item.path.startsWith('/api/v1/admin/'))).toBe(true);
  });
});
