import type { PointerEvent } from 'react';
import type { BuilderObject } from './model';

export function LayoutCanvas({
  objects,
  selected,
  onSelect,
  onMove,
  preview = false,
}: {
  objects: BuilderObject[];
  selected?: string;
  onSelect?: (key: string) => void;
  onMove?: (key: string, x: number, y: number) => void;
  preview?: boolean;
}) {
  const startDrag = (event: PointerEvent<SVGGElement>, item: BuilderObject) => {
    if (preview || !onMove) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    const origin = { x: event.clientX, y: event.clientY, itemX: item.x, itemY: item.y };
    const move = (next: globalThis.PointerEvent) =>
      onMove(
        item.object_key,
        Math.max(0, origin.itemX + next.clientX - origin.x),
        Math.max(0, origin.itemY + next.clientY - origin.y),
      );
    const stop = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', stop);
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', stop);
  };
  return (
    <svg
      className={`layout-canvas ${preview ? 'layout-preview-canvas' : ''}`}
      viewBox="0 0 1000 650"
      role="img"
      aria-label="Venue floor plan"
    >
      <defs>
        <pattern id="floor-grid" width="25" height="25" patternUnits="userSpaceOnUse">
          <path d="M 25 0 L 0 0 0 25" fill="none" stroke="currentColor" opacity=".08" />
        </pattern>
      </defs>
      <rect width="1000" height="650" fill="url(#floor-grid)" />
      {objects.map((item) => {
        const orientation = ['STAGE', 'RING', 'FIELD'].includes(item.type);
        const seatCount =
          item.type === 'RESERVED'
            ? (item.rows ?? 0) * (item.seatsPerRow ?? 0)
            : item.type === 'TABLE'
              ? (item.tables ?? 0) * (item.seatsPerTable ?? 0)
              : item.capacity;
        return (
          <g
            key={item.object_key}
            transform={`translate(${item.x} ${item.y}) rotate(${item.rotation ?? 0} ${item.width / 2} ${item.height / 2})`}
            className={`layout-object ${orientation ? 'orientation-object' : ''} ${selected === item.object_key ? 'selected' : ''}`}
            onPointerDown={(event) => startDrag(event, item)}
            onClick={() => onSelect?.(item.object_key)}
            tabIndex={preview ? undefined : 0}
            role={preview ? undefined : 'button'}
            aria-label={`${item.label}, ${item.type.toLowerCase()}`}
          >
            <rect width={item.width} height={item.height} rx={orientation ? 10 : 18} />
            <text
              x={item.width / 2}
              y={item.height / 2 - 8}
              textAnchor="middle"
              className="layout-object-title"
            >
              {item.label}
            </text>
            <text
              x={item.width / 2}
              y={item.height / 2 + 18}
              textAnchor="middle"
              className="layout-object-meta"
            >
              {orientation
                ? item.type === 'STAGE'
                  ? 'Audience faces this way'
                  : item.type
                : `${seatCount ?? 0} ${item.type === 'GA' ? 'capacity' : 'seats'}`}
            </text>
          </g>
        );
      })}
    </svg>
  );
}
