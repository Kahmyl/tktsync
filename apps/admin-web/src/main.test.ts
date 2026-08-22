import { describe, expect, it } from 'vitest';
import { workflows } from './main';

describe('admin workflow catalog', () => {
  it('covers every M9 administration domain with contract paths', () => {
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
      'Ticket operations',
      'Admission operations',
    ]);
    expect(workflows.every((item) => item.path.startsWith('/api/v1/admin/'))).toBe(true);
  });
});
