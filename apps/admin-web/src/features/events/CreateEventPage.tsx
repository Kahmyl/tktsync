import { ArrowLeft, ArrowRight, CalendarPlus, Check } from 'lucide-react';
import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  Button,
  ErrorState,
  Field,
  Input,
  InlineNotice,
  LoadingState,
  PageHeader,
  Panel,
  PanelBody,
  Select,
} from '../../components/ui';
import { humanName, optionalISO } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useIntentMutation, useVenues } from '../admin/queries';

export function CreateEventPage() {
  const navigate = useNavigate();
  const venues = useVenues();
  const [step, setStep] = useState(1);
  const [name, setName] = useState('');
  const [venueId, setVenueId] = useState('');
  const [timeZone, setTimeZone] = useState(
    Intl.DateTimeFormat().resolvedOptions().timeZone || 'Africa/Lagos',
  );
  const [startsAt, setStartsAt] = useState('');
  const [endsAt, setEndsAt] = useState('');
  const [salesOpenAt, setSalesOpenAt] = useState('');
  const [salesCloseAt, setSalesCloseAt] = useState('');
  const [admissionOpenAt, setAdmissionOpenAt] = useState('');
  const [admissionCloseAt, setAdmissionCloseAt] = useState('');
  const [error, setError] = useState('');
  const scheduleRows = [
    ['Starts', startsAt],
    ['Ends', endsAt],
    ['Sales open', salesOpenAt],
    ['Sales close', salesCloseAt],
    ['Admission open', admissionOpenAt],
    ['Admission close', admissionCloseAt],
  ] as const;
  const mutation = useIntentMutation({
    intent: () => `create-event:${name.trim()}:${venueId}:${startsAt}`,
    mutationFn: (token, key) =>
      adminApi.createEvent(token, key, {
        venue_id: venueId,
        name: name.trim(),
        starts_at: optionalISO(startsAt),
        ends_at: optionalISO(endsAt),
        sales_open_at: optionalISO(salesOpenAt),
        sales_close_at: optionalISO(salesCloseAt),
        admission_open_at: optionalISO(admissionOpenAt),
        admission_close_at: optionalISO(admissionCloseAt),
        timezone_name: timeZone.trim() || undefined,
      }),
    invalidate: [adminKeys.dashboard, adminKeys.events()],
  });

  const next = () => {
    if (step === 1 && (!name.trim() || !venueId)) {
      setError('Add an event name and choose a venue to continue.');
      return;
    }
    if (step === 2) {
      const invalidPair = [
        [startsAt, endsAt, 'Event end time must be after the event starts.'],
        [salesOpenAt, salesCloseAt, 'Sales close time must be after sales open.'],
        [admissionOpenAt, admissionCloseAt, 'Admission close time must be after admission opens.'],
      ].find(([open, close]) => open && close && new Date(close) < new Date(open));
      if (invalidPair) {
        setError(invalidPair[2]!);
        return;
      }
    }
    setError('');
    setStep((value) => Math.min(3, value + 1));
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    try {
      const created = await mutation.mutateAsync(undefined);
      navigate(`/events/${created.id}`);
    } catch {
      // The polished mutation error remains rendered in the review step.
    }
  };

  if (venues.isLoading) return <LoadingState />;
  if (venues.error)
    return <ErrorState error={venues.error} onRetry={() => void venues.refetch()} />;

  return (
    <form onSubmit={(event) => void submit(event)}>
      <PageHeader
        title="Create event"
        description="Start with the essentials. Add pricing, inventory and partner access before opening sales."
        actions={
          <Link className="button button-secondary button-normal" to="/events">
            <ArrowLeft size={16} />
            Back to events
          </Link>
        }
      />
      <div className="wizard-steps" aria-label="Create event progress">
        <span className={step >= 1 ? 'active' : ''}>
          <i>{step > 1 ? <Check size={14} /> : 1}</i>Basics
        </span>
        <b />
        <span className={step >= 2 ? 'active' : ''}>
          <i>{step > 2 ? <Check size={14} /> : 2}</i>Schedule
        </span>
        <b />
        <span className={step >= 3 ? 'active' : ''}>
          <i>3</i>Review
        </span>
      </div>
      <div className="wizard-grid">
        <Panel>
          <PanelBody className="wizard-body">
            {step === 1 ? (
              <>
                <div className="wizard-heading">
                  <CalendarPlus size={20} />
                  <div>
                    <h2>Event basics</h2>
                    <p>Name the event and choose where it happens.</p>
                  </div>
                </div>
                <div className="form-grid">
                  <Field label="Event name">
                    <Input
                      id="event-name"
                      value={name}
                      onChange={(event) => setName(event.target.value)}
                      placeholder="e.g. Championship Night"
                      autoFocus
                    />
                  </Field>
                  <Field label="Venue">
                    <Select
                      id="event-venue"
                      value={venueId}
                      onChange={(event) => setVenueId(event.target.value)}
                    >
                      <option value="">Select a venue</option>
                      {venues.data?.map((venue) => (
                        <option key={venue.id} value={venue.id}>
                          {humanName(venue.name, 'Untitled venue')}
                        </option>
                      ))}
                    </Select>
                  </Field>
                </div>
              </>
            ) : null}
            {step === 2 ? (
              <>
                <div className="wizard-heading">
                  <CalendarPlus size={20} />
                  <div>
                    <h2>Schedule</h2>
                    <p>Only the times you provide are sent to the real event contract.</p>
                  </div>
                </div>
                <div className="form-grid two">
                  <Field label="Starts">
                    <Input
                      id="event-start"
                      type="datetime-local"
                      value={startsAt}
                      onChange={(event) => setStartsAt(event.target.value)}
                    />
                  </Field>
                  <Field label="Ends">
                    <Input
                      id="event-end"
                      type="datetime-local"
                      value={endsAt}
                      onChange={(event) => setEndsAt(event.target.value)}
                    />
                  </Field>
                  <Field label="Sales open">
                    <Input
                      id="sales-open"
                      type="datetime-local"
                      value={salesOpenAt}
                      onChange={(event) => setSalesOpenAt(event.target.value)}
                    />
                  </Field>
                  <Field label="Sales close">
                    <Input
                      id="sales-close"
                      type="datetime-local"
                      value={salesCloseAt}
                      onChange={(event) => setSalesCloseAt(event.target.value)}
                    />
                  </Field>
                  <Field label="Admission open">
                    <Input
                      id="admission-open"
                      type="datetime-local"
                      value={admissionOpenAt}
                      onChange={(event) => setAdmissionOpenAt(event.target.value)}
                    />
                  </Field>
                  <Field label="Admission close">
                    <Input
                      id="admission-close"
                      type="datetime-local"
                      value={admissionCloseAt}
                      onChange={(event) => setAdmissionCloseAt(event.target.value)}
                    />
                  </Field>
                  <Field label="Timezone">
                    <Input
                      id="event-timezone"
                      value={timeZone}
                      onChange={(event) => setTimeZone(event.target.value)}
                    />
                  </Field>
                </div>
              </>
            ) : null}
            {step === 3 ? (
              <>
                <div className="wizard-heading">
                  <Check size={20} />
                  <div>
                    <h2>Review draft</h2>
                    <p>
                      The event remains a draft until its real setup is complete and sales are
                      opened.
                    </p>
                  </div>
                </div>
                <dl className="definition-grid">
                  <div>
                    <dt>Event</dt>
                    <dd>{name}</dd>
                  </div>
                  <div>
                    <dt>Venue</dt>
                    <dd>
                      {humanName(
                        venues.data?.find((venue) => venue.id === venueId)?.name,
                        'Untitled venue',
                      )}
                    </dd>
                  </div>
                  {scheduleRows.map(([label, value]) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>{value ? new Date(value).toLocaleString() : 'Not scheduled'}</dd>
                    </div>
                  ))}
                  <div>
                    <dt>Timezone</dt>
                    <dd>{timeZone || 'Not set'}</dd>
                  </div>
                </dl>
                {scheduleRows.some(([, value]) => !value) ? (
                  <InlineNotice tone="warning">
                    Any line marked Not scheduled will be left empty. Choose Back if you intended to
                    set it before creating the Event.
                  </InlineNotice>
                ) : (
                  <InlineNotice>
                    All Event, sales and admission schedule values are ready to save.
                  </InlineNotice>
                )}
                <InlineNotice>
                  Next: configure the sales policy, materialize a published layout, add pricing and
                  grant partner access.
                </InlineNotice>
                {mutation.error ? <ErrorState error={mutation.error} /> : null}
              </>
            ) : null}
            {error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
          </PanelBody>
          <div className="panel-divider" />
          <div className="wizard-actions">
            {step > 1 ? (
              <Button
                type="button"
                variant="secondary"
                onClick={() => setStep((value) => value - 1)}
              >
                <ArrowLeft size={16} />
                Back
              </Button>
            ) : (
              <span />
            )}
            {step < 3 ? (
              <Button
                key="continue"
                type="button"
                onClick={(event) => {
                  event.preventDefault();
                  next();
                }}
              >
                Continue
                <ArrowRight size={16} />
              </Button>
            ) : (
              <Button key="submit" type="submit" busy={mutation.isPending}>
                Create draft event
              </Button>
            )}
          </div>
        </Panel>
        <Panel className="wizard-help">
          <PanelBody>
            <h2>What happens next</h2>
            <ol>
              <li>
                <span>1</span>Set the transaction policy
              </li>
              <li>
                <span>2</span>Materialize a published venue layout
              </li>
              <li>
                <span>3</span>Add price tiers and assign pricing
              </li>
              <li>
                <span>4</span>Open sales when setup is complete
              </li>
            </ol>
          </PanelBody>
        </Panel>
      </div>
    </form>
  );
}
