/* eslint-disable react-refresh/only-export-components */
import { StrictMode, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { createTktSyncClient } from '@tktsync/api-client';
import {
  Button,
  FormField,
  InlineNotice,
  PageHeader,
  Panel,
  ProductShell,
  StatusPill,
} from '@tktsync/ui';
import './styles.css';

type ScanResult = {
  result: string;
  ticket?: {
    id: string;
    display: { section_name?: string; row_label?: string; seat_label?: string };
  };
  admission_id?: string;
  admitted_at?: string;
  previous_admission?: { admitted_at: string; gate_reference: string };
  scan_attempt_id?: string;
};
type Detector = { detect(source: CanvasImageSource): Promise<Array<{ rawValue: string }>> };
type DetectorConstructor = new (options: { formats: string[] }) => Detector;
export const tone = (outcome?: string) =>
  !outcome
    ? 'neutral'
    : outcome === 'ADMITTED' || outcome === 'MANUAL_OVERRIDE_ADMITTED'
      ? 'success'
      : outcome === 'TICKET_ALREADY_ADMITTED'
        ? 'warning'
        : 'danger';
export const label = (result?: ScanResult, error?: string) =>
  error ? 'AUTHORITY UNAVAILABLE' : (result?.result?.replaceAll('_', ' ') ?? 'READY TO SCAN');

export function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem('tktsync.scanner.token') ?? '');
  const [eventID, setEventID] = useState(
    () => sessionStorage.getItem('tktsync.scanner.event') ?? '',
  );
  const [deviceID] = useState(() => {
    const existing = sessionStorage.getItem('tktsync.scanner.device');
    const id = existing ?? crypto.randomUUID();
    sessionStorage.setItem('tktsync.scanner.device', id);
    return id;
  });
  const [manual, setManual] = useState('');
  const [result, setResult] = useState<ScanResult>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [camera, setCamera] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const stream = useRef<MediaStream | null>(null);
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  const submit = useCallback(
    async (qr: string) => {
      if (!qr.trim() || busy) return;
      setBusy(true);
      setError('');
      sessionStorage.setItem('tktsync.scanner.token', token);
      sessionStorage.setItem('tktsync.scanner.event', eventID);
      try {
        const response = await client.POST('/api/v1/admission/scans', {
          params: {
            header: {
              'Idempotency-Key': crypto.randomUUID(),
              'X-Request-ID': crypto.randomUUID(),
            },
          },
          headers: {
            Authorization: `Bearer ${token}`,
          },
          body: { event_id: eventID, credential: qr.trim(), gate_reference: deviceID },
        });
        if (response.error) {
          const failure = response.error as { error?: { code?: string; message?: string } };
          setResult(undefined);
          setError(
            failure.error?.code === 'AUTHORITY_TEMPORARILY_UNAVAILABLE'
              ? 'Central authority could not be reached. Do not admit.'
              : (failure.error?.message ?? 'Scan was rejected.'),
          );
          return;
        }
        setResult(response.data as unknown as ScanResult);
        setManual('');
      } catch {
        setResult(undefined);
        setError('Central authority could not be reached. Do not admit.');
      } finally {
        setBusy(false);
      }
    },
    [busy, client, deviceID, eventID, token],
  );
  const startCamera = async () => {
    try {
      stream.current = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: 'environment' } },
        audio: false,
      });
      if (video.current) video.current.srcObject = stream.current;
      setCamera(true);
    } catch {
      setError('Camera access was denied. Use manual scan entry.');
    }
  };
  useEffect(() => {
    if (!camera) return;
    const Detector = (window as unknown as { BarcodeDetector?: DetectorConstructor })
      .BarcodeDetector;
    if (!Detector) {
      setError('Barcode scanning is unavailable in this browser. Use manual scan entry.');
      return;
    }
    const detector = new Detector({ formats: ['qr_code'] });
    let active = true;
    const scan = async () => {
      if (!active || !video.current) return;
      try {
        const codes = await detector.detect(video.current);
        const raw = codes[0]?.rawValue;
        if (raw) {
          await submit(raw);
          setCamera(false);
          stream.current?.getTracks().forEach((track) => track.stop());
          return;
        }
      } catch {
        setError('The camera image could not be read.');
      }
      if (active) setTimeout(() => void scan(), 250);
    };
    void scan();
    return () => {
      active = false;
    };
  }, [camera, submit]);
  useEffect(() => () => stream.current?.getTracks().forEach((track) => track.stop()), []);
  const reset = () => {
    setResult(undefined);
    setError('');
    setManual('');
  };
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
          <div className={`result-stage ${tone(result?.result)} ${error ? 'danger' : ''}`}>
            <div className="result-icon">
              {error
                ? '!'
                : result?.result === 'ADMITTED' || result?.result === 'MANUAL_OVERRIDE_ADMITTED'
                  ? '✓'
                  : result?.result === 'TICKET_ALREADY_ADMITTED'
                    ? '↺'
                    : '⌁'}
            </div>
            <p>{label(result, error)}</p>
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
                />
              </FormField>
              <FormField label="Scanner bearer">
                <input
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="Human event-scoped token"
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
const root = typeof document === 'undefined' ? null : document.getElementById('root');
if (root)
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
