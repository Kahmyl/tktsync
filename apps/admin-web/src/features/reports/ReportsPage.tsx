import { BarChart3 } from 'lucide-react';
import { useEffect, useState } from 'react';
import {
  EmptyState,
  ErrorState,
  LoadingState,
  MetricCard,
  PageHeader,
  Panel,
  PanelBody,
  SectionHeading,
  Select,
} from '../../components/ui';
import { formatMoney, formatNumber, timeAgo } from '../../lib/format';
import { useEventReports, useEvents } from '../admin/queries';

export function ReportsPage() {
  const events = useEvents();
  const [eventId, setEventId] = useState('');
  useEffect(() => {
    if (!eventId && events.data?.items.length) setEventId(events.data.items[0]!.id);
  }, [eventId, events.data]);
  const reports = useEventReports(eventId);
  const error = reports.inventory.error ?? reports.sales.error ?? reports.admissions.error;
  const loading =
    Boolean(eventId) &&
    (reports.inventory.isLoading || reports.sales.isLoading || reports.admissions.isLoading);
  const inventory = reports.inventory.data;
  const sales = reports.sales.data;
  const admissions = reports.admissions.data;
  return (
    <>
      <PageHeader
        title="Reports"
        description="Understand sales, inventory and admissions from authoritative reporting snapshots."
        actions={
          <Select
            aria-label="Report event"
            value={eventId}
            onChange={(event) => setEventId(event.target.value)}
          >
            <option value="">Select event</option>
            {events.data?.items.map((event) => (
              <option key={event.id} value={event.id}>
                {event.name}
              </option>
            ))}
          </Select>
        }
      />
      {events.isLoading || loading ? (
        <LoadingState rows={8} />
      ) : error ? (
        <ErrorState
          error={error}
          onRetry={() => {
            void reports.inventory.refetch();
            void reports.sales.refetch();
            void reports.admissions.refetch();
          }}
        />
      ) : !eventId ? (
        <Panel>
          <EmptyState
            icon={<BarChart3 size={20} />}
            title="No events to report"
            description="Create an event before viewing operational reports."
          />
        </Panel>
      ) : (
        <div className="tab-stack">
          <div className="metric-grid four">
            <MetricCard
              label="Tickets sold"
              value={sales?.active_sold_tickets ?? 0}
              hint={`${formatNumber(sales?.voided_sold_tickets)} voided`}
            />
            <MetricCard
              label="Revenue"
              value={formatMoney(sales?.historical_amount_minor ?? 0, sales?.currency ?? 'NGN')}
            />
            <MetricCard
              label="Available"
              value={inventory?.total.available ?? 0}
              hint={`of ${formatNumber(inventory?.total.capacity)} capacity`}
            />
            <MetricCard
              label="Admitted"
              value={admissions?.active_admissions ?? 0}
              hint={`${formatNumber(admissions?.reversed_admissions)} reversed`}
            />
          </div>
          <Panel>
            <SectionHeading
              title="Inventory position"
              description={`Snapshot generated ${timeAgo(inventory?.generated_at)}`}
            />
            <div className="panel-divider" />
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Inventory</th>
                    <th className="align-right">Capacity</th>
                    <th className="align-right">Available</th>
                    <th className="align-right">Held</th>
                    <th className="align-right">Sold</th>
                    <th className="align-right">Allocated</th>
                    <th className="align-right">Blocked</th>
                  </tr>
                </thead>
                <tbody>
                  {inventory
                    ? [
                        ['Reserved seating', inventory.reserved_seating],
                        ['General admission', inventory.general_admission],
                        ['Total', inventory.total],
                      ].map(([label, raw]) => {
                        const row = raw as typeof inventory.total;
                        return (
                          <tr key={String(label)}>
                            <td>
                              <strong>{String(label)}</strong>
                            </td>
                            <td className="align-right num">{formatNumber(row.capacity)}</td>
                            <td className="align-right num">{formatNumber(row.available)}</td>
                            <td className="align-right num">{formatNumber(row.held)}</td>
                            <td className="align-right num">{formatNumber(row.sold_current)}</td>
                            <td className="align-right num">{formatNumber(row.allocated)}</td>
                            <td className="align-right num">{formatNumber(row.blocked)}</td>
                          </tr>
                        );
                      })
                    : null}
                </tbody>
              </table>
            </div>
          </Panel>
          <div className="report-cards">
            <Panel>
              <SectionHeading
                title="Commercial performance"
                description={`Snapshot generated ${timeAgo(sales?.generated_at)}`}
              />
              <PanelBody>
                <dl className="definition-grid">
                  <div>
                    <dt>Sales</dt>
                    <dd>{formatNumber(sales?.sale_count)}</dd>
                  </div>
                  <div>
                    <dt>Historical quantity</dt>
                    <dd>{formatNumber(sales?.historical_sale_quantity)}</dd>
                  </div>
                  <div>
                    <dt>Active sold capacity</dt>
                    <dd>{formatNumber(sales?.current_sold_capacity)}</dd>
                  </div>
                  <div>
                    <dt>Gross amount</dt>
                    <dd>
                      {formatMoney(sales?.historical_amount_minor ?? 0, sales?.currency ?? 'NGN')}
                    </dd>
                  </div>
                </dl>
              </PanelBody>
            </Panel>
            <Panel>
              <SectionHeading
                title="Admission outcomes"
                description={`Snapshot generated ${timeAgo(admissions?.generated_at)}`}
              />
              <PanelBody>
                {Object.keys(admissions?.scan_outcomes ?? {}).length ? (
                  <ul className="compact-list">
                    {Object.entries(admissions?.scan_outcomes ?? {}).map(([result, count]) => (
                      <li key={result}>
                        <strong>{result.replaceAll('_', ' ').toLowerCase()}</strong>
                        <span className="num">{formatNumber(count)}</span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p>No admission outcomes yet.</p>
                )}
              </PanelBody>
            </Panel>
          </div>
        </div>
      )}
    </>
  );
}
