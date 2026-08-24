import { Eye, Plus, Save, Send, Trash2, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Button, Field, InlineNotice, Input, Select } from '../../../components/ui';
import type { VenueLayoutDetail } from '../../admin/types';
import { LayoutCanvas } from './LayoutCanvas';
import { fromLayout, stableKey, toLayout, type BuilderObject } from './model';

const palette: Array<[BuilderObject['type'], string]> = [
  ['RESERVED', 'Reserved section'],
  ['GA', 'General admission'],
  ['TABLE', 'Table area'],
  ['STAGE', 'Stage'],
  ['RING', 'Ring'],
  ['FIELD', 'Field'],
];

export function LayoutBuilder({
  venueName,
  layout,
  saving,
  publishing,
  error,
  onClose,
  onSave,
  onPublish,
}: {
  venueName: string;
  layout: VenueLayoutDetail;
  saving: boolean;
  publishing: boolean;
  error?: unknown;
  onClose: () => void;
  onSave: (body: ReturnType<typeof toLayout>) => Promise<void>;
  onPublish: (body: ReturnType<typeof toLayout>) => Promise<void>;
}) {
  const [objects, setObjects] = useState(() => fromLayout(layout));
  const [selectedKey, setSelectedKey] = useState(objects[0]?.object_key ?? '');
  const [preview, setPreview] = useState(false);
  const [dirty, setDirty] = useState(false);
  const selected = objects.find((item) => item.object_key === selectedKey);
  const totals = useMemo(() => toLayout(objects), [objects]);
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault();
    };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
  const update = (key: string, changes: Partial<BuilderObject>) => {
    const regeneratesStructure = Object.keys(changes).some((field) =>
      ['rows', 'seatsPerRow', 'startSeat', 'tables', 'seatsPerTable'].includes(field),
    );
    setObjects((items) =>
      items.map((item) =>
        item.object_key === key
          ? { ...item, ...changes, structural: regeneratesStructure ? undefined : item.structural }
          : item,
      ),
    );
    setDirty(true);
  };
  const add = (type: BuilderObject['type'], label: string) => {
    const object_key = stableKey(label, objects);
    const orientation = ['STAGE', 'RING', 'FIELD'].includes(type);
    const inventoryIndex = objects.filter((item) =>
      ['RESERVED', 'GA', 'TABLE'].includes(item.type),
    ).length;
    const next: BuilderObject = {
      object_key,
      type,
      label,
      x: orientation ? 350 : 40 + (inventoryIndex % 3) * 310,
      y: orientation ? 25 : 130 + Math.floor(inventoryIndex / 3) * 230,
      width: orientation ? 300 : 270,
      height: orientation ? 75 : 180,
      rows: 4,
      seatsPerRow: 10,
      startSeat: 1,
      capacity: 250,
      tables: 4,
      seatsPerTable: 6,
    };
    setObjects((items) => [...items, next]);
    setSelectedKey(object_key);
    setDirty(true);
  };
  const close = () => {
    if (!dirty || window.confirm('You have unsaved layout changes. Leave without saving?'))
      onClose();
  };
  return (
    <div
      className="layout-builder-shell"
      role="dialog"
      aria-modal="true"
      aria-label="Visual floor-plan builder"
    >
      <header className="layout-builder-toolbar">
        <div>
          <strong>{venueName}</strong>
          <span>Layout version {layout.version_number} · Draft</span>
        </div>
        <div className="layout-builder-actions">
          <Button variant="secondary" onClick={() => setPreview((value) => !value)}>
            <Eye size={16} />
            {preview ? 'Edit floor plan' : 'Preview buyer view'}
          </Button>
          <Button
            variant="secondary"
            busy={saving}
            disabled={!dirty || !totals.sections.length}
            onClick={() => void onSave(totals).then(() => setDirty(false))}
          >
            <Save size={16} />
            Save draft
          </Button>
          <Button
            busy={publishing}
            disabled={!totals.sections.length}
            onClick={() => void onPublish(totals)}
          >
            <Send size={16} />
            Publish
          </Button>
          <Button variant="ghost" aria-label="Close builder" onClick={close}>
            <X size={20} />
          </Button>
        </div>
      </header>
      <div className="layout-builder-mobile">
        <InlineNotice>
          Use a larger screen to edit this floor plan. You can review the current arrangement below.
        </InlineNotice>
      </div>
      <div className={`layout-builder-workspace ${preview ? 'previewing' : ''}`}>
        {!preview && (
          <aside className="layout-palette">
            <h2>Add to floor plan</h2>
            {palette.map(([type, label]) => (
              <button key={type} type="button" onClick={() => add(type, label)}>
                <Plus size={15} />
                <span>{label}</span>
              </button>
            ))}
            <small>Drag objects on the canvas. Exact pixel placement is not required.</small>
          </aside>
        )}
        <main className="layout-canvas-wrap">
          <LayoutCanvas
            objects={objects}
            selected={selectedKey}
            onSelect={setSelectedKey}
            onMove={(key, x, y) => update(key, { x, y })}
            preview={preview}
          />
          <div className="layout-canvas-legend">
            <span>{totals.sections.length} areas</span>
            <span>{totals.rows.length} rows</span>
            <span>{totals.seats.length} seats</span>
            <span>
              {totals.ga_zones.reduce((sum, item) => sum + item.default_capacity, 0)} standing
              capacity
            </span>
          </div>
        </main>
        {!preview && (
          <aside className="layout-properties">
            <h2>Properties</h2>
            {selected ? (
              <div className="form-stack">
                <Field label="Name">
                  <Input
                    value={selected.label}
                    onChange={(event) => update(selected.object_key, { label: event.target.value })}
                  />
                </Field>
                <div className="form-grid two">
                  <Field label="Width">
                    <Input
                      type="number"
                      min="80"
                      value={selected.width}
                      onChange={(event) =>
                        update(selected.object_key, { width: Number(event.target.value) })
                      }
                    />
                  </Field>
                  <Field label="Height">
                    <Input
                      type="number"
                      min="50"
                      value={selected.height}
                      onChange={(event) =>
                        update(selected.object_key, { height: Number(event.target.value) })
                      }
                    />
                  </Field>
                </div>
                <Field label="Rotation">
                  <Select
                    value={selected.rotation ?? 0}
                    onChange={(event) =>
                      update(selected.object_key, { rotation: Number(event.target.value) })
                    }
                  >
                    <option value="0">Faces top</option>
                    <option value="90">Faces right</option>
                    <option value="180">Faces bottom</option>
                    <option value="270">Faces left</option>
                  </Select>
                </Field>
                {selected.type === 'RESERVED' && (
                  <>
                    <div className="form-grid two">
                      <Field label="Rows">
                        <Input
                          type="number"
                          min="1"
                          max="26"
                          value={selected.rows}
                          onChange={(event) =>
                            update(selected.object_key, { rows: Number(event.target.value) })
                          }
                        />
                      </Field>
                      <Field label="Seats per row">
                        <Input
                          type="number"
                          min="1"
                          max="100"
                          value={selected.seatsPerRow}
                          onChange={(event) =>
                            update(selected.object_key, { seatsPerRow: Number(event.target.value) })
                          }
                        />
                      </Field>
                    </div>
                    <Field label="Starting seat number">
                      <Input
                        type="number"
                        min="1"
                        value={selected.startSeat}
                        onChange={(event) =>
                          update(selected.object_key, { startSeat: Number(event.target.value) })
                        }
                      />
                    </Field>
                  </>
                )}
                {selected.type === 'GA' && (
                  <Field label="Capacity">
                    <Input
                      type="number"
                      min="1"
                      value={selected.capacity}
                      onChange={(event) =>
                        update(selected.object_key, { capacity: Number(event.target.value) })
                      }
                    />
                  </Field>
                )}
                {selected.type === 'TABLE' && (
                  <div className="form-grid two">
                    <Field label="Tables">
                      <Input
                        type="number"
                        min="1"
                        value={selected.tables}
                        onChange={(event) =>
                          update(selected.object_key, { tables: Number(event.target.value) })
                        }
                      />
                    </Field>
                    <Field label="Seats per table">
                      <Input
                        type="number"
                        min="1"
                        value={selected.seatsPerTable}
                        onChange={(event) =>
                          update(selected.object_key, { seatsPerTable: Number(event.target.value) })
                        }
                      />
                    </Field>
                  </div>
                )}
                <Button
                  variant="danger"
                  onClick={() => {
                    setObjects((items) =>
                      items.filter((item) => item.object_key !== selected.object_key),
                    );
                    setSelectedKey('');
                    setDirty(true);
                  }}
                >
                  <Trash2 size={15} />
                  Delete object
                </Button>
              </div>
            ) : (
              <p>Select an object to edit it.</p>
            )}
            {error ? (
              <InlineNotice tone="error">
                The floor plan could not be saved. Review the values and try again.
              </InlineNotice>
            ) : null}
          </aside>
        )}
      </div>
    </div>
  );
}
