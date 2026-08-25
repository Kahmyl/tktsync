import type { InventoryItem } from '../../admin/types';

export type InventoryTargets = {
  reservedIds: string[];
  reservedKeys: string[];
  ga: Array<{ id: string; key: string; quantity: number }>;
  sectionKeys: string[];
};

export function targetsForEntireEvent(
  inventory: InventoryItem[],
  mode: 'pricing' | 'restriction',
): InventoryTargets {
  return {
    reservedIds:
      mode === 'restriction'
        ? inventory.filter((item) => item.kind === 'RESERVED').map((item) => item.id)
        : [],
    reservedKeys: [],
    ga: inventory
      .filter((item) => item.kind === 'GA')
      .map((item) => ({ id: item.id, key: item.snapshot_object_key, quantity: item.quantity })),
    sectionKeys:
      mode === 'pricing'
        ? Array.from(
            new Set(
              inventory
                .filter((item) => item.kind === 'RESERVED')
                .map((item) => item.section_object_key),
            ),
          )
        : [],
  };
}

export function targetsForSection(
  inventory: InventoryItem[],
  sectionKey: string,
  mode: 'pricing' | 'restriction',
): InventoryTargets {
  const items = inventory.filter((item) => item.section_object_key === sectionKey);
  const hasReserved = items.some((item) => item.kind === 'RESERVED');
  return {
    reservedIds:
      mode === 'restriction'
        ? items.filter((item) => item.kind === 'RESERVED').map((item) => item.id)
        : [],
    reservedKeys: [],
    ga: items
      .filter((item) => item.kind === 'GA')
      .map((item) => ({ id: item.id, key: item.snapshot_object_key, quantity: item.quantity })),
    sectionKeys: mode === 'pricing' && hasReserved ? [sectionKey] : [],
  };
}
