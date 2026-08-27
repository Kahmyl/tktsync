import { useEffect, useState } from 'react';
import { demoAPI } from './api';
import type { Event, Order, TicketResult } from './types';

function money(price?: { amount_minor: number; currency: string } | null) {
  if (!price) return 'Price shown during selection';
  return new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency: price.currency,
    maximumFractionDigits: 0,
  }).format(price.amount_minor / 100);
}
function date(value?: string | null) {
  if (!value) return 'Date to be announced';
  return new Intl.DateTimeFormat(undefined, {
    weekday: 'long',
    month: 'long',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(value));
}
function eventReference(id: string) {
  const readable = id.replace(/^evt_/i, '').replace(/[^a-z0-9]/gi, '');
  return (readable.slice(-8) || 'EVENT').toUpperCase();
}
function itemName(item: Order['reservation']['items'][number]) {
  const d = item.display;
  if (item.inventory_kind === 'GA')
    return d.label || d.section || item.price_tier_label || 'General admission';
  return [
    d.section || item.price_tier_label || 'Reserved seat',
    d.table && `Table ${d.table}`,
    d.row && `Row ${d.row}`,
    d.seat && `Seat ${d.seat}`,
  ]
    .filter(Boolean)
    .join(' · ');
}

function StorefrontShell({
  partnerName,
  children,
}: {
  partnerName: string;
  children: React.ReactNode;
}) {
  return (
    <>
      <div className="demo-banner">
        <strong>Demo Partner Application</strong>
        <span>
          This sample storefront is not part of TktSync. It demonstrates an independent ticketing
          platform using the real TktSync Partner API.
        </span>
      </div>
      <header>
        <a href="/events" className="partner-brand">
          <span>✦</span>
          <div>
            <strong>{partnerName}</strong>
            <small>Demo ticket storefront</small>
          </div>
        </a>
        <a href="/events" className="events-link">
          Events
        </a>
      </header>
      <main>{children}</main>
      <footer>
        <strong>{partnerName} — Demo storefront</strong>
        <span>Assessment-only reference implementation</span>
      </footer>
    </>
  );
}

type Connection = { id: string; name: string; active: boolean };

function ConnectionSetup() {
  const issue = new URLSearchParams(location.search).get('connection');
  return (
    <section className="connection-setup connection-only">
      <form className="connection-form" method="post" action="/demo-api/connections">
        <p className="eyebrow">Demo Partner setup</p>
        <h1>Choose the Partner for this storefront.</h1>
        <p className="setup-intro">
          Add the Partner created in Admin, or open a previously saved Partner connection.
        </p>
        <h2>Add an existing Partner</h2>
        <p>
          The credential is sent directly to the Demo Partner server and saved in an encrypted,
          HttpOnly browser cookie.
        </p>
        {issue === 'missing' && (
          <p className="inline-error" role="alert">
            Enter both the Partner name and credential.
          </p>
        )}
        {issue === 'invalid' && (
          <p className="inline-error" role="alert">
            The TktSync API rejected that credential. Copy the complete one-time credential from the
            same Partner in Admin, or issue a replacement credential and try again.
          </p>
        )}
        {issue === 'configuration' && (
          <p className="inline-error" role="alert">
            The Demo Partner deployment is not connected to the TktSync API. This is a deployment
            configuration issue, not a problem with your Partner credential.
          </p>
        )}
        {issue === 'unavailable' && (
          <p className="inline-error" role="alert">
            The Demo Partner server cannot reach the TktSync API right now. Keep the credential safe
            and retry after the deployment is available.
          </p>
        )}
        <label htmlFor="partner-name">Partner name</label>
        <input
          id="partner-name"
          name="name"
          autoComplete="organization"
          required
          placeholder="Example: Demo Partner"
        />
        <p className="field-note">
          This name labels the sample storefront. The credential securely determines which Partner
          and Events the TktSync API permits.
        </p>
        <label htmlFor="partner-credential">Partner credential</label>
        <input
          id="partner-credential"
          name="credential"
          type="password"
          autoComplete="off"
          required
          placeholder="Paste the one-time credential from Admin"
        />
        <button className="cta" type="submit">
          Connect and view Events
        </button>
        <a className="saved-link" href="/connections">
          View saved Partners →
        </a>
        <div className="connection-boundary">
          <strong>Assessment setup only</strong>
          <span>This is not TktSync Partner onboarding or a Partner portal.</span>
        </div>
      </form>
    </section>
  );
}

function ConnectionsPage() {
  const [connections, setConnections] = useState<Connection[]>();
  const [error, setError] = useState('');
  useEffect(() => {
    demoAPI<{ items: Connection[] }>('/connections')
      .then((result) => setConnections(result.items))
      .catch((reason: Error) => setError(reason.message));
  }, []);
  if (error) return <ErrorView message={error} />;
  if (!connections)
    return (
      <Loading
        title="Loading saved Partners"
        copy="Reading the secure connections on this device…"
      />
    );
  return (
    <section className="connections-page">
      <a className="back" href="/">
        ← Add another connection
      </a>
      <div className="page-intro">
        <p className="eyebrow">Assessment connections</p>
        <h1>Saved Partners</h1>
        <p>Choose which Partner should appear as the ticket storefront.</p>
      </div>
      {connections.length === 0 ? (
        <div className="empty">
          <h2>No Partner connected yet</h2>
          <p>Add the Partner created in Admin to begin.</p>
          <a href="/">Add Partner connection</a>
        </div>
      ) : (
        <div className="connection-list">
          {connections.map((connection) => (
            <article key={connection.id}>
              <div>
                <span>{connection.active ? 'Active connection' : 'Saved connection'}</span>
                <h2>{connection.name}</h2>
                <p>The credential is encrypted and never returned to this page.</p>
              </div>
              <div className="connection-actions">
                {connection.active ? (
                  <a href="/events">View Events →</a>
                ) : (
                  <form method="post" action={`/demo-api/connections/${connection.id}/activate`}>
                    <button type="submit">Use this Partner</button>
                  </form>
                )}
                {connection.id !== 'deployment' && (
                  <form method="post" action={`/demo-api/connections/${connection.id}/remove`}>
                    <button className="remove" type="submit">
                      Remove
                    </button>
                  </form>
                )}
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
function Loading({
  title = 'Loading tickets',
  copy = 'Connecting to the live TktSync Partner API…',
}: {
  title?: string;
  copy?: string;
}) {
  return (
    <div className="state">
      <span className="loader" />
      <h1>{title}</h1>
      <p>{copy}</p>
    </div>
  );
}
function ErrorView({ message, retry }: { message: string; retry?: () => void }) {
  return (
    <div className="state error" role="alert">
      <span>!</span>
      <h1>We couldn’t load this</h1>
      <p>{message}</p>
      {retry ? <button onClick={retry}>Try again</button> : <a href="/events">Back to Events</a>}
    </div>
  );
}

function EventsPage() {
  const [events, setEvents] = useState<Event[]>();
  const [connection, setConnection] = useState<{ id: string; name: string }>();
  const [error, setError] = useState('');
  const load = () => {
    setError('');
    demoAPI<{ items: Event[]; connection: { id: string; name: string } }>('/events')
      .then((r) => {
        setEvents(r.items);
        setConnection(r.connection);
      })
      .catch((e: Error) => setError(e.message));
  };
  useEffect(load, []);
  if (error) return <ErrorView message={error} retry={load} />;
  if (!events) return <Loading />;
  return (
    <section>
      <div className="page-intro">
        <p className="eyebrow">Live Events</p>
        <h1>Find your next night out.</h1>
        <p>Every Event below is returned by the real TktSync Partner API.</p>
        {connection && (
          <div className="active-connection">
            <span>Connected as</span>
            <strong>{connection.name}</strong>
            <a href="/connections">Switch Partner</a>
          </div>
        )}
      </div>
      {events.length === 0 ? (
        <div className="empty">
          <h2>No Events are on the programme yet</h2>
          <p>
            The demo is connected, but no Events are available to this Partner. Run the assessment
            demo seed and refresh.
          </p>
        </div>
      ) : (
        <div className="event-grid">
          {events.map((event) => (
            <article className="event-card" key={event.id}>
              <div className="event-art">
                <span>LIVE EVENT</span>
                <b>✦</b>
              </div>
              <div className="event-card-body">
                <p className={`sale-state ${event.state.toLowerCase()}`}>
                  {event.state === 'ON_SALE'
                    ? 'On sale'
                    : event.state === 'PAUSED'
                      ? 'Sales paused'
                      : 'Not on sale'}
                </p>
                <h2>{event.name}</h2>
                <dl>
                  <div>
                    <dt>When</dt>
                    <dd>{date(event.starts_at)}</dd>
                  </div>
                  <div>
                    <dt>Where</dt>
                    <dd>{event.venue_name || 'Venue to be announced'}</dd>
                  </div>
                </dl>
                <div className="card-bottom">
                  <span>
                    From <strong>{money(event.starting_price)}</strong>
                  </span>
                  <a href={`/events/${event.id}`}>View event →</a>
                </div>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function EventPage({ id, partnerName }: { id: string; partnerName: string }) {
  const [event, setEvent] = useState<Event>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    demoAPI<Event>(`/events/${id}`)
      .then(setEvent)
      .catch((e: Error) => setError(e.message));
  }, [id]);
  const choose = async () => {
    setBusy(true);
    setError('');
    try {
      const session = await demoAPI<{ selection_url: string }>(`/events/${id}/selection`, {
        method: 'POST',
        body: '{}',
      });
      location.assign(session.selection_url);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to start ticket selection.');
      setBusy(false);
    }
  };
  if (error && !event) return <ErrorView message={error} />;
  if (!event) return <Loading />;
  const open = event.state === 'ON_SALE';
  return (
    <section className="detail">
      <a className="back" href="/events">
        ← All Events
      </a>
      <div className="detail-layout">
        <div className="event-poster">
          <span>{partnerName} presents</span>
          <strong>✦</strong>
          <p>{event.name}</p>
        </div>
        <div className="detail-copy">
          <p className={`sale-state ${event.state.toLowerCase()}`}>
            {open
              ? 'Tickets available'
              : event.state === 'PAUSED'
                ? 'Sales temporarily paused'
                : 'Sales not open'}
          </p>
          <h1>{event.name}</h1>
          <p className="lead">
            A live ticketed experience at {event.venue_name || 'a venue to be announced'}.
          </p>
          <div className="facts">
            <div>
              <span>Date & time</span>
              <strong>{date(event.starts_at)}</strong>
            </div>
            <div>
              <span>Venue</span>
              <strong>{event.venue_name || 'To be announced'}</strong>
              <small>{event.address_text}</small>
            </div>
            <div>
              <span>Tickets</span>
              <strong>From {money(event.starting_price)}</strong>
            </div>
          </div>
          {error && (
            <p className="inline-error" role="alert">
              {error}
            </p>
          )}
          <button className="cta" disabled={!open || busy} onClick={choose}>
            {busy
              ? 'Opening secure selection…'
              : open
                ? 'Choose tickets'
                : event.state === 'PAUSED'
                  ? 'Sales are paused'
                  : 'Tickets unavailable'}
          </button>
          <details className="assessment-note">
            <summary>Assessment note · How this works</summary>
            <p>
              Ticket selection opens the TktSync-hosted Selector. {partnerName} never owns the
              authoritative inventory state, and its Partner credential stays on the server.
            </p>
          </details>
        </div>
      </div>
    </section>
  );
}

function OrderLines({ order }: { order: Order }) {
  return (
    <div className="order-lines">
      {order.reservation.items.map((item) => (
        <div key={item.id}>
          <div>
            <strong>{itemName(item)}</strong>
            <span>
              {item.price_tier_label ||
                (item.inventory_kind === 'GA' ? 'General admission' : 'Reserved ticket')}{' '}
              · Qty {item.quantity}
            </span>
          </div>
          <b>
            {money({
              amount_minor: item.unit_amount_minor * item.quantity,
              currency: item.currency,
            })}
          </b>
        </div>
      ))}
      <div className="order-total">
        <span>Total</span>
        <strong>{money(order.reservation.total)}</strong>
      </div>
    </div>
  );
}

function Countdown({ end, server }: { end: string; server: string }) {
  const [remaining, setRemaining] = useState(
    Math.max(0, new Date(end).getTime() - new Date(server).getTime()),
  );
  useEffect(() => {
    const timer = setInterval(() => setRemaining((v) => Math.max(0, v - 1000)), 1000);
    return () => clearInterval(timer);
  }, []);
  const minutes = Math.floor(remaining / 60000);
  const seconds = Math.floor((remaining % 60000) / 1000);
  return (
    <span className={remaining < 60_000 ? 'ending' : ''}>
      {minutes}:{String(seconds).padStart(2, '0')} remaining
    </span>
  );
}

function CheckoutPage({ partnerName }: { partnerName: string }) {
  const [order, setOrder] = useState<Order>();
  const [stage, setStage] = useState<'review' | 'payment'>('review');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    window.scrollTo({ top: 0 });
  }, [stage]);
  useEffect(() => {
    demoAPI<Order>('/checkout')
      .then(setOrder)
      .catch((e: Error) => setError(e.message));
  }, []);
  const begin = async () => {
    setBusy(true);
    setError('');
    try {
      await demoAPI('/checkout/begin', { method: 'POST', body: '{}' });
      setStage('payment');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to protect checkout.');
    } finally {
      setBusy(false);
    }
  };
  const confirm = async () => {
    setBusy(true);
    setError('');
    try {
      await demoAPI('/checkout/confirm', { method: 'POST', body: '{}' });
      location.assign('/ticket');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Confirmation is temporarily unavailable.');
      setBusy(false);
    }
  };
  const fail = async () => {
    setBusy(true);
    setError('');
    try {
      await demoAPI('/checkout/fail', { method: 'POST', body: '{}' });
      setStage('review');
      setError(
        'Demo payment declined. No charge was made. Continue to payment to start a new protected attempt.',
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to report the payment result.');
    } finally {
      setBusy(false);
    }
  };
  if (error && !order) return <ErrorView message={error} />;
  if (!order) return <Loading />;
  return (
    <section className="checkout">
      <div className="checkout-head">
        <p className="eyebrow">{stage === 'review' ? 'Secure checkout' : 'Demonstration step'}</p>
        <h1>{stage === 'review' ? 'Review your order' : 'Demo payment'}</h1>
        <p>
          {stage === 'review'
            ? 'Your tickets are temporarily held by TktSync.'
            : 'Payment is owned by the ticketing Partner and is intentionally simulated in this assessment environment.'}
        </p>
        <span className="checkout-partner">Storefront by {partnerName}</span>
      </div>
      <div className="checkout-grid">
        <div className="checkout-main">
          {stage === 'review' ? (
            <>
              <h2>{order.event.name}</h2>
              <p>
                {date(order.event.starts_at)} · {order.event.venue_name}
              </p>
              <OrderLines order={order} />
              {error && (
                <p className="inline-error" role="alert">
                  {error}
                </p>
              )}
              <button className="cta" disabled={busy} onClick={begin}>
                {busy ? 'Protecting checkout…' : 'Continue to payment'}
              </button>
              <p className="button-note">This first asks TktSync to protect the checkout window.</p>
            </>
          ) : (
            <>
              <div className="demo-payment">
                <span>DEMO</span>
                <h2>No card details required</h2>
                <p>
                  The success action sends a simulated Partner payment reference through the real
                  TktSync confirmation lifecycle.
                </p>
              </div>
              {error && (
                <p className="inline-error" role="alert">
                  {error}
                </p>
              )}
              <button className="cta" disabled={busy} onClick={confirm}>
                {busy ? 'Issuing your ticket…' : 'Simulate successful payment'}
              </button>
              <button className="text-button" disabled={busy} onClick={fail}>
                Simulate failed payment
              </button>
            </>
          )}
        </div>
        <aside className="hold-card">
          <span>Ticket hold</span>
          <Countdown
            end={order.reservation.hold_expires_at}
            server={order.reservation.server_time}
          />
          <small>TktSync is protecting this inventory while you check out.</small>
        </aside>
      </div>
    </section>
  );
}

function TicketPage({ partnerName }: { partnerName: string }) {
  const [result, setResult] = useState<TicketResult>();
  const [error, setError] = useState('');
  useEffect(() => {
    demoAPI<TicketResult>('/ticket')
      .then(setResult)
      .catch((e: Error) => setError(e.message));
  }, []);
  if (error) return <ErrorView message={error} />;
  if (!result) return <Loading />;
  return (
    <section className="ticket-page">
      <div className="success-mark">✓</div>
      <p className="eyebrow">Order confirmed</p>
      <h1>Your ticket is ready.</h1>
      <p className="success-copy">
        {partnerName} has completed the order. TktSync issued the ticket and its admission
        credential.
      </p>
      {result.confirmation.tickets.map((ticket, index) => {
        const credential = result.credentials[index];
        const item = result.reservation.items[Math.min(index, result.reservation.items.length - 1)];
        return (
          <article className="ticket" key={ticket.id}>
            <div className="ticket-info">
              <span className="ticket-brand">✦ {partnerName}</span>
              <p>{result.event.name}</p>
              <h2>{item ? itemName(item) : 'Event admission'}</h2>
              <dl>
                <div>
                  <dt>Date</dt>
                  <dd>{date(result.event.starts_at)}</dd>
                </div>
                <div>
                  <dt>Venue</dt>
                  <dd>{result.event.venue_name || 'Venue to be announced'}</dd>
                </div>
                <div>
                  <dt>Event reference</dt>
                  <dd>
                    <code>{eventReference(result.event.id)}</code>
                  </dd>
                </div>
                <div>
                  <dt>Ticket public ID</dt>
                  <dd>
                    <code>{ticket.id}</code>
                  </dd>
                </div>
                <div>
                  <dt>Status</dt>
                  <dd className="active-ticket">{ticket.status}</dd>
                </div>
              </dl>
            </div>
            <div className="qr">
              <img src={credential?.qr_url} alt={`Entry QR code for ticket ${ticket.id}`} />
              <strong>QR generated and validated by TktSync</strong>
              <span>Present this code at entry</span>
            </div>
          </article>
        );
      })}
      <div className="scanner-handoff">
        <div>
          <h2>Ready to test admission?</h2>
          <p>
            Open Scanner and scan the QR above. The first scan should be admitted; a second scan
            should be rejected as a duplicate.
          </p>
        </div>
        <a href={result.scanner_url} target="_blank" rel="noreferrer">
          Test this ticket in Scanner →
        </a>
      </div>
      <details className="assessment-note">
        <summary>Assessment note · Product boundary</summary>
        <p>
          {partnerName} controls this ticket presentation. TktSync owns the QR credential and
          admission authority.
        </p>
      </details>
    </section>
  );
}

function App() {
  const path = location.pathname;
  const eventId = path.match(/^\/events\/(evt_[^/]+)$/)?.[1];
  const setupRoute = path === '/' || path === '/connections';
  const [activePartner, setActivePartner] = useState<Connection>();
  const [connectionError, setConnectionError] = useState('');
  useEffect(() => {
    window.scrollTo({ top: 0 });
  }, [path]);
  useEffect(() => {
    if (setupRoute) return;
    demoAPI<{ items: Connection[] }>('/connections')
      .then((result) => {
        const active = result.items.find((connection) => connection.active);
        if (!active) location.replace('/');
        else setActivePartner(active);
      })
      .catch((error: Error) => setConnectionError(error.message));
  }, [setupRoute]);

  if (setupRoute) {
    return (
      <main className="setup-main">
        {path === '/connections' ? <ConnectionsPage /> : <ConnectionSetup />}
      </main>
    );
  }
  if (connectionError)
    return (
      <main className="setup-main">
        <ErrorView message={connectionError} />
      </main>
    );
  if (!activePartner)
    return (
      <main className="setup-main">
        <Loading title="Opening storefront" copy="Loading the selected Partner connection…" />
      </main>
    );

  return (
    <StorefrontShell partnerName={activePartner.name}>
      {path === '/checkout' ? (
        <CheckoutPage partnerName={activePartner.name} />
      ) : path === '/ticket' ? (
        <TicketPage partnerName={activePartner.name} />
      ) : eventId ? (
        <EventPage id={eventId} partnerName={activePartner.name} />
      ) : (
        <EventsPage />
      )}
    </StorefrontShell>
  );
}
export default App;
