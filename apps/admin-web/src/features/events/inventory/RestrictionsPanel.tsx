import { useState } from 'react';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  Field,
  InlineNotice,
  Input,
  Panel,
  SectionHeading,
  Select,
  StatusPill,
} from '../../../components/ui';
import { formatDateTime, humanName } from '../../../lib/format';
import { adminApi } from '../../admin/api';
import { adminKeys, useIntentMutation } from '../../admin/queries';
import type { InventoryItem, InventoryRestriction } from '../../admin/types';
import { InventoryTargetPicker } from './InventoryTargetPicker';
import type { InventoryTargets } from './inventoryTargets';

const emptyTargets = (): InventoryTargets => ({
  reservedIds: [],
  reservedKeys: [],
  ga: [],
  sectionKeys: [],
});

export function RestrictionsPanel({
  eventId,
  inventory,
  restrictions,
  partners,
}: {
  eventId: string;
  inventory: InventoryItem[];
  restrictions: InventoryRestriction[];
  partners: Array<{ id: string; name: string; state: string }>;
}) {
  const [kind, setKind] = useState<'BLOCK' | 'ALLOCATION' | null>(null);
  const [purpose, setPurpose] = useState('VIP');
  const [reason, setReason] = useState('');
  const [mode, setMode] = useState<'NON_PUBLIC' | 'CHANNEL'>('NON_PUBLIC');
  const [partner, setPartner] = useState('');
  const [targets, setTargets] = useState<InventoryTargets>(emptyTargets());
  const [reclassify, setReclassify] = useState<InventoryRestriction | null>(null);
  const invalidate = [
    adminKeys.restrictions(eventId),
    adminKeys.inventory(eventId),
    adminKeys.inventoryReport(eventId),
    adminKeys.audit(eventId),
  ];
  const create = useIntentMutation({
    intent: () => `${eventId}:${kind}:${purpose}:${JSON.stringify(targets)}`,
    mutationFn: (token, key) =>
      kind === 'BLOCK'
        ? adminApi.createBlock(token, key, eventId, {
            purpose,
            reason,
            reserved_inventory_ids: targets.reservedIds,
            ga_targets: targets.ga.map((item) => ({
              inventory_id: item.id,
              quantity: item.quantity,
            })),
          })
        : adminApi.createAllocation(token, key, eventId, {
            mode,
            partner_id: mode === 'CHANNEL' ? partner : undefined,
            purpose,
            reason,
            release_destination: { kind: 'SHARED' },
            reserved_inventory_ids: targets.reservedIds,
            ga_targets: targets.ga.map((item) => ({
              inventory_id: item.id,
              quantity: item.quantity,
            })),
          }),
    invalidate,
  });
  const release = useIntentMutation({
    intent: (item: InventoryRestriction) => `${item.id}:release`,
    mutationFn: (token, key, item) =>
      item.kind === 'BLOCK'
        ? adminApi.releaseBlock(token, key, item.id)
        : adminApi.releaseAllocation(token, key, item.id),
    invalidate,
  });
  const change = useIntentMutation({
    intent: (item: InventoryRestriction) => `${item.id}:reclassify:${mode}:${partner}`,
    mutationFn: (token, key, item) =>
      adminApi.reclassifyAllocation(token, key, item.id, {
        mode,
        partner_id: mode === 'CHANNEL' ? partner : undefined,
      }),
    invalidate,
  });
  const reset = () => {
    setKind(null);
    setTargets(emptyTargets());
    setReason('');
    create.reset();
  };
  return (
    <Panel>
      <SectionHeading
        title="Blocks & allocations"
        description="Reserve inventory for guests, sponsors, internal use or controlled sales channels"
        actions={
          <div className="table-actions">
            <Button size="small" variant="secondary" onClick={() => setKind('BLOCK')}>
              Block inventory
            </Button>
            <Button size="small" onClick={() => setKind('ALLOCATION')}>
              Create allocation
            </Button>
          </div>
        }
      />
      <div className="panel-divider" />
      {restrictions.length ? (
        <div className="restriction-list">
          {restrictions.map((item) => (
            <article key={item.id}>
              <div>
                <div className="restriction-heading">
                  <strong>
                    {humanName(
                      item.purpose,
                      item.kind === 'BLOCK' ? 'Inventory block' : 'Allocation',
                    )}
                  </strong>
                  <StatusPill
                    label={
                      item.state === 'ACTIVE'
                        ? 'Active'
                        : item.state === 'RELEASED'
                          ? 'Released'
                          : 'Closed'
                    }
                    tone={item.state === 'ACTIVE' ? 'warning' : 'neutral'}
                  />
                </div>
                <p>
                  {item.kind === 'BLOCK'
                    ? 'Inventory block'
                    : item.mode === 'CHANNEL'
                      ? `Partner allocation${item.partner_name ? ` · ${humanName(item.partner_name, 'Partner')}` : ''}`
                      : 'Internal allocation'}{' '}
                  · {item.reserved_quantity} seats · {item.ga_quantity} standing
                </p>
                <small>
                  {item.inventory_labels
                    .slice(0, 4)
                    .map((label) => humanName(label, 'Inventory'))
                    .join(', ') || 'Inventory selection'}{' '}
                  · Created {formatDateTime(item.created_at)}
                </small>
                {item.reason ? <small>Reason: {item.reason}</small> : null}
              </div>
              {item.state === 'ACTIVE' ? (
                <div className="table-actions">
                  {item.kind === 'ALLOCATION' ? (
                    <Button
                      size="small"
                      variant="ghost"
                      onClick={() => {
                        setReclassify(item);
                        setMode(item.mode ?? 'NON_PUBLIC');
                        setPartner(item.partner_id ?? '');
                      }}
                    >
                      Reclassify
                    </Button>
                  ) : null}
                  <Button
                    size="small"
                    variant="secondary"
                    busy={release.isPending && release.variables?.id === item.id}
                    onClick={() => {
                      if (
                        window.confirm(
                          `Release this ${item.kind === 'BLOCK' ? 'block' : 'allocation'}?`,
                        )
                      )
                        void release.mutateAsync(item);
                    }}
                  >
                    Release
                  </Button>
                </div>
              ) : null}
            </article>
          ))}
        </div>
      ) : (
        <EmptyState
          title="No blocks or allocations"
          description="Available inventory has no active operational restrictions."
        />
      )}
      <Dialog
        open={Boolean(kind)}
        title={kind === 'BLOCK' ? 'Block inventory' : 'Create allocation'}
        description={
          kind === 'BLOCK'
            ? 'Keep selected inventory out of public sale.'
            : 'Assign selected inventory to an internal pool or selling partner.'
        }
        onClose={reset}
      >
        <div className="dialog-body form-stack">
          {kind === 'BLOCK' ? (
            <Field label="Purpose">
              <Select value={purpose} onChange={(event) => setPurpose(event.target.value)}>
                {[
                  'VIP',
                  'Sponsor',
                  'Complimentary',
                  'Media',
                  'Production',
                  'Holdback',
                  'Other',
                ].map((item) => (
                  <option key={item}>{item}</option>
                ))}
              </Select>
            </Field>
          ) : (
            <>
              <Field label="Allocation type">
                <Select
                  value={mode}
                  onChange={(event) => setMode(event.target.value as typeof mode)}
                >
                  <option value="NON_PUBLIC">Internal / non-public pool</option>
                  <option value="CHANNEL">Selling partner</option>
                </Select>
              </Field>
              {mode === 'CHANNEL' ? (
                <Field label="Partner">
                  <Select value={partner} onChange={(event) => setPartner(event.target.value)}>
                    <option value="">Choose a partner</option>
                    {partners
                      .filter((item) => item.state === 'ACTIVE')
                      .map((item) => (
                        <option key={item.id} value={item.id}>
                          {humanName(item.name, 'Partner')}
                        </option>
                      ))}
                  </Select>
                </Field>
              ) : null}
              <Field label="Purpose">
                <Input
                  value={purpose}
                  onChange={(event) => setPurpose(event.target.value)}
                  placeholder="e.g. Sponsor allocation"
                />
              </Field>
            </>
          )}
          <Field label="Reason" hint="Optional operational context">
            <Input value={reason} onChange={(event) => setReason(event.target.value)} />
          </Field>
          <InventoryTargetPicker inventory={inventory} value={targets} onChange={setTargets} />
          {create.error ? <ErrorState error={create.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={reset}>
            Cancel
          </Button>
          <Button
            busy={create.isPending}
            disabled={
              !purpose.trim() ||
              (!targets.reservedIds.length && !targets.ga.length) ||
              (mode === 'CHANNEL' && !partner)
            }
            onClick={() => void create.mutateAsync(undefined).then(reset)}
          >
            {kind === 'BLOCK' ? 'Create block' : 'Create allocation'}
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={Boolean(reclassify)}
        title="Reclassify allocation"
        description="Change how this active allocation is used without releasing its inventory."
        onClose={() => setReclassify(null)}
      >
        <div className="dialog-body form-stack">
          <InlineNotice>
            This changes the allocation classification; its selected inventory remains controlled.
          </InlineNotice>
          <Field label="Allocation type">
            <Select value={mode} onChange={(event) => setMode(event.target.value as typeof mode)}>
              <option value="NON_PUBLIC">Internal / non-public pool</option>
              <option value="CHANNEL">Selling partner</option>
            </Select>
          </Field>
          {mode === 'CHANNEL' ? (
            <Field label="Partner">
              <Select value={partner} onChange={(event) => setPartner(event.target.value)}>
                <option value="">Choose a partner</option>
                {partners.map((item) => (
                  <option key={item.id} value={item.id}>
                    {humanName(item.name, 'Partner')}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setReclassify(null)}>
            Cancel
          </Button>
          <Button
            busy={change.isPending}
            disabled={mode === 'CHANNEL' && !partner}
            onClick={() =>
              reclassify && void change.mutateAsync(reclassify).then(() => setReclassify(null))
            }
          >
            Apply classification
          </Button>
        </DialogActions>
      </Dialog>
    </Panel>
  );
}
