import {
  Button,
  FormField,
  InlineNotice,
  PageHeader,
  Panel,
  ProductShell,
  StatusPill,
} from '@tktsync/ui';
import { resultLabel, resultTone } from './outcome';
import { useScanner } from './useScanner';

export function ScannerPage() {
  const {
    token,
    setToken,
    eventID,
    setEventID,
    deviceID,
    manual,
    setManual,
    result,
    error,
    busy,
    camera,
    video,
    submit,
    startCamera,
    reset,
  } = useScanner();

  return (
    <ProductShell
      product="Gate scanner"
      eyebrow="TktSync Admission"
      actions={
        <>
          <StatusPill tone={token && eventID ? 'success' : 'warning'}>
            {token && eventID ? 'Event authority set' : 'Setup needed'}
          </StatusPill>
          <span className="device">Device {deviceID.slice(0, 8)}</span>
        </>
      }
    >
      <PageHeader
        title="Authoritative admission"
        description="Every scan is decided by central ticket authority. A camera read alone never admits a guest."
        actions={
          <Button className="secondary" onClick={reset}>
            Next guest
          </Button>
        }
      />
      <div className="scanner-layout">
        <Panel className="scanner-panel">
          <div
            className={`result-stage ${resultTone(result?.result)} ${error ? 'danger' : ''}`}
            aria-live="assertive"
            aria-busy={busy}
          >
            <div className="result-icon">
              {error
                ? '!'
                : result?.result === 'ADMITTED' || result?.result === 'MANUAL_OVERRIDE_ADMITTED'
                  ? '✓'
                  : result?.result === 'TICKET_ALREADY_ADMITTED'
                    ? '↺'
                    : '⌁'}
            </div>
            <p>{resultLabel(result, error)}</p>
            {result?.ticket?.display && (
              <strong>
                {[
                  result.ticket.display.section_name,
                  result.ticket.display.row_label,
                  result.ticket.display.seat_label,
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </strong>
            )}
            {result?.admitted_at && (
              <small>Admitted {new Date(result.admitted_at).toLocaleTimeString()}</small>
            )}
            {error && <small>{error}</small>}
          </div>
          <div className="camera-frame">
            {camera ? (
              <video ref={video} autoPlay playsInline muted />
            ) : (
              <div>
                <span>⌗</span>
                <p>Camera is paused</p>
              </div>
            )}
            <i />
            <i />
            <i />
            <i />
          </div>
          <div className="scan-actions">
            <Button onClick={startCamera} disabled={busy || !token || !eventID}>
              {camera ? 'Scanning…' : 'Open camera'}
            </Button>
          </div>
        </Panel>
        <div className="side-stack">
          <Panel title="Gate setup" description="Scope this device to one event before scanning.">
            <div className="form-stack">
              <FormField label="Event ID">
                <input
                  value={eventID}
                  onChange={(e) => setEventID(e.target.value)}
                  placeholder="evt_…"
                  autoComplete="off"
                  aria-invalid={Boolean(eventID && !eventID.startsWith('evt_'))}
                />
              </FormField>
              <FormField label="Scanner bearer">
                <input
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="Human event-scoped token"
                  autoComplete="off"
                />
              </FormField>
            </div>
          </Panel>
          <Panel
            title="Manual scan"
            description="Paste decoded QR data when camera capture is unavailable."
          >
            <div className="form-stack">
              <FormField label="QR payload">
                <textarea
                  value={manual}
                  onChange={(e) => setManual(e.target.value)}
                  placeholder="qr1.…"
                  spellCheck={false}
                  autoComplete="off"
                />
              </FormField>
              <Button
                onClick={() => void submit(manual)}
                disabled={busy || !manual || !token || !eventID}
              >
                {busy ? 'Checking authority…' : 'Validate ticket'}
              </Button>
            </div>
          </Panel>
          <InlineNotice tone="warning" title="Fail closed">
            If authority is unavailable, cancelled, or cannot establish ticket state, do not admit
            the guest.
          </InlineNotice>
        </div>
      </div>
    </ProductShell>
  );
}
