import type { ReplaceLayoutBody, VenueLayoutDetail } from '../../admin/types';

export type BuilderObject = {
  object_key: string;
  type: 'RESERVED' | 'GA' | 'TABLE' | 'STAGE' | 'RING' | 'FIELD';
  label: string;
  sourceLabel?: string;
  x: number;
  y: number;
  width: number;
  height: number;
  rotation?: number;
  rows?: number;
  seatsPerRow?: number;
  startSeat?: number;
  capacity?: number;
  tables?: number;
  seatsPerTable?: number;
  structural?: Pick<ReplaceLayoutBody, 'rows' | 'tables' | 'seats' | 'ga_zones'>;
};

export function stableKey(label: string, existing: BuilderObject[]) {
  const base =
    label
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '') || 'area';
  let key = base;
  let suffix = 2;
  while (existing.some((item) => item.object_key === key)) key = `${base}-${suffix++}`;
  return key;
}

export function fromLayout(layout: VenueLayoutDetail): BuilderObject[] {
  const geometry = new Map((layout.geometry?.objects ?? []).map((item) => [item.object_key, item]));
  const inventory = layout.sections.map((section, index) => {
    const visual = geometry.get(section.object_key);
    const rows = layout.rows.filter((row) => row.section_key === section.object_key);
    const tables = layout.tables.filter((table) => table.section_key === section.object_key);
    const seats = layout.seats.filter((seat) => seat.section_key === section.object_key);
    const zone = layout.ga_zones.find((item) => item.section_key === section.object_key);
    return {
      object_key: section.object_key,
      type: section.kind === 'MIXED_VISUAL' ? 'RESERVED' : section.kind,
      label: section.name,
      sourceLabel: section.name,
      x: visual?.x ?? 70 + (index % 3) * 230,
      y: visual?.y ?? 80 + Math.floor(index / 3) * 180,
      width: visual?.width ?? 190,
      height: visual?.height ?? 140,
      rotation: visual?.rotation ?? 0,
      rows: rows.length || 4,
      seatsPerRow: rows.length
        ? Math.max(
            ...rows.map((row) => seats.filter((seat) => seat.row_key === row.object_key).length),
          )
        : 10,
      startSeat: 1,
      capacity: zone?.default_capacity ?? 100,
      tables: tables.length || 4,
      seatsPerTable: tables.length
        ? Math.max(
            ...tables.map(
              (table) => seats.filter((seat) => seat.table_key === table.object_key).length,
            ),
          )
        : 6,
      structural: {
        rows: rows.map((row) => ({ ...row })),
        tables: tables.map((table) => ({ ...table })),
        seats: seats.map((seat) => ({ ...seat })),
        ga_zones: zone ? [{ ...zone }] : [],
      },
    } as BuilderObject;
  });
  const orientation = (layout.geometry?.objects ?? [])
    .filter((item) => ['STAGE', 'RING', 'FIELD'].includes(item.type))
    .map((item) => ({ ...item, type: item.type as BuilderObject['type'] }));
  return [...inventory, ...orientation];
}

function rowLabel(index: number) {
  let value = '';
  for (let number = index + 1; number > 0; number = Math.floor((number - 1) / 26))
    value = String.fromCharCode(65 + ((number - 1) % 26)) + value;
  return value;
}

export function toLayout(objects: BuilderObject[]): ReplaceLayoutBody {
  const inventory = objects.filter(
    (item): item is BuilderObject & { type: 'RESERVED' | 'GA' | 'TABLE' } =>
      item.type === 'RESERVED' || item.type === 'GA' || item.type === 'TABLE',
  );
  const rows: ReplaceLayoutBody['rows'] = [];
  const tables: ReplaceLayoutBody['tables'] = [];
  const seats: ReplaceLayoutBody['seats'] = [];
  const ga_zones: ReplaceLayoutBody['ga_zones'] = [];
  inventory.forEach((item) => {
    if (item.structural) {
      rows.push(...item.structural.rows);
      tables.push(...item.structural.tables);
      seats.push(...item.structural.seats);
      ga_zones.push(
        ...item.structural.ga_zones.map((zone) =>
          item.type === 'GA' && item.label !== item.sourceLabel
            ? {
                ...zone,
                name: item.label,
                default_capacity: item.capacity ?? zone.default_capacity,
              }
            : zone,
        ),
      );
      return;
    }
    if (item.type === 'RESERVED') {
      for (let rowIndex = 0; rowIndex < (item.rows ?? 1); rowIndex += 1) {
        const label = rowLabel(rowIndex);
        const rowKey = `${item.object_key}-row-${label.toLowerCase()}`;
        rows.push({
          object_key: rowKey,
          section_key: item.object_key,
          label,
          sort_order: rowIndex,
        });
        for (let seatIndex = 0; seatIndex < (item.seatsPerRow ?? 1); seatIndex += 1) {
          const number = (item.startSeat ?? 1) + seatIndex;
          seats.push({
            object_key: `${item.object_key}-${label.toLowerCase()}-${number}`,
            section_key: item.object_key,
            row_key: rowKey,
            table_key: '',
            seat_label: String(number),
            sort_order: seatIndex,
          });
        }
      }
    } else if (item.type === 'TABLE') {
      for (let tableIndex = 0; tableIndex < (item.tables ?? 1); tableIndex += 1) {
        const tableKey = `${item.object_key}-table-${tableIndex + 1}`;
        tables.push({
          object_key: tableKey,
          section_key: item.object_key,
          label: `Table ${tableIndex + 1}`,
        });
        for (let seatIndex = 0; seatIndex < (item.seatsPerTable ?? 1); seatIndex += 1)
          seats.push({
            object_key: `${tableKey}-seat-${seatIndex + 1}`,
            section_key: item.object_key,
            row_key: '',
            table_key: tableKey,
            seat_label: String(seatIndex + 1),
            sort_order: seatIndex,
          });
      }
    } else if (item.type === 'GA') {
      ga_zones.push({
        object_key: `${item.object_key}-zone`,
        section_key: item.object_key,
        name: item.label,
        default_capacity: item.capacity ?? 1,
      });
    }
  });
  return {
    geometry: {
      canvas: { width: 1000, height: 650 },
      objects: objects.map((item) => ({
        object_key: item.object_key,
        type: item.type,
        label: item.label,
        x: item.x,
        y: item.y,
        width: item.width,
        height: item.height,
        rotation: item.rotation,
      })),
    },
    sections: inventory.map((item, index) => ({
      object_key: item.object_key,
      name: item.label,
      kind: item.type,
      sort_order: index,
    })),
    rows,
    tables,
    seats,
    ga_zones,
  };
}
