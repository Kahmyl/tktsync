import { useMemo, useState } from 'react';
import { Field, Input, Select } from '../../../components/ui';
import { humanName } from '../../../lib/format';
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

export function InventoryTargetPicker({
  inventory,
  value,
  onChange,
  allowEntireEvent = false,
  mode = 'restriction',
}: {
  inventory: InventoryItem[];
  value: InventoryTargets;
  onChange: (value: InventoryTargets) => void;
  allowEntireEvent?: boolean;
  mode?: 'pricing' | 'restriction';
}) {
  const [scope, setScope] = useState('section');
  const [search, setSearch] = useState('');
  const sections = useMemo(() => {
    const byKey = new Map<string, string>();
    inventory.forEach((item) => byKey.set(item.section_object_key, item.section_name));
    return Array.from(byKey, ([key, name]) => ({ key, name }));
  }, [inventory]);
  const displayLabel = (item: InventoryItem) =>
    item.kind === 'GA'
      ? humanName(item.area_name || item.section_name, 'General admission')
      : [
          item.section_name,
          item.row_label && `Row ${item.row_label}`,
          item.table_label,
          item.seat_label && `Seat ${item.seat_label}`,
        ]
          .filter(Boolean)
          .join(' · ');
  const filtered = inventory.filter((item) =>
    displayLabel(item).toLowerCase().includes(search.toLowerCase()),
  );
  const selectAll = () => onChange(targetsForEntireEvent(inventory, mode));
  const chooseSection = (sectionKey: string) =>
    onChange(targetsForSection(inventory, sectionKey, mode));
  return (
    <div className="inventory-target-picker">
      <Field label="Apply to">
        <Select
          value={scope}
          onChange={(event) => {
            const next = event.target.value;
            setScope(next);
            if (next === 'all') selectAll();
            else onChange({ reservedIds: [], reservedKeys: [], ga: [], sectionKeys: [] });
          }}
        >
          {allowEntireEvent ? <option value="all">Entire event</option> : null}
          <option value="section">Section / area</option>
          <option value="seats">Specific seats or areas</option>
        </Select>
      </Field>
      {scope === 'section' ? (
        <Field label="Section or area">
          <Select value="" onChange={(event) => chooseSection(event.target.value)}>
            <option value="">Choose an area</option>
            {sections.map((section) => (
              <option key={section.key} value={section.key}>
                {humanName(section.name, 'Area')}
              </option>
            ))}
          </Select>
        </Field>
      ) : null}
      {scope === 'seats' ? (
        <>
          <Field label="Find inventory">
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="Search by section, row or seat"
            />
          </Field>
          <div className="inventory-choice-list">
            {filtered.map((item) => {
              const checked =
                item.kind === 'RESERVED'
                  ? value.reservedIds.includes(item.id)
                  : value.ga.some((target) => target.id === item.id);
              return (
                <label key={item.id}>
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => {
                      if (item.kind === 'RESERVED') {
                        const include = !checked;
                        onChange({
                          ...value,
                          reservedIds: include
                            ? [...value.reservedIds, item.id]
                            : value.reservedIds.filter((id) => id !== item.id),
                          reservedKeys: include
                            ? [...value.reservedKeys, item.snapshot_object_key]
                            : value.reservedKeys.filter((key) => key !== item.snapshot_object_key),
                        });
                      } else {
                        onChange({
                          ...value,
                          ga: checked
                            ? value.ga.filter((target) => target.id !== item.id)
                            : [
                                ...value.ga,
                                { id: item.id, key: item.snapshot_object_key, quantity: 1 },
                              ],
                        });
                      }
                    }}
                  />
                  <span>{displayLabel(item)}</span>
                  {mode === 'restriction' && item.kind === 'GA' && checked ? (
                    <Input
                      aria-label={`${displayLabel(item)} quantity`}
                      type="number"
                      min="1"
                      max={item.quantity}
                      value={value.ga.find((target) => target.id === item.id)?.quantity ?? 1}
                      onChange={(event) =>
                        onChange({
                          ...value,
                          ga: value.ga.map((target) =>
                            target.id === item.id
                              ? {
                                  ...target,
                                  quantity: Math.max(
                                    1,
                                    Math.min(item.quantity, Number(event.target.value)),
                                  ),
                                }
                              : target,
                          ),
                        })
                      }
                    />
                  ) : null}
                </label>
              );
            })}
          </div>
        </>
      ) : null}
      <small>
        {value.sectionKeys.length
          ? `${value.sectionKeys.length} area${value.sectionKeys.length === 1 ? '' : 's'} selected`
          : `${value.reservedIds.length || value.reservedKeys.length} reserved seats · ${value.ga.length} general-admission area${value.ga.length === 1 ? '' : 's'} selected`}
      </small>
    </div>
  );
}
