import {
  AlertTriangle,
  Check,
  CircleDollarSign,
  Pause,
  Play,
  Plus,
  ShieldCheck,
} from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  EventStatus,
  Field,
  InlineNotice,
  Input,
  LoadingState,
  MetricCard,
  PageHeader,
  Panel,
  PanelBody,
  ProgressBar,
  SectionHeading,
  Select,
  StatusPill,
  Textarea,
} from '../../components/ui';
import {
  admissionLabel,
  formatDateTime,
  formatMoney,
  formatNumber,
  friendlyOperation,
  timeAgo,
} from '../../lib/format';
import { adminApi } from '../admin/api';
import {
  adminKeys,
  useAdmissions,
  useEvent,
  useEventWorkspace,
  useIntentMutation,
  useTickets,
  useVenue,
} from '../admin/queries';
import type { EventState } from '../admin/types';

const tabs = [
  ['overview', 'Overview'],
  ['layout', 'Layout & seats'],
  ['pricing', 'Pricing'],
  ['inventory', 'Inventory'],
  ['partners', 'Partners'],
  ['sales', 'Sales'],
  ['admissions', 'Admissions'],
  ['reports', 'Reports'],
] as const;
type Tab = (typeof tabs)[number][0];
type LifecycleAction = 'open-sales' | 'pause-sales' | 'resume-sales' | 'close-sales' | 'complete';

export function EventDetailPage() {
  const { eventId = '' } = useParams();
  const [tab, setTab] = useState<Tab>('overview');
  const event = useEvent(eventId);
  const workspace = useEventWorkspace(eventId);
  const venue = useVenue(event.data?.venue_id ?? '');
  const tickets = useTickets('', eventId);
  const admissions = useAdmissions(eventId);
  const [priceOpen, setPriceOpen] = useState(false);
  const [priceName, setPriceName] = useState('');
  const [priceCode, setPriceCode] = useState('');
  const [priceAmount, setPriceAmount] = useState('');
  const [currency, setCurrency] = useState('NGN');
  const [layoutId, setLayoutId] = useState('');
  const [assignTierId, setAssignTierId] = useState('');
  const [confirm, setConfirm] = useState<{
    action: LifecycleAction | 'cancel';
    title: string;
    description: string;
  } | null>(null);
  const [reason, setReason] = useState('');

  const invalidate = [
    adminKeys.event(eventId),
    adminKeys.configuration(eventId),
    adminKeys.inventory(eventId),
    adminKeys.inventoryReport(eventId),
    adminKeys.salesReport(eventId),
    adminKeys.admissionReport(eventId),
    adminKeys.audit(eventId),
    adminKeys.events(),
    adminKeys.dashboard,
  ];
  const lifecycle = useIntentMutation({
    intent: (variables: { action: LifecycleAction | 'cancel'; reason?: string }) =>
      `${eventId}:${variables.action}:${variables.reason ?? ''}`,
    mutationFn: (token, key, variables) =>
      variables.action === 'cancel'
        ? adminApi.cancelEvent(token, key, eventId, variables.reason ?? '')
        : adminApi.lifecycle(token, key, eventId, variables.action),
    invalidate,
  });
  const policy = useIntentMutation({
    intent: () => `${eventId}:policy`,
    mutationFn: (token, key) => adminApi.configurePolicy(token, key, eventId),
    invalidate,
  });
  const price = useIntentMutation({
    intent: () => `${eventId}:price:${priceCode}:${priceAmount}:${currency}`,
    mutationFn: (token, key) =>
      adminApi.createPriceTier(token, key, eventId, {
        code: priceCode.trim().toUpperCase(),
        name: priceName.trim(),
        amount_minor: Math.round(Number(priceAmount) * 100),
        currency: currency.trim().toUpperCase(),
      }),
    invalidate,
  });
  const materialize = useIntentMutation({
    intent: () => `${eventId}:layout:${layoutId}`,
    mutationFn: (token, key) => adminApi.materializeLayout(token, key, eventId, layoutId),
    invalidate,
  });
  const access = useIntentMutation({
    intent: (variables: { partnerId: string; enabled: boolean }) =>
      `${eventId}:access:${variables.partnerId}:${variables.enabled}`,
    mutationFn: (token, key, variables) =>
      adminApi.setEventAccess(token, key, eventId, variables.partnerId, variables.enabled),
    invalidate,
  });
  const assign = useIntentMutation({
    intent: () => `${eventId}:assign:${assignTierId}`,
    mutationFn: (token, key) =>
      adminApi.assignPricing(token, key, eventId, {
        price_tier_id: assignTierId,
        reserved_object_keys: (workspace.inventory.data?.inventory ?? [])
          .filter((item) => item.kind === 'RESERVED')
          .map((item) => item.snapshot_object_key),
        ga_pool_object_keys: (workspace.inventory.data?.inventory ?? [])
          .filter((item) => item.kind === 'GA')
          .map((item) => item.snapshot_object_key),
      }),
    invalidate,
  });

  if (event.isLoading) return <LoadingState rows={8} />;
  if (event.error || !event.data)
    return <ErrorState error={event.error} onRetry={() => void event.refetch()} />;
  const detail = event.data;
  const configuration = workspace.configuration.data;
  const inventoryReport = workspace.inventoryReport.data;
  const sales = workspace.salesReport.data;
  const admissionReport = workspace.admissionReport.data;
  const setup = {
    policy: Boolean(configuration?.transaction_policy),
    layout: Boolean(configuration?.layout?.finalized_at),
    pricing: Boolean(configuration?.price_tiers.some((tier) => tier.state === 'ACTIVE')),
    inventory: Boolean((workspace.inventory.data?.inventory.length ?? 0) > 0),
  };
  const primary = primaryAction(detail.state);
  const activeTiers = configuration?.price_tiers.filter((tier) => tier.state === 'ACTIVE') ?? [];
  const publishedLayouts =
    venue.layouts.data?.filter((layout) => layout.state === 'PUBLISHED') ?? [];
  const operationError =
    lifecycle.error ??
    policy.error ??
    price.error ??
    materialize.error ??
    access.error ??
    assign.error;

  const runPrimary = () => {
    if (!primary) return;
    if (
      primary.action === 'open-sales' ||
      primary.action === 'pause-sales' ||
      primary.action === 'resume-sales'
    ) {
      void lifecycle.mutateAsync({ action: primary.action });
    } else
      setConfirm({
        action: primary.action,
        title: primary.label,
        description: primary.description,
      });
  };
  const submitPrice = async () => {
    if (
      !priceName.trim() ||
      !priceCode.trim() ||
      !Number.isFinite(Number(priceAmount)) ||
      Number(priceAmount) < 0 ||
      currency.trim().length !== 3
    )
      return;
    await price.mutateAsync(undefined);
    setPriceOpen(false);
    setPriceName('');
    setPriceCode('');
    setPriceAmount('');
  };
  const runConfirmed = async () => {
    if (!confirm) return;
    if (confirm.action === 'cancel' && !reason.trim()) return;
    await lifecycle.mutateAsync({ action: confirm.action, reason: reason.trim() });
    setConfirm(null);
    setReason('');
  };

  return (
    <>
      <PageHeader
        title={detail.name}
        description={`${venue.venue.data?.name ?? 'Venue'} · ${formatDateTime(detail.starts_at)}`}
        eyebrow={<EventStatus state={detail.state as EventState} />}
        actions={
          <div className="event-actions">
            {primary ? (
              <Button busy={lifecycle.isPending} onClick={runPrimary}>
                {primary.icon}
                {primary.label}
              </Button>
            ) : null}
            <Select
              aria-label="More event actions"
              value=""
              onChange={(event) => {
                const action = event.target.value as 'close-sales' | 'cancel' | 'complete';
                if (!action) return;
                const copy: readonly [string, string] =
                  action === 'cancel'
                    ? ['Cancel event', 'Cancelling stops all event activity and cannot be undone.']
                    : action === 'close-sales'
                      ? ['Close sales', 'Closing sales ends new ticket acquisition for this event.']
                      : ['Complete event', 'Complete only after event operations have finished.'];
                setConfirm({ action, title: copy[0], description: copy[1] });
              }}
            >
              <option value="">More actions</option>
              {detail.state === 'ON_SALE' || detail.state === 'PAUSED' ? (
                <option value="close-sales">Close sales</option>
              ) : null}
              {detail.state !== 'CANCELLED' && detail.state !== 'COMPLETED' ? (
                <option value="cancel">Cancel event</option>
              ) : null}
              {detail.state === 'SALES_CLOSED' ? (
                <option value="complete">Complete event</option>
              ) : null}
            </Select>
          </div>
        }
      />
      {operationError ? <ErrorState error={operationError} /> : null}
      <div className="tabs" role="tablist">
        {tabs.map(([value, label]) => (
          <button
            key={value}
            type="button"
            role="tab"
            aria-selected={tab === value}
            className={tab === value ? 'active' : ''}
            onClick={() => setTab(value)}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === 'overview' ? (
        <div className="tab-stack">
          <div className="metric-grid four">
            <MetricCard
              label="Tickets sold"
              value={sales?.active_sold_tickets ?? 0}
              hint={`of ${formatNumber(inventoryReport?.total.capacity)} capacity`}
            />
            <MetricCard
              label="Available"
              value={inventoryReport?.total.available ?? 0}
              hint={`${formatNumber(inventoryReport?.total.held)} currently held`}
            />
            <MetricCard
              label="Revenue"
              value={formatMoney(sales?.historical_amount_minor ?? 0, sales?.currency ?? 'NGN')}
              hint="Historical gross ticket revenue"
            />
            <MetricCard
              label="Partner access"
              value={
                configuration?.partner_access.filter((item) => item.access_state === 'ACTIVE')
                  .length ?? 0
              }
              hint="Partners currently enabled"
            />
          </div>
          <div className="detail-grid">
            <Panel>
              <SectionHeading title="Event summary" />
              <div className="panel-divider" />
              <PanelBody>
                <dl className="definition-grid">
                  <div>
                    <dt>Venue</dt>
                    <dd>{venue.venue.data?.name ?? 'Loading venue…'}</dd>
                  </div>
                  <div>
                    <dt>Starts</dt>
                    <dd>{formatDateTime(detail.starts_at)}</dd>
                  </div>
                  <div>
                    <dt>Ends</dt>
                    <dd>{formatDateTime(detail.ends_at)}</dd>
                  </div>
                  <div>
                    <dt>Timezone</dt>
                    <dd>{detail.timezone_name ?? 'Not set'}</dd>
                  </div>
                  <div>
                    <dt>Sales window</dt>
                    <dd>
                      {formatDateTime(detail.sales_open_at)} –{' '}
                      {formatDateTime(detail.sales_close_at)}
                    </dd>
                  </div>
                  <div>
                    <dt>Admission window</dt>
                    <dd>
                      {formatDateTime(detail.admission_open_at)} –{' '}
                      {formatDateTime(detail.admission_close_at)}
                    </dd>
                  </div>
                </dl>
              </PanelBody>
            </Panel>
            <Panel>
              <SectionHeading
                title="Sales progress"
                description="Share of materialized capacity sold"
              />
              <div className="panel-divider" />
              <PanelBody>
                <ProgressBar
                  value={sales?.current_sold_capacity ?? 0}
                  total={inventoryReport?.total.capacity ?? 0}
                  label={`${formatNumber(sales?.current_sold_capacity)} sold`}
                />
              </PanelBody>
            </Panel>
          </div>
          <Panel>
            <SectionHeading
              title="Event setup"
              description="Complete every blocking step before opening sales"
            />
            <div className="panel-divider" />
            <div className="setup-list">
              {Object.entries(setup).map(([key, done]) => (
                <div key={key}>
                  <span className={done ? 'setup-done' : ''}>
                    {done ? <Check size={15} /> : <AlertTriangle size={15} />}
                  </span>
                  <div>
                    <strong>{setupLabel(key)}</strong>
                    <small>{done ? 'Configured' : 'Needs attention'}</small>
                  </div>
                  {key === 'policy' && !done ? (
                    <Button
                      variant="ghost"
                      size="small"
                      busy={policy.isPending}
                      onClick={() => void policy.mutateAsync(undefined)}
                    >
                      Use recommended policy
                    </Button>
                  ) : (
                    <Button
                      variant="ghost"
                      size="small"
                      onClick={() =>
                        setTab(
                          key === 'layout'
                            ? 'layout'
                            : key === 'pricing'
                              ? 'pricing'
                              : key === 'inventory'
                                ? 'inventory'
                                : 'overview',
                        )
                      }
                    >
                      {done ? 'Review' : 'Configure'}
                    </Button>
                  )}
                </div>
              ))}
            </div>
          </Panel>
        </div>
      ) : null}

      {tab === 'layout' ? (
        <Panel>
          <SectionHeading
            title={
              configuration?.layout
                ? `Layout version ${configuration.layout.version_number}`
                : 'Layout & seats'
            }
            description={
              configuration?.layout?.finalized_at
                ? 'A published venue layout is materialized for this event.'
                : 'Materialize a published venue layout to create authoritative inventory.'
            }
          />
          <div className="panel-divider" />
          <PanelBody>
            {configuration?.layout?.finalized_at ? (
              <div className="success-block">
                <ShieldCheck size={22} />
                <div>
                  <strong>Layout materialized</strong>
                  <p>
                    Inventory was created from {configuration.layout.id}. Venue layout changes will
                    not silently alter this event.
                  </p>
                </div>
              </div>
            ) : publishedLayouts.length ? (
              <div className="inline-form">
                <Select
                  aria-label="Published layout"
                  value={layoutId}
                  onChange={(event) => setLayoutId(event.target.value)}
                >
                  <option value="">Choose a published layout</option>
                  {publishedLayouts.map((layout) => (
                    <option key={layout.id} value={layout.id}>
                      Version {layout.version_number}
                    </option>
                  ))}
                </Select>
                <Button
                  disabled={!layoutId}
                  busy={materialize.isPending}
                  onClick={() => void materialize.mutateAsync(undefined)}
                >
                  Materialize layout
                </Button>
              </div>
            ) : (
              <EmptyState
                title="No published layout"
                description="Create or publish a venue layout before assigning it to this event."
                action={
                  <Link
                    className="button button-secondary button-normal"
                    to={`/venues/${detail.venue_id}`}
                  >
                    Open venue
                  </Link>
                }
              />
            )}
          </PanelBody>
        </Panel>
      ) : null}

      {tab === 'pricing' ? (
        <div className="tab-stack">
          <Panel>
            <SectionHeading
              title="Price tiers"
              description="What audiences pay for configured inventory"
              actions={
                <Button size="small" onClick={() => setPriceOpen(true)}>
                  <Plus size={15} />
                  Add price tier
                </Button>
              }
            />
            <div className="panel-divider" />
            {activeTiers.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Tier</th>
                      <th>Code</th>
                      <th>Price</th>
                      <th>Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {configuration?.price_tiers.map((tier) => (
                      <tr key={tier.id}>
                        <td>
                          <strong>{tier.name}</strong>
                        </td>
                        <td className="num">{tier.code}</td>
                        <td className="num">{formatMoney(tier.amount_minor, tier.currency)}</td>
                        <td>
                          <StatusPill
                            label={tier.state === 'ACTIVE' ? 'Active' : 'Retired'}
                            tone={tier.state === 'ACTIVE' ? 'positive' : 'neutral'}
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyState
                icon={<CircleDollarSign size={20} />}
                title="No pricing yet"
                description="Add at least one price tier before opening sales."
                action={<Button onClick={() => setPriceOpen(true)}>Add price tier</Button>}
              />
            )}
          </Panel>
          {activeTiers.length && workspace.inventory.data?.inventory.length ? (
            <Panel>
              <SectionHeading
                title="Assign pricing"
                description="Apply one active tier to all currently materialized inventory."
              />
              <div className="panel-divider" />
              <PanelBody>
                <div className="inline-form">
                  <Select
                    aria-label="Price tier to assign"
                    value={assignTierId}
                    onChange={(event) => setAssignTierId(event.target.value)}
                  >
                    <option value="">Choose a tier</option>
                    {activeTiers.map((tier) => (
                      <option value={tier.id} key={tier.id}>
                        {tier.name} · {formatMoney(tier.amount_minor, tier.currency)}
                      </option>
                    ))}
                  </Select>
                  <Button
                    disabled={!assignTierId}
                    busy={assign.isPending}
                    onClick={() => void assign.mutateAsync(undefined)}
                  >
                    Assign to all inventory
                  </Button>
                </div>
              </PanelBody>
            </Panel>
          ) : null}
        </div>
      ) : null}

      {tab === 'inventory' ? (
        <div className="tab-stack">
          <div className="metric-grid four">
            <MetricCard label="Available" value={inventoryReport?.total.available ?? 0} />
            <MetricCard
              label="Held"
              value={inventoryReport?.total.held ?? 0}
              hint="Active checkout holds"
            />
            <MetricCard label="Sold" value={inventoryReport?.total.sold_current ?? 0} />
            <MetricCard label="Blocked" value={inventoryReport?.total.blocked ?? 0} />
          </div>
          <Panel>
            <SectionHeading
              title="Inventory units"
              description="Authoritative reserved seats and general-admission pools"
            />
            <div className="panel-divider" />
            {workspace.inventory.isLoading ? (
              <LoadingState />
            ) : workspace.inventory.error ? (
              <ErrorState error={workspace.inventory.error} />
            ) : workspace.inventory.data?.inventory.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Label</th>
                      <th>Type</th>
                      <th className="align-right">Quantity</th>
                      <th>Pricing</th>
                    </tr>
                  </thead>
                  <tbody>
                    {workspace.inventory.data.inventory.map((item) => (
                      <tr key={item.id}>
                        <td>
                          <strong>{item.label}</strong>
                        </td>
                        <td>{item.kind === 'GA' ? 'General admission' : 'Reserved seat'}</td>
                        <td className="align-right num">{formatNumber(item.quantity)}</td>
                        <td>
                          {item.price_tier_id ? (
                            <StatusPill label="Assigned" tone="positive" />
                          ) : (
                            <StatusPill label="Unpriced" tone="warning" />
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyState
                title="No materialized inventory"
                description="Materialize a published layout first."
              />
            )}
          </Panel>
        </div>
      ) : null}

      {tab === 'partners' ? (
        <Panel>
          <SectionHeading
            title="Partner selling"
            description="Which ticketing partners can sell this event"
          />
          <div className="panel-divider" />
          {configuration?.partner_access.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Partner</th>
                    <th>Partner status</th>
                    <th>Event access</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {configuration.partner_access.map((item) => {
                    const enabled = item.access_state === 'ACTIVE';
                    return (
                      <tr key={item.partner_id}>
                        <td>
                          <Link className="record-link" to={`/partners/${item.partner_id}`}>
                            <strong>{item.partner_name}</strong>
                          </Link>
                        </td>
                        <td>
                          <StatusPill
                            label={item.partner_state === 'ACTIVE' ? 'Active' : 'Disabled'}
                            tone={item.partner_state === 'ACTIVE' ? 'positive' : 'critical'}
                          />
                        </td>
                        <td>
                          <StatusPill
                            label={enabled ? 'Enabled' : 'No access'}
                            tone={enabled ? 'positive' : 'neutral'}
                          />
                        </td>
                        <td className="align-right">
                          <Button
                            variant="secondary"
                            size="small"
                            disabled={item.partner_state !== 'ACTIVE'}
                            busy={
                              access.isPending && access.variables?.partnerId === item.partner_id
                            }
                            onClick={() =>
                              void access.mutateAsync({
                                partnerId: item.partner_id,
                                enabled: !enabled,
                              })
                            }
                          >
                            {enabled ? 'Disable access' : 'Grant access'}
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              title="No partners yet"
              description="Add a ticketing partner before granting event access."
              action={
                <Link className="button button-primary button-normal" to="/partners">
                  Add partner
                </Link>
              }
            />
          )}
        </Panel>
      ) : null}

      {tab === 'sales' ? (
        <div className="tab-stack">
          <div className="metric-grid four">
            <MetricCard label="Tickets sold" value={sales?.active_sold_tickets ?? 0} />
            <MetricCard
              label="Revenue"
              value={formatMoney(sales?.historical_amount_minor ?? 0, sales?.currency ?? 'NGN')}
            />
            <MetricCard label="Transactions" value={sales?.sale_count ?? 0} />
            <MetricCard label="Voided tickets" value={sales?.voided_sold_tickets ?? 0} />
          </div>
          <Panel>
            <SectionHeading title="Recent tickets" />
            <div className="panel-divider" />
            {tickets.isLoading ? (
              <LoadingState />
            ) : tickets.data?.items.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Ticket</th>
                      <th>Holder</th>
                      <th>Seat / area</th>
                      <th>Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tickets.data.items.slice(0, 10).map((ticket) => (
                      <tr key={ticket.id}>
                        <td className="num">{ticket.id}</td>
                        <td>{ticket.attendee_name ?? 'Not provided'}</td>
                        <td>{ticket.display_label ?? '—'}</td>
                        <td>
                          <StatusPill
                            label={ticket.status === 'ACTIVE' ? 'Active' : 'Voided'}
                            tone={ticket.status === 'ACTIVE' ? 'positive' : 'critical'}
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyState
                title="No tickets issued yet"
                description="Sales activity appears here once tickets are created."
              />
            )}
          </Panel>
        </div>
      ) : null}

      {tab === 'admissions' ? (
        <div className="tab-stack">
          <div className="metric-grid four">
            <MetricCard label="Admitted" value={admissionReport?.active_admissions ?? 0} />
            <MetricCard label="Reversed" value={admissionReport?.reversed_admissions ?? 0} />
            <MetricCard
              label="Duplicate attempts"
              value={admissionReport?.scan_outcomes.ALREADY_ADMITTED ?? 0}
            />
            <MetricCard label="Rejected" value={rejectedTotal(admissionReport?.scan_outcomes)} />
          </div>
          <Panel>
            <SectionHeading title="Recent gate activity" />
            <div className="panel-divider" />
            {admissions.data?.items.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Result</th>
                      <th>Ticket</th>
                      <th>Gate</th>
                      <th>When</th>
                    </tr>
                  </thead>
                  <tbody>
                    {admissions.data.items.slice(0, 20).map((entry) => (
                      <tr key={entry.id}>
                        <td>
                          <StatusPill
                            label={admissionLabel(entry.result)}
                            tone={
                              admissionLabel(entry.result) === 'Admitted'
                                ? 'positive'
                                : admissionLabel(entry.result) === 'Already admitted'
                                  ? 'warning'
                                  : 'critical'
                            }
                          />
                        </td>
                        <td>
                          <strong>
                            {entry.attendee_name ?? entry.ticket_id ?? 'Unknown ticket'}
                          </strong>
                          <small className="table-subline">{entry.display_label}</small>
                        </td>
                        <td>{entry.gate_reference ?? 'Unspecified'}</td>
                        <td>{formatDateTime(entry.occurred_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyState
                title="No entry activity yet"
                description="Scans appear here once doors open."
              />
            )}
          </Panel>
        </div>
      ) : null}

      {tab === 'reports' ? (
        <div className="report-cards">
          <Panel>
            <SectionHeading
              title="Inventory report"
              description={`Generated ${timeAgo(inventoryReport?.generated_at)}`}
            />
            <PanelBody>
              <strong className="report-value">
                {formatNumber(inventoryReport?.total.capacity)}
              </strong>
              <p>
                Total materialized capacity with {formatNumber(inventoryReport?.total.available)}{' '}
                available.
              </p>
            </PanelBody>
          </Panel>
          <Panel>
            <SectionHeading
              title="Sales report"
              description={`Generated ${timeAgo(sales?.generated_at)}`}
            />
            <PanelBody>
              <strong className="report-value">
                {formatMoney(sales?.historical_amount_minor ?? 0, sales?.currency ?? 'NGN')}
              </strong>
              <p>{formatNumber(sales?.historical_sale_quantity)} tickets sold historically.</p>
            </PanelBody>
          </Panel>
          <Panel>
            <SectionHeading
              title="Admission report"
              description={`Generated ${timeAgo(admissionReport?.generated_at)}`}
            />
            <PanelBody>
              <strong className="report-value">
                {formatNumber(admissionReport?.active_admissions)}
              </strong>
              <p>Active admissions from authoritative gate records.</p>
            </PanelBody>
          </Panel>
          <Panel>
            <SectionHeading title="Recent audit" description="Event manager audit trail" />
            <PanelBody>
              {workspace.audit.data?.items.length ? (
                <ul className="compact-list">
                  {workspace.audit.data.items.slice(0, 5).map((item, index) => (
                    <li key={index}>
                      <strong>{friendlyOperation(String(item.operation ?? 'Activity'))}</strong>
                      <small>{timeAgo(String(item.occurred_at ?? ''))}</small>
                    </li>
                  ))}
                </ul>
              ) : (
                <p>No audit activity yet.</p>
              )}
            </PanelBody>
          </Panel>
        </div>
      ) : null}

      <Dialog
        open={priceOpen}
        title="Add price tier"
        description="Create a real event price tier in minor currency units."
        onClose={() => setPriceOpen(false)}
      >
        <div className="dialog-body form-stack">
          <Field label="Name">
            <Input
              id="price-name"
              value={priceName}
              onChange={(event) => setPriceName(event.target.value)}
              placeholder="Standard admission"
            />
          </Field>
          <div className="form-grid two">
            <Field label="Code">
              <Input
                id="price-code"
                value={priceCode}
                onChange={(event) => setPriceCode(event.target.value)}
                placeholder="STD"
              />
            </Field>
            <Field label="Currency">
              <Input
                id="price-currency"
                maxLength={3}
                value={currency}
                onChange={(event) => setCurrency(event.target.value)}
              />
            </Field>
          </div>
          <Field label="Price">
            <Input
              id="price-amount"
              type="number"
              min="0"
              step="0.01"
              value={priceAmount}
              onChange={(event) => setPriceAmount(event.target.value)}
            />
          </Field>
          {price.error ? <ErrorState error={price.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setPriceOpen(false)}>
            Cancel
          </Button>
          <Button
            busy={price.isPending}
            disabled={!priceName.trim() || !priceCode.trim() || priceAmount === ''}
            onClick={() => void submitPrice()}
          >
            Add tier
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={Boolean(confirm)}
        title={confirm?.title ?? ''}
        description={confirm?.description}
        onClose={() => {
          setConfirm(null);
          setReason('');
        }}
      >
        <div className="dialog-body form-stack">
          {confirm?.action === 'cancel' ? (
            <Field label="Reason">
              <Textarea
                id="cancel-reason"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder="Why is this event being cancelled?"
              />
            </Field>
          ) : (
            <InlineNotice tone="warning">
              This changes authoritative event state. The page will refetch before showing the
              result.
            </InlineNotice>
          )}
          {lifecycle.error ? <ErrorState error={lifecycle.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setConfirm(null)}>
            Keep event
          </Button>
          <Button
            variant="danger"
            busy={lifecycle.isPending}
            disabled={confirm?.action === 'cancel' && !reason.trim()}
            onClick={() => void runConfirmed()}
          >
            {confirm?.title}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}

function primaryAction(
  state: EventState,
): { action: LifecycleAction; label: string; description: string; icon: ReactNode } | null {
  if (state === 'DRAFT')
    return {
      action: 'open-sales',
      label: 'Open sales',
      description: 'Open this event for ticket acquisition.',
      icon: <Play size={16} />,
    };
  if (state === 'ON_SALE')
    return {
      action: 'pause-sales',
      label: 'Pause sales',
      description: 'Temporarily stop ticket acquisition.',
      icon: <Pause size={16} />,
    };
  if (state === 'PAUSED')
    return {
      action: 'resume-sales',
      label: 'Resume sales',
      description: 'Resume ticket acquisition.',
      icon: <Play size={16} />,
    };
  if (state === 'SALES_CLOSED')
    return {
      action: 'complete',
      label: 'Complete event',
      description: 'Mark event operations complete.',
      icon: <Check size={16} />,
    };
  return null;
}

function setupLabel(key: string) {
  return (
    (
      {
        policy: 'Sales policy',
        layout: 'Layout & seats',
        pricing: 'Pricing',
        inventory: 'Inventory',
      } as Record<string, string>
    )[key] ?? key
  );
}

function rejectedTotal(outcomes?: Record<string, number>) {
  if (!outcomes) return 0;
  return Object.entries(outcomes)
    .filter(([key]) => !['ADMITTED', 'MANUAL_OVERRIDE_ADMITTED', 'ALREADY_ADMITTED'].includes(key))
    .reduce((sum, [, count]) => sum + count, 0);
}
