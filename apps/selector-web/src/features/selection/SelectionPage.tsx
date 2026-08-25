import { useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertCircle,
  Check,
  ChevronRight,
  Clock3,
  LoaderCircle,
  LockKeyhole,
  MapPin,
  Minus,
  Plus,
  RefreshCw,
  ShieldCheck,
  Ticket,
  X,
} from 'lucide-react';
import { formatMoney, humanLabel, remaining } from './presentation';
import type { Layout, SelectionLine } from './types';
import { useSelectionSession } from './useSelectionSession';

function Brand() {
  return (
    <span className="selector-brand" aria-label="TktSync">
      <span className="selector-mark" aria-hidden="true">
        <Ticket size={17} strokeWidth={2.5} />
      </span>
      <span>
        Tkt<span>Sync</span>
      </span>
    </span>
  );
}

function eventDate(value?: string | null) {
  if (!value) return 'Date to be announced';
  return new Intl.DateTimeFormat(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(value));
}

function SelectionLines({ lines }: { lines: SelectionLine[] }) {
  return (
    <div className="selection-lines">
      {lines.map(({ offer, quantity }) => (
        <div className="selection-line" key={offer.offer_id}>
          <div>
            <strong>{humanLabel(offer.label, 'Ticket')}</strong>
            <span>
              {offer.kind === 'reserved'
                ? [
                    humanLabel(offer.row, '') ? `Row ${humanLabel(offer.row, '')}` : '',
                    humanLabel(offer.table, '') ? humanLabel(offer.table, '') : '',
                    humanLabel(offer.seat, '') ? `Seat ${humanLabel(offer.seat, '')}` : '',
                  ]
                    .filter(Boolean)
                    .join(' · ')
                : `Quantity ${quantity}`}
            </span>
          </div>
          <b>{formatMoney(offer.price.amount_minor * quantity, offer.price.currency)}</b>
        </div>
      ))}
    </div>
  );
}

function Summary({
  lines,
  busy,
  onReserve,
}: {
  lines: SelectionLine[];
  busy: boolean;
  onReserve: () => void;
}) {
  const total = lines.reduce((sum, line) => sum + line.offer.price.amount_minor * line.quantity, 0);
  const currency = lines[0]?.offer.price.currency ?? 'NGN';

  return (
    <aside className="selection-summary" aria-label="Your selection">
      <div className="summary-heading">
        <div>
          <p>Your selection</p>
          <span>{lines.reduce((sum, line) => sum + line.quantity, 0)} tickets</span>
        </div>
        <Ticket size={20} aria-hidden="true" />
      </div>
      {lines.length ? (
        <>
          <SelectionLines lines={lines} />
          <div className="summary-total">
            <span>Total</span>
            <strong>{formatMoney(total, currency)}</strong>
          </div>
          <button className="primary-action" type="button" onClick={onReserve} disabled={busy}>
            {busy ? (
              <>
                <LoaderCircle className="spin" size={18} /> Reserving your tickets…
              </>
            ) : (
              <>
                Hold tickets <ChevronRight size={18} />
              </>
            )}
          </button>
          <p className="summary-assurance">
            <LockKeyhole size={13} /> You won't be charged yet
          </p>
        </>
      ) : (
        <div className="summary-empty">
          <span>
            <Ticket size={24} />
          </span>
          <strong>Choose your tickets</strong>
          <p>Pick a seat on the map or choose a general admission quantity to begin.</p>
          <button type="button" disabled>
            Select tickets to continue
          </button>
        </div>
      )}
    </aside>
  );
}

function MobileSummary({
  lines,
  open,
  setOpen,
  busy,
  onReserve,
}: {
  lines: SelectionLine[];
  open: boolean;
  setOpen: (value: boolean) => void;
  busy: boolean;
  onReserve: () => void;
}) {
  const closeButton = useRef<HTMLButtonElement>(null);
  const opener = useRef<HTMLButtonElement>(null);
  const total = lines.reduce((sum, line) => sum + line.offer.price.amount_minor * line.quantity, 0);
  const currency = lines[0]?.offer.price.currency ?? 'NGN';

  useEffect(() => {
    if (!open) return;
    const trigger = opener.current;
    closeButton.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      trigger?.focus();
    };
  }, [open, setOpen]);

  if (!lines.length) {
    return (
      <div className="mobile-summary-bar empty">
        <div className="mobile-summary-copy">
          <strong>No tickets selected</strong>
          <span>Choose seats or a quantity</span>
        </div>
        <button className="primary-action" type="button" disabled>
          Review
        </button>
      </div>
    );
  }
  return (
    <>
      <div className="mobile-summary-bar">
        <button ref={opener} type="button" onClick={() => setOpen(true)}>
          <span>{lines.reduce((sum, line) => sum + line.quantity, 0)} tickets</span>
          <strong>{formatMoney(total, currency)}</strong>
        </button>
        <button className="primary-action" type="button" onClick={() => setOpen(true)}>
          Review <ChevronRight size={17} />
        </button>
      </div>
      {open && (
        <div className="sheet-backdrop" role="presentation" onMouseDown={() => setOpen(false)}>
          <section
            className="bottom-sheet"
            role="dialog"
            aria-modal="true"
            aria-labelledby="mobile-selection-title"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="sheet-handle" />
            <div className="sheet-heading">
              <h2 id="mobile-selection-title">Your selection</h2>
              <button
                ref={closeButton}
                type="button"
                aria-label="Close selection"
                onClick={() => setOpen(false)}
              >
                <X size={20} />
              </button>
            </div>
            <SelectionLines lines={lines} />
            <div className="summary-total">
              <span>Total</span>
              <strong>{formatMoney(total, currency)}</strong>
            </div>
            <button className="primary-action" type="button" onClick={onReserve} disabled={busy}>
              {busy ? 'Reserving your tickets…' : 'Hold tickets'}
            </button>
          </section>
        </div>
      )}
    </>
  );
}

function StateScreen({
  icon,
  title,
  description,
  action,
  nested = false,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  action?: React.ReactNode;
  nested?: boolean;
}) {
  const Tag = nested ? 'section' : 'main';
  return (
    <Tag className="selector-state">
      <span>{icon}</span>
      <h1>{title}</h1>
      <p>{description}</p>
      {action}
    </Tag>
  );
}

export function SelectionPage() {
  const session = useSelectionSession();
  const [reviewOpen, setReviewOpen] = useState(false);
  const availabilityByInventory = useMemo(
    () =>
      new Map(session.availability?.reserved_units.map((item) => [item.inventory_id, item]) ?? []),
    [session.availability],
  );
  const offerByInventory = useMemo(
    () => new Map(session.offers.map((offer) => [offer.inventory_id, offer])),
    [session.offers],
  );
  const selectedIDs = useMemo(
    () => new Set(session.selectedLines.map((line) => line.offer.offer_id)),
    [session.selectedLines],
  );
  const reservedSections = useMemo(() => {
    const sections = new Map<
      string,
      { name: string; rows: Map<string, Layout['reserved_units']> }
    >();
    for (const unit of session.layout?.reserved_units ?? []) {
      const section = unit.section_object_key ?? unit.section_id ?? unit.section_name ?? 'reserved';
      const current = sections.get(section) ?? {
        name: humanLabel(unit.section_name, 'Reserved seating'),
        rows: new Map(),
      };
      const rows = current.rows;
      const row = humanLabel(unit.row || unit.table, '—');
      rows.set(row, [...(rows.get(row) ?? []), unit]);
      sections.set(section, current);
    }
    return sections;
  }, [session.layout]);
  const gaOffers = session.offers.filter((offer) => offer.kind === 'ga');
  const spatialBounds = useMemo(() => {
    const objects = session.layout?.geometry?.objects ?? [];
    return {
      width: Math.max(1000, ...objects.map((item) => item.x + item.width)),
      height: Math.max(650, ...objects.map((item) => item.y + item.height)),
    };
  }, [session.layout]);
  const hasSpatialCollision = useMemo(() => {
    const objects = session.layout?.geometry?.objects ?? [];
    return objects.some((item, index) =>
      objects
        .slice(index + 1)
        .some(
          (other) =>
            item.x < other.x + other.width &&
            item.x + item.width > other.x &&
            item.y < other.y + other.height &&
            item.y + item.height > other.y,
        ),
    );
  }, [session.layout]);
  const spatialStyle = (
    item: NonNullable<NonNullable<Layout['geometry']>['objects']>[number],
    contentMinimum?: { width?: number; height?: number },
  ) => {
    const orientation = ['STAGE', 'RING', 'FIELD'].includes(item.type);
    const minimumWidth = contentMinimum?.width ?? (orientation ? 180 : 250);
    const minimumHeight = contentMinimum?.height ?? (orientation ? 90 : 160);
    const left = 2 + (item.x / spatialBounds.width) * 96;
    const top = 2 + (item.y / spatialBounds.height) * 96;
    return {
      left: `min(${left}%, calc(98% - ${minimumWidth}px))`,
      top: `min(${top}%, calc(98% - ${minimumHeight}px))`,
      width: `max(${(item.width / spatialBounds.width) * 96}%, ${minimumWidth}px)`,
      height: `max(${(item.height / spatialBounds.height) * 96}%, ${minimumHeight}px)`,
      transform: `rotate(${item.rotation ?? 0}deg)`,
    };
  };

  const shell = (content: React.ReactNode) => (
    <div className="selector-app">
      <header className="selector-header">
        <Brand />
        {session.event && (
          <div className="selector-event-mini">
            <strong>{humanLabel(session.event.name, 'Untitled event')}</strong>
            <span>{eventDate(session.event.starts_at)}</span>
          </div>
        )}
      </header>
      {content}
      <footer className="selector-footer">
        © {new Date().getFullYear()} TktSync · Tickets are only valid when purchased through an
        authorised seller.
      </footer>
    </div>
  );

  if (session.loading) {
    return shell(
      <main className="selector-loading" aria-live="polite">
        <div className="loading-copy">
          <LoaderCircle className="spin" />
          <h1>Loading ticket availability</h1>
        </div>
        <div className="loading-grid">
          <div className="skeleton skeleton-map" />
          <div className="skeleton skeleton-summary" />
        </div>
      </main>,
    );
  }

  if (session.invalidLink) {
    return shell(
      <StateScreen
        icon={<AlertCircle size={28} />}
        title="This ticket selection link is no longer available."
        description="Return to the ticket seller to start again."
      />,
    );
  }

  if (session.networkError) {
    return shell(
      <StateScreen
        icon={<RefreshCw size={28} />}
        title="We couldn't refresh availability."
        description="Check your connection, then try again."
        action={
          <button className="primary-action compact" onClick={() => void session.retry()}>
            Try again
          </button>
        }
      />,
    );
  }

  if (session.eventUnavailable) {
    return shell(
      <StateScreen
        icon={<Clock3 size={28} />}
        title="Ticket selection isn't available right now."
        description="Return to the ticket seller for the latest event information."
      />,
    );
  }

  if (session.hold) {
    const lines = session.heldLines;
    return shell(
      <main className="hold-view">
        <section className={`hold-card ${session.holdExpired ? 'expired' : ''}`} aria-live="polite">
          <span className="hold-icon">{session.holdExpired ? <Clock3 /> : <ShieldCheck />}</span>
          <h1>{session.holdExpired ? 'Your reservation expired' : 'Your tickets are held'}</h1>
          <p>
            {session.holdExpired
              ? "These tickets were released because checkout wasn't completed in time."
              : "We'll keep these tickets for you while you continue checkout."}
          </p>
          {!session.holdExpired && (
            <div className={`hold-timer ${session.holdNearExpiry ? 'warning' : ''}`}>
              <Clock3 size={18} />
              <span>
                Time remaining
                <strong>
                  {remaining(session.hold.hold_expires_at, session.now + session.serverOffsetMs)}
                </strong>
              </span>
            </div>
          )}
          {session.holdNearExpiry && (
            <p className="expiry-warning">Complete checkout soon to keep these tickets.</p>
          )}
          {lines.length > 0 && <SelectionLines lines={lines} />}
          {!session.holdExpired && (
            <div className="summary-total">
              <span>Total</span>
              <strong>
                {formatMoney(session.hold.total.amount_minor, session.hold.total.currency)}
              </strong>
            </div>
          )}
          {session.error && <div className="buyer-notice danger">{session.error}</div>}
          <div className="hold-actions">
            {session.holdExpired ? (
              <button
                className="primary-action"
                type="button"
                onClick={() => void session.release()}
              >
                Choose tickets again
              </button>
            ) : (
              <>
                <button className="primary-action" type="button" onClick={session.handoff}>
                  {session.handoffPending ? 'Returning to checkout…' : 'Continue to checkout'}
                </button>
                <button
                  className="text-action"
                  type="button"
                  onClick={() => void session.release()}
                  disabled={session.busy}
                >
                  Change selection
                </button>
              </>
            )}
          </div>
        </section>
      </main>,
    );
  }

  const empty = reservedSections.size === 0 && gaOffers.length === 0;
  return shell(
    <>
      <main className="selector-main">
        <section className="event-intro">
          <div>
            <p className="event-kicker">Choose your tickets</p>
            <h1>{humanLabel(session.event?.name, 'Untitled event')}</h1>
            <p className="event-date">{eventDate(session.event?.starts_at)}</p>
            {session.event?.venue_name && (
              <p className="event-venue">
                <MapPin size={15} /> {humanLabel(session.event.venue_name, 'Venue to be announced')}
              </p>
            )}
          </div>
          {session.refreshing && (
            <span className="refreshing" role="status">
              Updating…
            </span>
          )}
        </section>

        {session.availabilityNotice && (
          <div className="buyer-notice danger" role="alert">
            <AlertCircle size={18} /> {session.availabilityNotice}
          </div>
        )}
        {session.error && (
          <div className="buyer-notice danger" role="alert">
            <AlertCircle size={18} /> {session.error}
          </div>
        )}

        {empty ? (
          <StateScreen
            icon={<Ticket size={28} />}
            title="No tickets are currently available."
            description="Please check again later."
            nested
          />
        ) : (
          <div className="selector-grid">
            <div className="inventory-column">
              {reservedSections.size > 0 &&
                !(session.layout?.geometry?.objects ?? []).some((item) =>
                  ['STAGE', 'RING', 'FIELD'].includes(item.type),
                ) && <div className="stage-mark">Event area</div>}
              <div className="spatial-map-scroll">
                <div
                  className={`spatial-map ${hasSpatialCollision ? 'collision-safe-layout' : ''}`}
                  aria-label="Interactive venue floor plan"
                  style={{ aspectRatio: `${spatialBounds.width} / ${spatialBounds.height}` }}
                >
                  {(session.layout?.geometry?.objects ?? [])
                    .filter((item) => ['STAGE', 'RING', 'FIELD'].includes(item.type))
                    .map((item) => (
                      <div
                        className="spatial-orientation"
                        key={item.object_key}
                        style={spatialStyle(item)}
                      >
                        <span style={{ transform: `rotate(${-(item.rotation ?? 0)}deg)` }}>
                          {humanLabel(item.label, item.type)}
                        </span>
                      </div>
                    ))}
                  {[...reservedSections.entries()].map(([sectionKey, sectionData]) => {
                    const section = sectionData.name;
                    const rows = sectionData.rows;
                    const geo = session.layout?.geometry?.objects?.find(
                      (item) => item.object_key === sectionKey,
                    );
                    return (
                      <section
                        className={`seat-section ${geo ? 'spatial-section' : 'spatial-fallback'}`}
                        key={sectionKey}
                        aria-label={section}
                        style={
                          geo
                            ? spatialStyle(geo, {
                                width: 270,
                                height: Math.max(190, 92 + rows.size * 38),
                              })
                            : undefined
                        }
                      >
                        <div className="section-heading">
                          <div>
                            <h2>{section}</h2>
                            <p>Reserved seating</p>
                          </div>
                        </div>
                        <div className="seat-map-scroll">
                          <div className="seat-rows">
                            {[...rows.entries()].map(([row, units]) => (
                              <div className="seat-row" key={row}>
                                <span className="row-label">{row}</span>
                                <div className="row-seats">
                                  {units.map((unit) => {
                                    const current = availabilityByInventory.get(unit.inventory_id);
                                    const offer = offerByInventory.get(unit.inventory_id);
                                    const selected = Boolean(
                                      offer && selectedIDs.has(offer.offer_id),
                                    );
                                    const available = Boolean(current?.offer && offer);
                                    const readableRow = humanLabel(unit.row, '');
                                    const readableSeat = humanLabel(unit.seat, '');
                                    const location = [
                                      readableRow ? `Row ${readableRow}` : '',
                                      unit.table ? humanLabel(unit.table, '') : '',
                                      readableSeat ? `Seat ${readableSeat}` : '',
                                    ]
                                      .filter(Boolean)
                                      .join(' · ');
                                    const label = `${section}, ${location}, ${
                                      offer
                                        ? formatMoney(
                                            offer.price.amount_minor,
                                            offer.price.currency,
                                          )
                                        : 'unavailable'
                                    }`;
                                    return (
                                      <button
                                        key={unit.inventory_id}
                                        type="button"
                                        className={`seat ${selected ? 'selected' : available ? 'available' : 'unavailable'}`}
                                        disabled={!available}
                                        aria-label={label}
                                        aria-pressed={selected}
                                        onClick={() => offer && session.toggleReserved(offer)}
                                      >
                                        <span>{readableSeat || '—'}</span>
                                        {selected && <Check size={12} aria-hidden="true" />}
                                      </button>
                                    );
                                  })}
                                </div>
                                <span className="row-label">{row}</span>
                              </div>
                            ))}
                          </div>
                        </div>
                      </section>
                    );
                  })}

                  {gaOffers.map((offer) => {
                    const line = session.selectedLines.find(
                      (selection) => selection.offer.offer_id === offer.offer_id,
                    );
                    const quantity = line?.quantity ?? 0;
                    const max = offer.available_quantity ?? 0;
                    const pool = session.layout?.ga_pools.find(
                      (item) => item.inventory_id === offer.inventory_id,
                    );
                    const geo = session.layout?.geometry?.objects?.find(
                      (item) => item.object_key === pool?.section_object_key,
                    );
                    return (
                      <section
                        className={`ga-card ${geo ? 'spatial-ga' : 'spatial-fallback'}`}
                        key={offer.offer_id}
                        aria-label={humanLabel(offer.label, 'General admission')}
                        style={geo ? spatialStyle(geo, { width: 270, height: 210 }) : undefined}
                      >
                        <div className="ga-details">
                          <span className="ga-icon">
                            <Ticket size={19} />
                          </span>
                          <div>
                            <h2>{humanLabel(offer.label, 'General admission')}</h2>
                            <p>{humanLabel(offer.section_name, 'General admission')}</p>
                            <strong>
                              {formatMoney(offer.price.amount_minor, offer.price.currency)} each
                            </strong>
                            <small>{max} left</small>
                          </div>
                        </div>
                        <div
                          className="quantity-control"
                          aria-label={`${humanLabel(offer.label, 'Ticket')} quantity`}
                        >
                          <button
                            type="button"
                            aria-label={`Remove one ${humanLabel(offer.label, 'general admission')} ticket`}
                            disabled={quantity === 0}
                            onClick={() => session.setGAQuantity(offer, quantity - 1)}
                          >
                            <Minus size={17} />
                          </button>
                          <output aria-live="polite">{quantity}</output>
                          <button
                            type="button"
                            aria-label={`Add one ${humanLabel(offer.label, 'general admission')} ticket`}
                            disabled={quantity >= max}
                            onClick={() => session.setGAQuantity(offer, quantity + 1)}
                          >
                            <Plus size={17} />
                          </button>
                        </div>
                      </section>
                    );
                  })}
                </div>
              </div>

              <div className="seat-legend" aria-label="Seat status key">
                <span>
                  <i className="available" /> Available
                </span>
                <span>
                  <i className="selected">
                    <Check size={9} />
                  </i>{' '}
                  Selected
                </span>
                <span>
                  <i className="unavailable" /> Unavailable
                </span>
              </div>
            </div>
            <Summary
              lines={session.selectedLines}
              busy={session.busy}
              onReserve={() => void session.reserve()}
            />
          </div>
        )}
      </main>
      <MobileSummary
        lines={session.selectedLines}
        open={reviewOpen}
        setOpen={setReviewOpen}
        busy={session.busy}
        onReserve={() => void session.reserve()}
      />
    </>,
  );
}
