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

export function InventoryTargetPicker({
  inventory,
  value,
  onChange,
  allowEntireEvent = false,
}: {
  inventory: InventoryItem[];
  value: InventoryTargets;
  onChange: (value: InventoryTargets) => void;
  allowEntireEvent?: boolean;
}) {
  const [scope, setScope] = useState('section');
  const [search, setSearch] = useState('');
  const sections = useMemo(
    () =>
      Array.from(
        new Set(
          inventory.map((item) =>
            humanName(
              item.label.split(' · ')[0],
              item.kind === 'GA' ? 'General admission' : 'Reserved seating',
            ),
          ),
        ),
      ),
    [inventory],
  );
  const filtered = inventory.filter((item) =>
    humanName(item.label, 'Inventory').toLowerCase().includes(search.toLowerCase()),
  );
  const selectAll = () =>
    onChange({
      reservedIds: inventory.filter((item) => item.kind === 'RESERVED').map((item) => item.id),
      reservedKeys: inventory
        .filter((item) => item.kind === 'RESERVED')
        .map((item) => item.snapshot_object_key),
      ga: inventory
        .filter((item) => item.kind === 'GA')
        .map((item) => ({ id: item.id, key: item.snapshot_object_key, quantity: item.quantity })),
      sectionKeys: [],
    });
  const chooseSection = (section: string) => {
    const items = inventory.filter((item) => humanName(item.label, '').startsWith(section));
    onChange({
      reservedIds: items.filter((item) => item.kind === 'RESERVED').map((item) => item.id),
      reservedKeys: items
        .filter((item) => item.kind === 'RESERVED')
        .map((item) => item.snapshot_object_key),
      ga: items
        .filter((item) => item.kind === 'GA')
        .map((item) => ({ id: item.id, key: item.snapshot_object_key, quantity: item.quantity })),
      // The inventory read model exposes seat/pool object keys, not the parent
      // section object key. Apply the friendly area selection through those
      // concrete targets so we never send seat keys as section scopes.
      sectionKeys: [],
    });
  };
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
              <option key={section} value={section}>
                {section}
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
                  <span>
                    {humanName(
                      item.label,
                      item.kind === 'GA' ? 'General admission' : 'Reserved seat',
                    )}
                  </span>
                  {item.kind === 'GA' && checked ? (
                    <Input
                      aria-label={`${item.label} quantity`}
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
        {value.reservedIds.length} reserved seats ·{' '}
        {value.ga.reduce((sum, item) => sum + item.quantity, 0)} general-admission tickets selected
      </small>
    </div>
  );
}
