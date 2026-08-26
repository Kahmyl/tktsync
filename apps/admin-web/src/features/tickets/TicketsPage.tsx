import { RotateCcw, Search, ShieldAlert, Ticket as TicketIcon } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  Select,
  StatusPill,
  Textarea,
} from '../../components/ui';
import { formatDateTime, humanDomainLabel, humanName } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useEvents, useIntentMutation, useTickets } from '../admin/queries';
import type { TicketSummary } from '../admin/types';

type TicketAction = 'void' | 'reissue' | 'rerelease';

export function TicketsPage() {
  const [query, setQuery] = useState('');
  const [eventId, setEventId] = useState('');
  const [stateFilter, setStateFilter] = useState('');
  const [selected, setSelected] = useState<TicketSummary | null>(null);
  const [action, setAction] = useState<TicketAction | null>(null);
  const [reason, setReason] = useState('');
  const tickets = useTickets(query, eventId, stateFilter);
  const events = useEvents();
  const invalidate = [adminKeys.tickets(query, eventId, stateFilter), adminKeys.dashboard];
  const mutate = useIntentMutation({
    intent: (variables: { ticketId: string; action: TicketAction; reason: string }) =>
      `${variables.ticketId}:${variables.action}:${variables.reason}`,
    mutationFn: (token, key, variables) =>
      variables.action === 'void'
        ? adminApi.voidTicket(token, key, variables.ticketId, variables.reason)
        : variables.action === 'rerelease'
          ? adminApi.rereleaseTicket(token, key, variables.ticketId, variables.reason)
          : adminApi.reissueTicket(token, key, variables.ticketId),
    invalidate,
  });
  const runAction = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const submittedReason = String(form.get('reason') || reason).trim();
    if (!selected || !action || ((action === 'void' || action === 'rerelease') && !submittedReason))
      return;
    await mutate.mutateAsync({ ticketId: selected.id, action, reason: submittedReason });
    setAction(null);
    setReason('');
    setSelected(null);
  };
  return (
    <>
      <PageHeader
        title="Tickets"
        description="Search authoritative ticket records and perform controlled recovery actions."
      />
      <div className="filters three">
        <label className="search-control">
          <Search size={16} />
          <Input
            aria-label="Search tickets"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Holder, event, seat or ticket reference"
          />
        </label>
        <Select
          aria-label="Filter tickets by event"
          value={eventId}
          onChange={(event) => setEventId(event.target.value)}
        >
          <option value="">All events</option>
          {events.data?.items.map((event) => (
            <option value={event.id} key={event.id}>
              {humanName(event.name, 'Untitled event')}
            </option>
          ))}
        </Select>
        <Select
          aria-label="Filter ticket status"
          value={stateFilter}
          onChange={(event) => setStateFilter(event.target.value)}
        >
          <option value="">All statuses</option>
          <option value="ACTIVE">Active</option>
          <option value="VOIDED">Voided</option>
        </Select>
      </div>
      <Panel>
        {tickets.isLoading ? (
          <LoadingState />
        ) : tickets.error ? (
          <ErrorState error={tickets.error} onRetry={() => void tickets.refetch()} />
        ) : tickets.data?.items.length ? (
          <>
            <div className="table-wrap desktop-table">
              <table>
                <thead>
                  <tr>
                    <th>Ticket</th>
                    <th>Event</th>
                    <th>Holder</th>
                    <th>Seat / area</th>
                    <th>Status</th>
                    <th>Entry</th>
                  </tr>
                </thead>
                <tbody>
                  {tickets.data.items.map((ticket) => (
                    <tr
                      key={ticket.id}
                      onClick={() => setSelected(ticket)}
                      className="clickable-row"
                    >
                      <td>
                        <strong>Admission ticket</strong>
                        <small className="table-subline">{formatDateTime(ticket.created_at)}</small>
                      </td>
                      <td>{humanName(ticket.event_name, 'Untitled event')}</td>
                      <td>{ticket.attendee_name ?? 'Not provided'}</td>
                      <td>{ticket.display_label ?? '—'}</td>
                      <td>
                        <StatusPill
                          label={ticket.status === 'ACTIVE' ? 'Active' : 'Voided'}
                          tone={ticket.status === 'ACTIVE' ? 'positive' : 'critical'}
                        />
                      </td>
                      <td>
                        <StatusPill
                          label={ticket.admission_state === 'ACTIVE' ? 'Admitted' : 'Not admitted'}
                          tone={ticket.admission_state === 'ACTIVE' ? 'info' : 'neutral'}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mobile-cards">
              {tickets.data.items.map((ticket) => (
                <button
                  type="button"
                  className="mobile-record"
                  key={ticket.id}
                  onClick={() => setSelected(ticket)}
                >
                  <div>
                    <strong>Admission ticket</strong>
                    <StatusPill
                      label={ticket.status === 'ACTIVE' ? 'Active' : 'Voided'}
                      tone={ticket.status === 'ACTIVE' ? 'positive' : 'critical'}
                    />
                  </div>
                  <p>{humanName(ticket.event_name, 'Untitled event')}</p>
                  <small>
                    {ticket.attendee_name ?? ticket.display_label ?? 'No attendee details'}
                  </small>
                </button>
              ))}
            </div>
          </>
        ) : (
          <EmptyState
            icon={<TicketIcon size={20} />}
            title="No tickets found"
            description={
              query || eventId || stateFilter
                ? 'Try different search or filter values.'
                : 'Tickets will appear after real sales or non-public issuance.'
            }
          />
        )}
      </Panel>
      <Dialog
        open={Boolean(selected) && !action}
        title="Ticket detail"
        description={selected ? `Issued ${formatDateTime(selected.created_at)}` : undefined}
        onClose={() => setSelected(null)}
        className="wide-dialog"
      >
        <div className="dialog-body">
          {selected ? (
            <>
              <dl className="definition-grid">
                <div>
                  <dt>Event</dt>
                  <dd>{humanName(selected.event_name, 'Untitled event')}</dd>
                </div>
                <div>
                  <dt>Status</dt>
                  <dd>{selected.status === 'ACTIVE' ? 'Active' : 'Voided'}</dd>
                </div>
                <div>
                  <dt>Holder</dt>
                  <dd>{selected.attendee_name ?? 'Not provided'}</dd>
                </div>
                <div>
                  <dt>Seat / area</dt>
                  <dd>{selected.display_label ?? 'Not provided'}</dd>
                </div>
                <div>
                  <dt>Credential</dt>
                  <dd>{humanDomainLabel(selected.credential_state, 'None')}</dd>
                </div>
                <div>
                  <dt>Admission</dt>
                  <dd>
                    {selected.admission_state === 'ACTIVE'
                      ? 'Admitted'
                      : selected.admission_state === 'REVERSED'
                        ? 'Admission reversed'
                        : 'Not admitted'}
                  </dd>
                </div>
              </dl>
              <div className="dialog-button-row">
                <Button
                  variant="secondary"
                  disabled={selected.status !== 'ACTIVE'}
                  onClick={() => setAction('reissue')}
                >
                  <RotateCcw size={15} />
                  Reissue credential
                </Button>
                <Button
                  variant="secondary"
                  disabled={selected.status !== 'VOIDED'}
                  onClick={() => setAction('rerelease')}
                >
                  Re-release inventory
                </Button>
                <Button
                  variant="danger"
                  disabled={selected.status !== 'ACTIVE'}
                  onClick={() => setAction('void')}
                >
                  Void ticket
                </Button>
              </div>
            </>
          ) : null}
        </div>
      </Dialog>
      <Dialog
        open={Boolean(action)}
        title={
          action === 'void'
            ? 'Void ticket'
            : action === 'rerelease'
              ? 'Re-release ticket inventory'
              : 'Reissue ticket credential'
        }
        description={
          action === 'void'
            ? 'Voiding revokes the active credential; inventory remains consumed until deliberately re-released.'
            : action === 'rerelease'
              ? 'This returns a voided ticket’s capacity to shared inventory.'
              : 'The current QR credential will be superseded without exposing QR material here.'
        }
        onClose={() => {
          setAction(null);
          setReason('');
        }}
      >
        <form onSubmit={(event) => void runAction(event)}>
          <div className="dialog-body form-stack">
            <div className="danger-block">
              <ShieldAlert size={20} />
              <p>Confirm the selected ticket and authoritative state before continuing.</p>
            </div>
            {action !== 'reissue' ? (
              <Field label="Reason">
                <Textarea
                  id="ticket-action-reason"
                  name="reason"
                  required
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                />
              </Field>
            ) : null}
            {mutate.error ? <ErrorState error={mutate.error} /> : null}
          </div>
          <DialogActions>
            <Button type="button" variant="secondary" onClick={() => setAction(null)}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant={action === 'reissue' ? 'primary' : 'danger'}
              busy={mutate.isPending}
            >
              {action === 'void'
                ? 'Void ticket'
                : action === 'rerelease'
                  ? 'Re-release inventory'
                  : 'Reissue credential'}
            </Button>
          </DialogActions>
        </form>
      </Dialog>
    </>
  );
}
