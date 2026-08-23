import {
  AlertTriangle,
  ArrowRight,
  CalendarDays,
  Plus,
  Ticket,
  TicketCheck,
  Users,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import { useOperator } from '../../auth/OperatorSession';
import {
  EmptyState,
  ErrorState,
  EventStatus,
  LoadingState,
  MetricCard,
  PageHeader,
  Panel,
  ProgressBar,
  SectionHeading,
} from '../../components/ui';
import { formatDateTime, formatNumber, friendlyOperation, timeAgo } from '../../lib/format';
import { useDashboard } from '../admin/queries';

function greeting() {
  const hour = new Date().getHours();
  if (hour < 12) return 'Good morning';
  if (hour < 18) return 'Good afternoon';
  return 'Good evening';
}

export function DashboardPage() {
  const { user } = useOperator();
  const dashboard = useDashboard();
  const firstName = user?.displayName.split(/\s+/)[0] ?? 'there';

  if (dashboard.isLoading) return <LoadingState rows={8} />;
  if (dashboard.error || !dashboard.data)
    return <ErrorState error={dashboard.error} onRetry={() => void dashboard.refetch()} />;
  const data = dashboard.data;

  return (
    <>
      <PageHeader
        title={`${greeting()}, ${firstName}`}
        description="Here's what's happening across your events."
        actions={
          <Link className="button button-primary button-normal" to="/events/new">
            <Plus size={16} />
            Create event
          </Link>
        }
      />
      <div className="metric-grid four">
        <MetricCard
          label="Active events"
          value={data.metrics.active_events}
          hint="On sale or paused"
          icon={<CalendarDays size={16} />}
        />
        <MetricCard
          label="Tickets sold"
          value={data.metrics.tickets_sold}
          hint="Active tickets across all events"
          icon={<Ticket size={16} />}
        />
        <MetricCard
          label="Reservations today"
          value={data.metrics.reservations_today}
          hint="Authoritative reservations created today"
          icon={<Users size={16} />}
        />
        <MetricCard
          label="Check-ins today"
          value={data.metrics.checkins_today}
          hint="Admission records created today"
          icon={<TicketCheck size={16} />}
        />
      </div>

      <div className="dashboard-grid">
        <Panel>
          <SectionHeading
            title="Upcoming events"
            description="Next events by start date"
            actions={
              <Link className="text-link" to="/events">
                View all <ArrowRight size={14} />
              </Link>
            }
          />
          <div className="panel-divider" />
          {data.upcoming_events.length ? (
            <ul className="record-list">
              {data.upcoming_events.map((event) => (
                <li key={event.id}>
                  <Link to={`/events/${event.id}`} className="upcoming-row">
                    <div>
                      <strong>{event.name}</strong>
                      <small>
                        {event.venue_name} · {formatDateTime(event.starts_at)}
                      </small>
                    </div>
                    <div className="upcoming-progress">
                      <ProgressBar
                        value={event.sold}
                        total={event.capacity}
                        label={`${formatNumber(event.sold)} / ${formatNumber(event.capacity)} sold`}
                      />
                    </div>
                    <EventStatus state={event.state} />
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState
              title="No upcoming events"
              description="Create a draft event to start planning."
              action={
                <Link className="button button-primary button-normal" to="/events/new">
                  Create event
                </Link>
              }
            />
          )}
        </Panel>

        <div className="side-stack">
          <Panel>
            <SectionHeading
              title="Attention needed"
              description="Real setup gaps worth resolving"
            />
            <div className="panel-divider" />
            {data.attention.length ? (
              <ul className="attention-list">
                {data.attention.map((item) => {
                  const missing = [
                    !item.layout_ready && 'layout',
                    !item.pricing_ready && 'pricing',
                    !item.policy_ready && 'sales policy',
                  ]
                    .filter(Boolean)
                    .join(', ');
                  return (
                    <li key={item.event_id}>
                      <AlertTriangle size={16} />
                      <div>
                        <strong>{item.event_name} needs setup</strong>
                        <p>Missing {missing}.</p>
                        <Link to={`/events/${item.event_id}`}>Review event</Link>
                      </div>
                    </li>
                  );
                })}
              </ul>
            ) : (
              <EmptyState
                title="Nothing needs attention"
                description="Draft event setup is complete."
              />
            )}
          </Panel>
          <Panel>
            <SectionHeading title="Recent activity" />
            <div className="panel-divider" />
            {data.recent_activity.length ? (
              <ul className="activity-list">
                {data.recent_activity.map((item, index) => (
                  <li key={`${item.operation}-${item.occurred_at}-${index}`}>
                    <span />
                    <div>
                      <strong>{friendlyOperation(item.operation)}</strong>
                      <small>
                        {item.event_name ?? item.partner_name ?? item.entity_type} ·{' '}
                        {timeAgo(item.occurred_at)}
                      </small>
                    </div>
                  </li>
                ))}
              </ul>
            ) : (
              <EmptyState
                title="No recent activity"
                description="Authoritative audit activity will appear here."
              />
            )}
          </Panel>
        </div>
      </div>
    </>
  );
}
