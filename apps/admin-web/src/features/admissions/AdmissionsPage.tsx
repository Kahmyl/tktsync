import { RotateCcw, ScanLine, ShieldCheck } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  Field,
  Input,
  LoadingState,
  MetricCard,
  PageHeader,
  Panel,
  Select,
  StatusPill,
  Textarea,
} from '../../components/ui';
import { admissionLabel, formatDateTime } from '../../lib/format';
import { adminApi } from '../admin/api';
import {
  adminKeys,
  useAdmissionReport,
  useAdmissions,
  useEvents,
  useIntentMutation,
} from '../admin/queries';
import type { AdmissionEntry } from '../admin/types';

export function AdmissionsPage() {
  const events = useEvents();
  const [eventId, setEventId] = useState('');
  const entries = useAdmissions(eventId);
  const report = useAdmissionReport(eventId);
  const [manualOpen, setManualOpen] = useState(false);
  const [ticketId, setTicketId] = useState('');
  const [gate, setGate] = useState('');
  const [reason, setReason] = useState('');
  const [reverse, setReverse] = useState<AdmissionEntry | null>(null);
  useEffect(() => {
    if (!eventId && events.data?.items.length)
      setEventId(
        events.data.items.find((event) => !['DRAFT', 'CANCELLED'].includes(event.state))?.id ??
          events.data.items[0]!.id,
      );
  }, [eventId, events.data]);
  const invalidate = [
    adminKeys.admissions(eventId),
    adminKeys.admissionReport(eventId),
    adminKeys.dashboard,
  ];
  const manual = useIntentMutation({
    intent: () => `${eventId}:manual:${ticketId}:${reason}`,
    mutationFn: (token, key) =>
      adminApi.manualAdmission(token, key, {
        event_id: eventId,
        ticket_id: ticketId.trim(),
        gate_reference: gate.trim() || undefined,
        reason: reason.trim(),
      }),
    invalidate,
  });
  const reversal = useIntentMutation({
    intent: () => `${reverse?.admission_id}:reverse:${reason}`,
    mutationFn: (token, key) =>
      adminApi.reverseAdmission(token, key, reverse?.admission_id ?? '', reason.trim()),
    invalidate,
  });
  const counts = useMemo(() => {
    const items = entries.data?.items ?? [];
    return {
      admitted: items.filter((item) => admissionLabel(item.result) === 'Admitted').length,
      duplicate: items.filter((item) => admissionLabel(item.result) === 'Already admitted').length,
      rejected: items.filter((item) =>
        ['Invalid', 'Rejected'].includes(admissionLabel(item.result)),
      ).length,
    };
  }, [entries.data]);
  const submitManual = async () => {
    if (!eventId || !ticketId.trim() || !reason.trim()) return;
    await manual.mutateAsync(undefined);
    setManualOpen(false);
    setTicketId('');
    setGate('');
    setReason('');
  };
  const submitReverse = async () => {
    if (!reverse?.admission_id || !reason.trim()) return;
    await reversal.mutateAsync(undefined);
    setReverse(null);
    setReason('');
  };

  return (
    <>
      <PageHeader
        title="Admissions"
        description="Admin oversight of real gate activity. Scanner remains a separate fail-closed application."
        actions={
          <>
            <Select
              aria-label="Admission event"
              value={eventId}
              onChange={(event) => setEventId(event.target.value)}
            >
              <option value="">Select event</option>
              {events.data?.items.map((event) => (
                <option value={event.id} key={event.id}>
                  {event.name}
                </option>
              ))}
            </Select>
            <Button onClick={() => setManualOpen(true)} disabled={!eventId}>
              <ShieldCheck size={16} />
              Manual override
            </Button>
          </>
        }
      />
      <div className="metric-grid four">
        <MetricCard
          label="Admitted"
          value={report.data?.active_admissions ?? counts.admitted}
          hint="Active authoritative admissions"
        />
        <MetricCard label="Reversed" value={report.data?.reversed_admissions ?? 0} />
        <MetricCard
          label="Already admitted"
          value={report.data?.scan_outcomes.ALREADY_ADMITTED ?? counts.duplicate}
          hint="Duplicate scan attempts"
        />
        <MetricCard label="Rejected" value={counts.rejected} hint="Invalid or refused entry" />
      </div>
      <Panel>
        {entries.isLoading || events.isLoading ? (
          <LoadingState />
        ) : entries.error ? (
          <ErrorState error={entries.error} onRetry={() => void entries.refetch()} />
        ) : !eventId ? (
          <EmptyState
            icon={<ScanLine size={20} />}
            title="No events to monitor"
            description="Once an event exists, gate activity can be reviewed here."
          />
        ) : entries.data?.items.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Result</th>
                  <th>Ticket / holder</th>
                  <th>Seat / area</th>
                  <th>Gate</th>
                  <th>When</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {entries.data.items.map((entry) => {
                  const label = admissionLabel(entry.result);
                  return (
                    <tr key={entry.id}>
                      <td>
                        <StatusPill
                          label={label}
                          tone={
                            label === 'Admitted'
                              ? 'positive'
                              : label === 'Already admitted'
                                ? 'warning'
                                : 'critical'
                          }
                        />
                      </td>
                      <td>
                        <strong>
                          {entry.attendee_name ?? entry.ticket_id ?? 'Unknown ticket'}
                        </strong>
                      </td>
                      <td>{entry.display_label ?? '—'}</td>
                      <td>{entry.gate_reference ?? 'Unspecified'}</td>
                      <td>{formatDateTime(entry.occurred_at)}</td>
                      <td className="align-right">
                        {entry.admission_id && entry.admission_state === 'ACTIVE' ? (
                          <Button variant="ghost" size="small" onClick={() => setReverse(entry)}>
                            <RotateCcw size={14} />
                            Reverse
                          </Button>
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            icon={<ScanLine size={20} />}
            title="No scans yet"
            description="Activity appears the moment gate scanning begins."
          />
        )}
      </Panel>
      <Dialog
        open={manualOpen}
        title="Manual admission override"
        description="This privileged path requires a real ticket, event and audit reason."
        onClose={() => {
          setManualOpen(false);
          setReason('');
        }}
      >
        <div className="dialog-body form-stack">
          <Field label="Ticket ID">
            <Input
              id="manual-ticket"
              value={ticketId}
              onChange={(event) => setTicketId(event.target.value)}
              placeholder="tkt_…"
            />
          </Field>
          <Field label="Gate reference">
            <Input
              id="manual-gate"
              value={gate}
              onChange={(event) => setGate(event.target.value)}
            />
          </Field>
          <Field label="Reason">
            <Textarea
              id="manual-reason"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </Field>
          {manual.error ? <ErrorState error={manual.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setManualOpen(false)}>
            Cancel
          </Button>
          <Button
            busy={manual.isPending}
            disabled={!ticketId.trim() || !reason.trim()}
            onClick={() => void submitManual()}
          >
            Admit with override
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={Boolean(reverse)}
        title="Reverse admission"
        description="This marks the current admission reversed and requires a reason."
        onClose={() => {
          setReverse(null);
          setReason('');
        }}
      >
        <div className="dialog-body form-stack">
          <Field label="Reason">
            <Textarea
              id="reverse-reason"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </Field>
          {reversal.error ? <ErrorState error={reversal.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setReverse(null)}>
            Keep admission
          </Button>
          <Button
            variant="danger"
            busy={reversal.isPending}
            disabled={!reason.trim()}
            onClick={() => void submitReverse()}
          >
            Reverse admission
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
