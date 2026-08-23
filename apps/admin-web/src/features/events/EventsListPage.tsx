import { CalendarDays, Plus, Search } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  EmptyState,
  ErrorState,
  EventStatus,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  ProgressBar,
  Select,
} from '../../components/ui';
import { formatDateTime, formatNumber } from '../../lib/format';
import { useEvents } from '../admin/queries';
import type { EventState } from '../admin/types';

export function EventsListPage() {
  const events = useEvents();
  const [query, setQuery] = useState('');
  const [state, setState] = useState('');
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (events.data?.items ?? []).filter(
      (event) =>
        (!needle || `${event.name} ${event.venue_name}`.toLowerCase().includes(needle)) &&
        (!state || event.state === state),
    );
  }, [events.data, query, state]);

  return (
    <>
      <PageHeader
        title="Events"
        description="Create and manage ticketed events."
        actions={
          <Link className="button button-primary button-normal" to="/events/new">
            <Plus size={16} />
            Create event
          </Link>
        }
      />
      <div className="filters">
        <label className="search-control">
          <Search size={16} />
          <Input
            aria-label="Search events"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search events or venues"
          />
        </label>
        <Select
          aria-label="Filter event status"
          value={state}
          onChange={(event) => setState(event.target.value)}
        >
          <option value="">All statuses</option>
          <option value="DRAFT">Draft</option>
          <option value="ON_SALE">On sale</option>
          <option value="PAUSED">Paused</option>
          <option value="SALES_CLOSED">Sales closed</option>
          <option value="COMPLETED">Completed</option>
          <option value="CANCELLED">Cancelled</option>
        </Select>
      </div>
      <Panel>
        {events.isLoading ? (
          <LoadingState />
        ) : events.error ? (
          <ErrorState error={events.error} onRetry={() => void events.refetch()} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={<CalendarDays size={20} />}
            title={events.data?.items.length ? 'No events match your filters' : 'No events yet'}
            description={
              events.data?.items.length
                ? 'Try a different search term or clear the status filter.'
                : 'Create your first event to get started.'
            }
            action={
              !events.data?.items.length ? (
                <Link className="button button-primary button-normal" to="/events/new">
                  Create event
                </Link>
              ) : undefined
            }
          />
        ) : (
          <>
            <div className="table-wrap desktop-table">
              <table>
                <thead>
                  <tr>
                    <th>Event</th>
                    <th>When</th>
                    <th>Status</th>
                    <th>Sales</th>
                    <th className="align-right">Sold</th>
                  </tr>
                </thead>
                <tbody>
                  {filtered.map((event) => (
                    <tr key={event.id}>
                      <td>
                        <Link className="record-link" to={`/events/${event.id}`}>
                          <strong>{event.name}</strong>
                          <small>{event.venue_name}</small>
                        </Link>
                      </td>
                      <td>{formatDateTime(event.starts_at)}</td>
                      <td>
                        <EventStatus state={event.state as EventState} />
                      </td>
                      <td>
                        <ProgressBar value={event.sold} total={event.capacity} />
                      </td>
                      <td className="align-right num">
                        {formatNumber(event.sold)} / {formatNumber(event.capacity)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mobile-cards">
              {filtered.map((event) => (
                <Link className="mobile-record" to={`/events/${event.id}`} key={event.id}>
                  <div>
                    <strong>{event.name}</strong>
                    <EventStatus state={event.state as EventState} />
                  </div>
                  <p>{event.venue_name}</p>
                  <small>{formatDateTime(event.starts_at)}</small>
                  <ProgressBar
                    value={event.sold}
                    total={event.capacity}
                    label={`${formatNumber(event.sold)} / ${formatNumber(event.capacity)} sold`}
                  />
                </Link>
              ))}
            </div>
          </>
        )}
      </Panel>
    </>
  );
}
