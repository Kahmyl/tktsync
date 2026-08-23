import {
  Button,
  EmptyState,
  ErrorState,
  InlineNotice,
  PageHeader,
  Panel,
  ProductShell,
  Skeleton,
  StatusPill,
} from '@tktsync/ui';
import { formatMoney, remaining } from './presentation';
import { useSelectionSession } from './useSelectionSession';

export function SelectionPage() {
  const {
    event,
    layout,
    availability,
    selected,
    setSelected,
    quantity,
    setQuantity,
    hold,
    error,
    busy,
    loading,
    refreshing,
    retry,
    serverOffsetMs,
    offers,
    reserve,
    add,
    release,
    handoff,
  } = useSelectionSession();

  const availabilityByInventory = new Map(
    availability?.reserved_units.map((item) => [item.inventory_id, item]) ?? [],
  );

  const reservedSections = new Map<string, NonNullable<typeof layout>['reserved_units']>();

  for (const unit of layout?.reserved_units ?? []) {
    const key = unit.section_id || 'Reserved seating';
    const section = reservedSections.get(key) ?? [];
    section.push(unit);
    reservedSections.set(key, section);
  }

  const gaOffers = offers.filter((offer) => offer.kind === 'ga');

  const maximumQuantity =
    selected?.kind === 'ga' ? Math.max(1, selected.available_quantity ?? 1) : 1;

  return (
    <ProductShell
      product="Seat selection"
      eyebrow="Secure partner checkout"
      actions={
        <StatusPill tone={event?.state === 'ON_SALE' ? 'success' : 'warning'}>
          {event?.state?.replaceAll('_', ' ') ?? 'Connecting'}
        </StatusPill>
      }
    >
      <PageHeader
        title={event?.name ?? 'Choose your place'}
        description="Live venue availability. A seat or area is protected only after the authoritative hold succeeds."
        actions={
          hold && (
            <div className="hold-clock">
              <span>Hold expires in</span>
              <strong>{remaining(hold.hold_expires_at, Date.now() + serverOffsetMs)}</strong>
            </div>
          )
        }
      />

      {error &&
        (event ? (
          <InlineNotice tone="danger" title="Selection unavailable">
            {error}
          </InlineNotice>
        ) : (
          <ErrorState
            title="Selection unavailable"
            description={error}
            retry={() => void retry()}
          />
        ))}

      {!hold ? (
        <div className="selector-grid">
          <Panel
            title="Venue selection"
            description="Unavailable reserved seats remain visible so the layout does not shift as inventory changes."
          >
            {loading && <Skeleton lines={6} />}

            {!loading && reservedSections.size === 0 && gaOffers.length === 0 && (
              <EmptyState
                title="No inventory available"
                description="Inventory may be held by another customer or the event may not currently be on sale."
                action={
                  <Button className="secondary" onClick={() => void retry()}>
                    Refresh availability
                  </Button>
                }
              />
            )}

            {[...reservedSections.entries()].map(([sectionID, units]) => (
              <section className="seat-section" key={sectionID} aria-label={`Section ${sectionID}`}>
                <div className="seat-section-heading">
                  <strong>{sectionID}</strong>
                  <span>{units.length} seats</span>
                </div>

                <div className="seat-map">
                  {units.map((unit) => {
                    const current = availabilityByInventory.get(unit.inventory_id);

                    const offer = current?.offer
                      ? offers.find((candidate) => candidate.offer_id === current.offer?.offer_id)
                      : undefined;

                    const available = Boolean(offer);

                    return (
                      <button
                        key={unit.inventory_id}
                        type="button"
                        className={[
                          'seat',
                          available ? 'available' : 'unavailable',
                          selected?.offer_id === offer?.offer_id ? 'selected' : '',
                        ]
                          .filter(Boolean)
                          .join(' ')}
                        disabled={!offer}
                        aria-label={unit.display_label ?? `${unit.row} seat ${unit.seat}`}
                        title={
                          offer
                            ? `${unit.row} · Seat ${unit.seat} · ${formatMoney(
                                offer.price.amount_minor,
                                offer.price.currency,
                              )}`
                            : `${unit.row} · Seat ${unit.seat} · Unavailable`
                        }
                        onClick={() => {
                          if (offer) setSelected(offer);
                        }}
                      >
                        <span>{unit.seat}</span>
                        <small>{unit.row}</small>
                      </button>
                    );
                  })}
                </div>
              </section>
            ))}

            {gaOffers.length > 0 && (
              <section className="ga-section" aria-label="General admission">
                <div className="seat-section-heading">
                  <strong>General admission</strong>
                  <span>Choose an area</span>
                </div>

                <div className="ga-grid">
                  {gaOffers.map((offer) => (
                    <button
                      key={offer.offer_id}
                      type="button"
                      className={
                        selected?.offer_id === offer.offer_id ? 'ga-card selected' : 'ga-card'
                      }
                      onClick={() => setSelected(offer)}
                    >
                      <span>
                        <strong>{offer.label}</strong>
                        <small>{offer.available_quantity ?? 0} available</small>
                      </span>
                      <b>{formatMoney(offer.price.amount_minor, offer.price.currency)}</b>
                    </button>
                  ))}
                </div>
              </section>
            )}

            {refreshing && (
              <small role="status" aria-live="polite">
                Updating availability…
              </small>
            )}

            <div className="seat-legend">
              <span>
                <i className="available" /> Available
              </span>
              <span>
                <i className="unavailable" /> Unavailable
              </span>
              <span>
                <i className="selected" /> Selected
              </span>
            </div>
          </Panel>

          <Panel
            title="Your selection"
            description="Review the price and quantity before creating an authoritative hold."
          >
            <div className="selection-card">
              {selected ? (
                <>
                  <span className="ticket-stub">{selected.label}</span>

                  {selected.kind === 'reserved' && (
                    <div className="selection-location">
                      <span>Reserved seat</span>
                      <strong>{[selected.row, selected.seat].filter(Boolean).join(' · ')}</strong>
                    </div>
                  )}

                  <div>
                    <span>Quantity</span>
                    <div className="stepper">
                      <button
                        disabled={selected.kind === 'reserved' || quantity <= 1}
                        onClick={() => setQuantity((value) => Math.max(1, value - 1))}
                      >
                        −
                      </button>
                      <strong>{quantity}</strong>
                      <button
                        disabled={selected.kind === 'reserved' || quantity >= maximumQuantity}
                        onClick={() => setQuantity((value) => Math.min(maximumQuantity, value + 1))}
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

                  <Button onClick={() => void reserve()} disabled={busy}>
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
            title="Your selection is held"
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
              <Button className="secondary" onClick={() => void add()} disabled={!selected || busy}>
                Add current selection
              </Button>
              <Button className="secondary" onClick={() => void release()} disabled={busy}>
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
