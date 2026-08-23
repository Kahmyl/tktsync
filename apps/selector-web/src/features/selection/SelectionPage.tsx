import { Button, InlineNotice, PageHeader, Panel, ProductShell, StatusPill } from '@tktsync/ui';
import { formatMoney, remaining } from './presentation';
import { useSelectionSession } from './useSelectionSession';

export function SelectionPage() {
  const {
    event,
    selected,
    setSelected,
    quantity,
    setQuantity,
    hold,
    error,
    busy,
    serverOffsetMs,
    offers,
    reserve,
    add,
    release,
    handoff,
  } = useSelectionSession();

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
              <strong>{remaining(hold.hold_expires_at, Date.now() + serverOffsetMs)}</strong>
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
