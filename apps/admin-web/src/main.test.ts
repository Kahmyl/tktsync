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
import {
  fromLayout,
  stableKey,
  toLayout,
  type BuilderObject,
} from './features/venues/layout-builder/model';
import type { VenueLayoutDetail } from './features/admin/types';
import {
  targetsForEntireEvent,
  targetsForSection,
} from './features/events/inventory/InventoryTargetPicker';

describe('admin interaction policy', () => {
  it('never automatically retries mutations', () => {
    expect(queryClient.getDefaultOptions().mutations?.retry).toBe(false);
  });

  it('round-trips irregular authoritative structure when only geometry changes', () => {
    const detail: VenueLayoutDetail = {
      id: 'lay_draft',
      venue_id: 'ven_1',
      version_number: 2,
      state: 'DRAFT',
      created_at: '2026-08-24T00:00:00Z',
      published_at: null,
      retired_at: null,
      geometry: {
        objects: [
          {
            object_key: 'vip',
            type: 'RESERVED',
            label: 'VIP',
            x: 10,
            y: 20,
            width: 300,
            height: 180,
          },
        ],
      },
      sections: [
        { object_key: 'vip', name: 'VIP', kind: 'RESERVED', sort_order: 0 },
        { object_key: 'banquet', name: 'Banquet', kind: 'TABLE', sort_order: 1 },
        { object_key: 'floor', name: 'Floor', kind: 'GA', sort_order: 2 },
      ],
      rows: [
        { object_key: 'vip-row-a', section_key: 'vip', label: 'A', sort_order: 7 },
        { object_key: 'vip-row-b', section_key: 'vip', label: 'B', sort_order: 9 },
      ],
      tables: [{ object_key: 'banquet-table-four', section_key: 'banquet', label: 'Table 4' }],
      seats: [
        {
          object_key: 'vip-a-50',
          section_key: 'vip',
          row_key: 'vip-row-a',
          seat_label: '50',
          sort_order: 0,
        },
        {
          object_key: 'vip-a-60',
          section_key: 'vip',
          row_key: 'vip-row-a',
          seat_label: '60',
          sort_order: 10,
        },
        {
          object_key: 'vip-b-1',
          section_key: 'vip',
          row_key: 'vip-row-b',
          seat_label: '1',
          sort_order: 0,
        },
        {
          object_key: 'banquet-four-2',
          section_key: 'banquet',
          table_key: 'banquet-table-four',
          seat_label: '2',
          sort_order: 1,
        },
      ],
      ga_zones: [
        {
          object_key: 'floor-zone-original',
          section_key: 'floor',
          name: 'Main Floor',
          default_capacity: 321,
        },
      ],
    };
    const objects = fromLayout(detail).map((item) =>
      item.object_key === 'vip' ? { ...item, x: 44, y: 55 } : item,
    );
    const output = toLayout(objects);
    expect(output.rows).toEqual(detail.rows);
    expect(output.tables).toEqual(detail.tables);
    expect(output.seats).toEqual(detail.seats);
    expect(output.ga_zones).toEqual(detail.ga_zones);
    expect(output.sections).toEqual(detail.sections);
    expect(output.geometry.objects?.find((item) => item.object_key === 'vip')).toMatchObject({
      x: 44,
      y: 55,
    });
  });

  it('infers a lossless regular reserved generator including its starting number', () => {
    const detail: VenueLayoutDetail = {
      id: 'lay_regular',
      venue_id: 'ven_1',
      version_number: 1,
      state: 'DRAFT',
      created_at: '2026-08-24T00:00:00Z',
      published_at: null,
      retired_at: null,
      geometry: {},
      sections: [{ object_key: 'vip', name: 'VIP', kind: 'RESERVED', sort_order: 7 }],
      rows: ['A', 'B', 'C'].map((label, index) => ({
        object_key: `vip-row-${label.toLowerCase()}`,
        section_key: 'vip',
        label,
        sort_order: index,
      })),
      tables: [],
      seats: ['A', 'B', 'C'].flatMap((label) =>
        Array.from({ length: 6 }, (_, index) => ({
          object_key: `vip-${label.toLowerCase()}-${10 + index}`,
          section_key: 'vip',
          row_key: `vip-row-${label.toLowerCase()}`,
          seat_label: String(10 + index),
          sort_order: index,
        })),
      ),
      ga_zones: [],
    };
    expect(fromLayout(detail)[0]).toMatchObject({
      generatorMode: 'generated',
      rows: 3,
      seatsPerRow: 6,
      startSeat: 10,
    });
    expect(toLayout(fromLayout(detail)).seats).toEqual(detail.seats);
    expect(toLayout(fromLayout(detail)).sections).toEqual(detail.sections);
  });

  it('preserves an existing GA identity while changing capacity', () => {
    const detail: VenueLayoutDetail = {
      id: 'lay_ga',
      venue_id: 'ven_1',
      version_number: 1,
      state: 'DRAFT',
      created_at: '2026-08-24T00:00:00Z',
      published_at: null,
      retired_at: null,
      geometry: {},
      sections: [{ object_key: 'floor', name: 'Floor', kind: 'GA', sort_order: 12 }],
      rows: [],
      tables: [],
      seats: [],
      ga_zones: [
        {
          object_key: 'main-floor-zone',
          section_key: 'floor',
          name: 'Main Floor',
          default_capacity: 321,
        },
      ],
    };
    const changed = fromLayout(detail).map((item) =>
      item.object_key === 'floor' ? { ...item, capacity: 500 } : item,
    );
    expect(toLayout(changed).ga_zones).toEqual([
      {
        object_key: 'main-floor-zone',
        section_key: 'floor',
        name: 'Main Floor',
        default_capacity: 500,
      },
    ]);
  });

  it('preserves every GA zone and MIXED_VISUAL section metadata without structural edits', () => {
    const detail: VenueLayoutDetail = {
      id: 'lay_mixed',
      venue_id: 'ven_1',
      version_number: 1,
      state: 'DRAFT',
      created_at: '2026-08-24T00:00:00Z',
      published_at: null,
      retired_at: null,
      geometry: {},
      sections: [{ object_key: 'mixed', name: 'Mixed', kind: 'MIXED_VISUAL', sort_order: 42 }],
      rows: [],
      tables: [],
      seats: [],
      ga_zones: [
        { object_key: 'zone-a', section_key: 'mixed', name: 'A', default_capacity: 10 },
        { object_key: 'zone-b', section_key: 'mixed', name: 'B', default_capacity: 20 },
      ],
    };
    const output = toLayout(fromLayout(detail));
    expect(output.sections).toEqual(detail.sections);
    expect(output.ga_zones).toEqual(detail.ga_zones);
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

describe('visual floor-plan domain mapping', () => {
  it('generates deterministic rows, seats, tables, GA capacity and orientation geometry', () => {
    const objects: BuilderObject[] = [
      {
        object_key: 'vip',
        type: 'RESERVED',
        label: 'VIP',
        x: 10,
        y: 20,
        width: 300,
        height: 180,
        rows: 2,
        seatsPerRow: 3,
        startSeat: 1,
      },
      {
        object_key: 'banquet',
        type: 'TABLE',
        label: 'Banquet',
        x: 350,
        y: 20,
        width: 250,
        height: 180,
        tables: 2,
        seatsPerTable: 4,
      },
      {
        object_key: 'floor',
        type: 'GA',
        label: 'Main Floor',
        x: 10,
        y: 240,
        width: 590,
        height: 180,
        capacity: 1500,
      },
      {
        object_key: 'stage',
        type: 'STAGE',
        label: 'Main Stage',
        x: 180,
        y: 500,
        width: 300,
        height: 70,
        rotation: 180,
      },
    ];
    const layout = toLayout(objects);
    expect(layout.rows.map((row) => row.object_key)).toEqual(['vip-row-a', 'vip-row-b']);
    expect(layout.seats).toHaveLength(14);
    expect(layout.tables).toHaveLength(2);
    expect(layout.ga_zones[0]).toMatchObject({ name: 'Main Floor', default_capacity: 1500 });
    expect(layout.geometry.objects?.find((item) => item.object_key === 'stage')).toMatchObject({
      type: 'STAGE',
      rotation: 180,
    });
    expect(stableKey('VIP', objects)).toBe('vip-2');
  });
});

describe('pricing inventory target mapping', () => {
  const inventory = [
    {
      kind: 'RESERVED' as const,
      id: 'reserved-id',
      snapshot_object_key: 'vip-a-10',
      section_object_key: 'vip',
      section_name: 'VIP',
      row_label: 'A',
      table_label: null,
      seat_label: '10',
      area_name: '',
      display_label: 'VIP A 10',
      label: '10',
      quantity: 1,
      price_tier_id: null,
    },
    {
      kind: 'GA' as const,
      id: 'ga-id',
      snapshot_object_key: 'main-floor-zone',
      section_object_key: 'main-floor',
      section_name: 'Main Floor',
      row_label: null,
      table_label: null,
      seat_label: null,
      area_name: 'Main Floor',
      display_label: 'Main Floor',
      label: 'Main Floor',
      quantity: 100,
      price_tier_id: null,
    },
  ];

  it('targets a reserved section without expanding individual seats', () => {
    expect(targetsForSection(inventory, 'vip', 'pricing')).toEqual({
      reservedIds: [],
      reservedKeys: [],
      ga: [],
      sectionKeys: ['vip'],
    });
  });

  it('targets a GA area by its authoritative pool snapshot key, not its section key', () => {
    expect(targetsForSection(inventory, 'main-floor', 'pricing')).toEqual({
      reservedIds: [],
      reservedKeys: [],
      ga: [{ id: 'ga-id', key: 'main-floor-zone', quantity: 100 }],
      sectionKeys: [],
    });
  });

  it('targets all reserved sections and all GA pools for the entire event', () => {
    expect(targetsForEntireEvent(inventory, 'pricing')).toEqual({
      reservedIds: [],
      reservedKeys: [],
      ga: [{ id: 'ga-id', key: 'main-floor-zone', quantity: 100 }],
      sectionKeys: ['vip'],
    });
  });
});
