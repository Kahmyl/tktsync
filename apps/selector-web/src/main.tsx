/* eslint-disable react-refresh/only-export-components */
import { StrictMode, useCallback, useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { createTktSyncClient } from '@tktsync/api-client';
import { Button, InlineNotice, PageHeader, Panel, ProductShell, StatusPill } from '@tktsync/ui';
import './styles.css';

type Offer = {
  offer_id: string;
  available_quantity?: number;
  price: { amount_minor: number; currency: string };
};
type SelectableOffer = Offer & { label: string };
type Availability = {
  reserved_units: Array<{
    inventory_id: string;
    row: string;
    seat: string;
    sellability: string;
    offer?: Offer;
  }>;
  ga_pools: Array<{ inventory_id: string; name: string; offers: Offer[] }>;
  server_time: string;
};
type Session = { id: string; event_id: string; return_url: string; expires_at: string };
type EventView = { name: string; state: string; starts_at?: string };
type Hold = {
  id: string;
  status: string;
  hold_expires_at: string;
  reservation_token: string;
  items: Array<{ id: string; inventory_id: string; quantity: number }>;
  total: { amount_minor: number; currency: string };
  return_url: string;
};

export function formatMoney(amount: number, currency: string) {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount / 100);
}
export function remaining(until?: string, now = Date.now()) {
  if (!until) return '--:--';
  const seconds = Math.max(0, Math.floor((new Date(until).getTime() - now) / 1000));
  return `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`;
}

export function App() {
  const [capability] = useState(() => {
    return consumeCapability(location, history);
  });
  const [session, setSession] = useState<Session>();
  const [event, setEvent] = useState<EventView>();
  const [availability, setAvailability] = useState<Availability>();
  const [selected, setSelected] = useState<SelectableOffer>();
  const [quantity, setQuantity] = useState(1);
  const [hold, setHold] = useState<Hold>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [, tick] = useState(0);
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  const headers = useMemo(() => ({ Authorization: `Bearer ${capability}` }), [capability]);
  const refresh = useCallback(async () => {
    setError('');
    const [s, e, a] = await Promise.all([
      client.GET('/api/v1/selection/session', { headers }),
      client.GET('/api/v1/selection/event', { headers }),
      client.GET('/api/v1/selection/availability', { headers }),
    ]);
    if (s.error || e.error || a.error) {
      setError('This selection link is invalid, expired, or the event is not available.');
      return;
    }
    setSession(s.data as Session);
    setEvent(e.data as EventView);
    setAvailability(a.data as Availability);
  }, [client, headers]);
  useEffect(() => {
    if (capability) void refresh();
    else setError('Open the secure selection link supplied by the ticket partner.');
  }, [capability, refresh]);
  useEffect(() => {
    const timer = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(timer);
  }, []);
  const reserve = async () => {
    if (!selected) return;
    setBusy(true);
    setError('');
    const response = await client.POST('/api/v1/selection/reservations', {
      params: {
        header: {
          'Idempotency-Key': crypto.randomUUID(),
          'X-Request-ID': crypto.randomUUID(),
        },
      },
      headers,
      body: { items: [{ offer_id: selected.offer_id, quantity }] },
    });
    setBusy(false);
    if (response.error) {
      setError('That inventory could not be held. Availability has been refreshed.');
      await refresh();
      return;
    }
    setHold(response.data as Hold);
  };
  const add = async () => {
    if (!hold || !selected) return;
    setBusy(true);
    const response = await client.PATCH('/api/v1/selection/reservations/{reservation_id}', {
      params: {
        path: { reservation_id: hold.id },
        header: {
          'X-TktSync-Reservation-Token': hold.reservation_token,
          'Idempotency-Key': crypto.randomUUID(),
        },
      },
      headers,
      body: { add_items: [{ offer_id: selected.offer_id, quantity }] },
    });
    setBusy(false);
    if (response.error) {
      setError('The hold could not be changed.');
      return;
    }
    setHold({ ...(response.data as Hold), reservation_token: hold.reservation_token });
  };
  const release = async () => {
    if (!hold) return;
    setBusy(true);
    const response = await client.POST('/api/v1/selection/reservations/{reservation_id}/release', {
      params: {
        path: { reservation_id: hold.id },
        header: {
          'X-TktSync-Reservation-Token': hold.reservation_token,
          'Idempotency-Key': crypto.randomUUID(),
        },
      },
      headers,
      body: {},
    });
    setBusy(false);
    if (response.error) {
      setError('The hold could not be released.');
      return;
    }
    setHold(undefined);
    setSelected(undefined);
    await refresh();
  };
  const handoff = () => {
    if (!hold || !session) return;
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = session.return_url;
    form.style.display = 'none';
    for (const [name, value] of Object.entries({
      reservation_id: hold.id,
      reservation_token: hold.reservation_token,
    })) {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      form.appendChild(input);
    }
    document.body.appendChild(form);
    form.submit();
  };
  const offers: SelectableOffer[] = [
    ...(availability?.reserved_units.flatMap((item) =>
      item.offer ? [{ ...item.offer, label: `${item.row} · Seat ${item.seat}` }] : [],
    ) ?? []),
    ...(availability?.ga_pools.flatMap((pool) =>
      pool.offers.map((offer) => ({ ...offer, label: pool.name })),
    ) ?? []),
  ];
  return (
    <ProductShell
      product="Seat selection"
      eyebrow="Secure partner checkout"
      actions={
        <StatusPill tone={event?.state === 'ON_SALE' ? 'success' : 'warning'}>
          {event?.state?.replace('_', ' ') ?? 'Connecting'}
        </StatusPill>
      }
    >
      <PageHeader
        title={event?.name ?? 'Choose your place'}
        description="Live availability from the venue. Your selection is protected only after the hold is confirmed."
        actions={
          hold && (
            <div className="hold-clock">
              <span>Hold expires in</span>
              <strong>{remaining(hold.hold_expires_at)}</strong>
            </div>
          )
        }
      />
      {error && (
        <InlineNotice tone="danger" title="Selection unavailable">
          {error}
        </InlineNotice>
      )}
      {!hold ? (
        <div className="selector-grid">
          <Panel
            title="Available inventory"
            description={`${offers.length} bookable offers. Availability may change until your hold succeeds.`}
          >
            <div className="seat-list">
              {offers.map((offer) => (
                <button
                  key={offer.offer_id}
                  className={selected?.offer_id === offer.offer_id ? 'selected' : ''}
                  onClick={() => setSelected(offer)}
                >
                  <span className="seat-dot" />
                  <span>
                    <strong>{offer.label}</strong>
                    <small>
                      {offer.available_quantity && offer.available_quantity > 1
                        ? `${offer.available_quantity} available`
                        : 'Reserved seat'}
                    </small>
                  </span>
                  <b>{formatMoney(offer.price.amount_minor, offer.price.currency)}</b>
                </button>
              ))}
              {offers.length === 0 && <p className="muted">No inventory is currently available.</p>}
            </div>
          </Panel>
          <Panel
            title="Your selection"
            description="Review the price before creating an authoritative hold."
          >
            <div className="selection-card">
              {selected ? (
                <>
                  <span className="ticket-stub">{selected.label}</span>
                  <div>
                    <span>Quantity</span>
                    <div className="stepper">
                      <button onClick={() => setQuantity((q) => Math.max(1, q - 1))}>−</button>
                      <strong>{quantity}</strong>
                      <button
                        onClick={() =>
                          setQuantity((q) => Math.min(selected.available_quantity ?? 1, q + 1))
                        }
                      >
                        +
                      </button>
                    </div>
                  </div>
                  <div className="total">
                    <span>Total</span>
                    <strong>
                      {formatMoney(selected.price.amount_minor * quantity, selected.price.currency)}
                    </strong>
                  </div>
                  <Button onClick={reserve} disabled={busy}>
                    {busy ? 'Protecting inventory…' : 'Hold selection'}
                  </Button>
                </>
              ) : (
                <div className="pick-prompt">
                  <span>⌁</span>
                  <strong>Select a seat or area</strong>
                  <p>Your price and hold details will appear here.</p>
                </div>
              )}
            </div>
          </Panel>
        </div>
      ) : (
        <div className="hold-layout">
          <Panel
            title="Your seats are held"
            description="TktSync is protecting this inventory while you continue to the partner checkout."
          >
            <StatusPill tone="success">Authoritative hold active</StatusPill>
            <div className="hold-items">
              {hold.items.map((item) => (
                <div key={item.id}>
                  <span>{item.inventory_id}</span>
                  <b>× {item.quantity}</b>
                </div>
              ))}
            </div>
            <div className="total">
              <span>Order total</span>
              <strong>{formatMoney(hold.total.amount_minor, hold.total.currency)}</strong>
            </div>
            <div className="hold-actions">
              <Button onClick={handoff}>Continue to checkout</Button>
              <Button className="secondary" onClick={add} disabled={!selected || busy}>
                Add current selection
              </Button>
              <Button className="secondary" onClick={release} disabled={busy}>
                Release hold
              </Button>
            </div>
            <InlineNotice title="Secure handoff">
              Your reservation token is sent to the partner by HTTPS form POST. It is never placed
              in the URL.
            </InlineNotice>
          </Panel>
        </div>
      )}
    </ProductShell>
  );
}

export function consumeCapability(
  source: Pick<Location, 'hash' | 'pathname' | 'search'>,
  browserHistory: Pick<History, 'replaceState'>,
) {
  const value = source.hash.slice(1);
  if (value) browserHistory.replaceState(null, '', source.pathname + source.search);
  return value;
}
const root = typeof document === 'undefined' ? null : document.getElementById('root');
if (root)
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
