import { useEffect, useMemo, useRef, useState } from 'react';
import {
  AlertTriangle,
  Camera,
  Check,
  ChevronRight,
  CircleAlert,
  Clock3,
  Keyboard,
  LoaderCircle,
  LogOut,
  MapPin,
  QrCode,
  RefreshCw,
  ScanLine,
  Settings,
  TicketCheck,
  Vibrate,
  Volume2,
  X,
  Zap,
} from 'lucide-react';
import { useOperatorSession } from '../../auth/useOperatorSession';
import { outcomePresentation, ticketLocation } from './outcome';
import type { RecentScan, ScannerEvent } from './types';
import { useAuthorizedEvents, useScanner } from './useScanner';

function Brand() {
  return (
    <span className="scanner-brand" aria-label="TktSync Scanner">
      <span>
        <ScanLine size={19} />
      </span>
      Tkt<span>Sync</span>
    </span>
  );
}

function formatEventTime(value?: string | null) {
  if (!value) return 'Date to be announced';
  return new Intl.DateTimeFormat(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(value));
}

function Sheet({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
}) {
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    close.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onClose, open]);
  if (!open) return null;
  return (
    <div className="scanner-sheet-backdrop" role="presentation" onMouseDown={onClose}>
      <section
        className="scanner-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby={`sheet-${title.replaceAll(' ', '-').toLowerCase()}`}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="scanner-sheet-handle" />
        <div className="scanner-sheet-heading">
          <h2 id={`sheet-${title.replaceAll(' ', '-').toLowerCase()}`}>{title}</h2>
          <button ref={close} type="button" aria-label={`Close ${title}`} onClick={onClose}>
            <X size={20} />
          </button>
        </div>
        {children}
      </section>
    </div>
  );
}

function SignIn({
  email,
  setEmail,
  password,
  setPassword,
  loading,
  error,
  onSubmit,
}: {
  email: string;
  setEmail: (value: string) => void;
  password: string;
  setPassword: (value: string) => void;
  loading: boolean;
  error: string;
  onSubmit: () => void;
}) {
  return (
    <main className="scanner-auth">
      <div className="scanner-auth-brand">
        <Brand />
      </div>
      <section className="signin-card">
        <span className="signin-icon">
          <QrCode size={25} />
        </span>
        <h1>Scanner sign in</h1>
        <p>Use your gate account to start checking tickets.</p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit();
          }}
        >
          <label>
            <span>Email</span>
            <input
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              autoComplete="username"
              required
            />
          </label>
          <label>
            <span>Password</span>
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
              required
            />
          </label>
          {error && (
            <div className="scanner-inline-error" role="alert">
              <CircleAlert size={17} /> {error}
            </div>
          )}
          <button
            className="scanner-primary"
            type="submit"
            disabled={loading || !email.trim() || !password}
          >
            {loading ? (
              <>
                <LoaderCircle className="spin" size={18} /> Signing in…
              </>
            ) : (
              <>
                Sign in <ChevronRight size={18} />
              </>
            )}
          </button>
        </form>
      </section>
      <p className="scanner-copyright">© {new Date().getFullYear()} TktSync Scanner</p>
    </main>
  );
}

function EventPicker({
  events,
  loading,
  error,
  userLabel,
  onSelect,
  onRetry,
  onSignOut,
}: {
  events: ScannerEvent[];
  loading: boolean;
  error: boolean;
  userLabel: string;
  onSelect: (event: ScannerEvent) => void;
  onRetry: () => void;
  onSignOut: () => void;
}) {
  return (
    <div className="event-picker-page">
      <header className="picker-header">
        <Brand />
        <button type="button" onClick={onSignOut}>
          <LogOut size={17} /> Sign out
        </button>
      </header>
      <main className="event-picker">
        <span className="picker-icon">
          <TicketCheck size={24} />
        </span>
        <h1>Choose an event to scan</h1>
        <p>{userLabel}</p>
        {loading && (
          <div className="picker-loading">
            <LoaderCircle className="spin" /> Loading your events…
          </div>
        )}
        {error && (
          <div className="picker-message">
            <AlertTriangle size={23} />
            <strong>We couldn't load your events.</strong>
            <button className="scanner-primary" type="button" onClick={onRetry}>
              <RefreshCw size={17} /> Try again
            </button>
          </div>
        )}
        {!loading && !error && events.length === 0 && (
          <div className="picker-message">
            <TicketCheck size={23} />
            <strong>No scanning events are assigned to this account.</strong>
          </div>
        )}
        <div className="event-list">
          {events.map((event) => (
            <button type="button" key={event.id} onClick={() => onSelect(event)}>
              <span className="event-card-icon">
                <TicketCheck size={20} />
              </span>
              <span className="event-card-copy">
                <strong>{event.name}</strong>
                <span>{formatEventTime(event.starts_at)}</span>
                <span>
                  <MapPin size={13} /> {event.venue_name}
                </span>
              </span>
              <ChevronRight size={19} />
            </button>
          ))}
        </div>
      </main>
    </div>
  );
}

function RecentScans({ scans }: { scans: RecentScan[] }) {
  return scans.length ? (
    <ul className="recent-list">
      {scans.map((scan) => (
        <li key={scan.id}>
          <i className={scan.tone}>
            {scan.tone === 'success' ? <Check size={13} /> : <CircleAlert size={13} />}
          </i>
          <div>
            <strong>{scan.title}</strong>
            <span>{scan.detail}</span>
          </div>
          <time>{scan.time}</time>
        </li>
      ))}
    </ul>
  ) : (
    <div className="recent-empty">
      <Clock3 size={21} />
      <p>Scans from this session will appear here.</p>
    </div>
  );
}

export function ScannerPage() {
  const auth = useOperatorSession();
  const eventsQuery = useAuthorizedEvents(auth.token);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [selectedEvent, setSelectedEvent] = useState<ScannerEvent>();
  const [manualOpen, setManualOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [recentOpen, setRecentOpen] = useState(false);
  const scanner = useScanner(auth.token, selectedEvent);

  useEffect(() => {
    if (!eventsQuery.data || selectedEvent) return;
    const remembered = sessionStorage.getItem('tktsync.scanner.event');
    const match = eventsQuery.data.find((event) => event.id === remembered);
    if (match) setSelectedEvent(match);
  }, [eventsQuery.data, selectedEvent]);

  const chooseEvent = (event: ScannerEvent) => {
    sessionStorage.setItem('tktsync.scanner.event', event.id);
    setSelectedEvent(event);
  };
  const changeEvent = () => {
    scanner.stopCamera();
    scanner.reset();
    sessionStorage.removeItem('tktsync.scanner.event');
    setSettingsOpen(false);
    setSelectedEvent(undefined);
  };
  const signOut = async () => {
    scanner.stopCamera();
    sessionStorage.removeItem('tktsync.scanner.event');
    setSelectedEvent(undefined);
    setSettingsOpen(false);
    await auth.signOut();
  };

  const presentation = useMemo(
    () =>
      outcomePresentation(
        scanner.result,
        scanner.cannotVerify,
        selectedEvent?.name ?? 'this event',
      ),
    [scanner.cannotVerify, scanner.result, selectedEvent?.name],
  );
  const location = ticketLocation(scanner.result);

  if (auth.loading && !auth.authenticated) {
    return (
      <main className="scanner-boot" aria-live="polite">
        <Brand />
        <LoaderCircle className="spin" />
        <p>Opening scanner…</p>
      </main>
    );
  }
  if (!auth.authenticated) {
    return (
      <SignIn
        email={email}
        setEmail={setEmail}
        password={password}
        setPassword={setPassword}
        loading={auth.loading}
        error={auth.error}
        onSubmit={() => void auth.signIn(email, password)}
      />
    );
  }
  if (!selectedEvent) {
    return (
      <EventPicker
        events={eventsQuery.data ?? []}
        loading={eventsQuery.isPending}
        error={eventsQuery.isError}
        userLabel={auth.userLabel}
        onSelect={chooseEvent}
        onRetry={() => void eventsQuery.refetch()}
        onSignOut={() => void signOut()}
      />
    );
  }

  const decisionVisible = Boolean(scanner.result || scanner.cannotVerify);
  const cameraActive = scanner.cameraState === 'active';

  return (
    <div className="scanner-app">
      <header className="scanner-header">
        <div className="scanner-event-title">
          <strong>{selectedEvent.name}</strong>
          <span>{selectedEvent.venue_name}</span>
        </div>
        <div className="scanner-header-actions">
          <button
            type="button"
            aria-label="Enter code manually"
            onClick={() => setManualOpen(true)}
          >
            <Keyboard size={20} />
          </button>
          <button type="button" aria-label="Scanner settings" onClick={() => setSettingsOpen(true)}>
            <Settings size={20} />
          </button>
        </div>
      </header>

      <main className="scanner-main">
        <section className="scan-column">
          <div
            className={`scan-stage ${decisionVisible ? presentation.tone : ''}`}
            aria-live="assertive"
            aria-busy={scanner.busy}
          >
            <video ref={scanner.video} autoPlay playsInline muted />
            {!cameraActive && !decisionVisible && (
              <div className="camera-placeholder">
                <span>
                  <Camera size={28} />
                </span>
                <strong>
                  {scanner.cameraState === 'requesting'
                    ? 'Starting camera…'
                    : 'Point the camera at the ticket QR code'}
                </strong>
                <p>
                  {scanner.cameraMessage ||
                    'Open the camera, then hold the code steady inside the frame.'}
                </p>
                <button
                  className="scanner-primary"
                  type="button"
                  onClick={() => void scanner.startCamera()}
                  disabled={scanner.cameraState === 'requesting'}
                >
                  {scanner.cameraState === 'requesting' ? (
                    <>
                      <LoaderCircle className="spin" size={18} /> Starting…
                    </>
                  ) : (
                    <>
                      <Camera size={18} /> Open camera
                    </>
                  )}
                </button>
              </div>
            )}
            {cameraActive && !decisionVisible && !scanner.busy && (
              <div className="scan-guidance">
                <div className="scan-reticle">
                  <i />
                  <i />
                  <i />
                  <i />
                </div>
                <strong>Point the camera at the ticket QR code</strong>
                <span>Hold the code steady inside the frame</span>
              </div>
            )}
            {scanner.busy && (
              <div className="checking-overlay">
                <LoaderCircle className="spin" />
                <strong>Checking ticket…</strong>
                <span>Please hold</span>
              </div>
            )}
            {decisionVisible && (
              <div className="decision-overlay">
                <span className="decision-icon">
                  {presentation.tone === 'success' ? (
                    <Check />
                  ) : presentation.tone === 'warning' ? (
                    <Clock3 />
                  ) : (
                    <X />
                  )}
                </span>
                <h1>{presentation.title}</h1>
                {(presentation.tone === 'success'
                  ? location || presentation.description
                  : presentation.description) && (
                  <p>
                    {presentation.tone === 'success'
                      ? location || presentation.description
                      : presentation.description}
                  </p>
                )}
                {scanner.result?.admitted_at && presentation.tone === 'success' && (
                  <time>
                    {new Date(scanner.result.admitted_at).toLocaleTimeString([], {
                      hour: 'numeric',
                      minute: '2-digit',
                    })}
                  </time>
                )}
                <button type="button" onClick={scanner.reset}>
                  <RefreshCw size={18} /> Scan next ticket
                </button>
              </div>
            )}
          </div>

          <div className="scan-quick-actions">
            <button type="button" onClick={() => setManualOpen(true)}>
              <Keyboard size={19} />
              <span>Enter code manually</span>
            </button>
            <button
              className="mobile-recent-button"
              type="button"
              onClick={() => setRecentOpen(true)}
            >
              <Clock3 size={19} />
              <span>Recent scans</span>
            </button>
          </div>
        </section>

        <aside className="scanner-side">
          <div className="scanner-side-heading">
            <div>
              <h2>Recent scans</h2>
              <p>This session</p>
            </div>
            <Clock3 size={19} />
          </div>
          <RecentScans scans={scanner.recentScans} />
          <div className="connection-note">
            <span />
            <p>
              <strong>Ready for the gate</strong>Each ticket is checked before entry.
            </p>
          </div>
        </aside>
      </main>

      <Sheet open={manualOpen} onClose={() => setManualOpen(false)} title="Enter code manually">
        <form
          className="manual-form"
          onSubmit={(event) => {
            event.preventDefault();
            void scanner.submit(scanner.manual);
            setManualOpen(false);
          }}
        >
          <label htmlFor="ticket-code">Ticket code</label>
          <input
            id="ticket-code"
            value={scanner.manual}
            onChange={(event) => scanner.setManual(event.target.value)}
            autoComplete="off"
            spellCheck={false}
            placeholder="Enter ticket code"
          />
          <button
            className="scanner-primary"
            type="submit"
            disabled={scanner.busy || !scanner.manual.trim()}
          >
            {scanner.busy ? 'Checking ticket…' : 'Check ticket'}
          </button>
        </form>
      </Sheet>

      <Sheet open={settingsOpen} onClose={() => setSettingsOpen(false)} title="Scanner settings">
        <div className="settings-event">
          <span>
            <TicketCheck size={18} />
          </span>
          <div>
            <strong>{selectedEvent.name}</strong>
            <p>{selectedEvent.venue_name}</p>
          </div>
        </div>
        <button className="settings-action" type="button" onClick={changeEvent}>
          <RefreshCw size={18} />
          <span>
            <strong>Change event</strong>
            <small>Choose another assigned event</small>
          </span>
          <ChevronRight size={18} />
        </button>
        <label className="toggle-row">
          <Volume2 size={18} />
          <span>
            <strong>Sound feedback</strong>
            <small>Play a tone after each decision</small>
          </span>
          <input
            type="checkbox"
            checked={scanner.soundEnabled}
            onChange={(event) => scanner.setSoundEnabled(event.target.checked)}
          />
        </label>
        {'vibrate' in navigator && (
          <label className="toggle-row">
            <Vibrate size={18} />
            <span>
              <strong>Vibration feedback</strong>
              <small>Vibrate after each decision</small>
            </span>
            <input
              type="checkbox"
              checked={scanner.vibrationEnabled}
              onChange={(event) => scanner.setVibrationEnabled(event.target.checked)}
            />
          </label>
        )}
        {scanner.torchSupported && (
          <button
            className="settings-action"
            type="button"
            onClick={() => void scanner.toggleTorch()}
          >
            <Zap size={18} />
            <span>
              <strong>Torch</strong>
              <small>{scanner.torchEnabled ? 'On' : 'Off'}</small>
            </span>
            <span className={`switch-visual ${scanner.torchEnabled ? 'on' : ''}`} />
          </button>
        )}
        <div className="settings-account">
          <span>{auth.userLabel.slice(0, 1).toUpperCase()}</span>
          <div>
            <strong>{auth.userLabel}</strong>
            <small>Gate operator</small>
          </div>
        </div>
        <button className="signout-action" type="button" onClick={() => void signOut()}>
          <LogOut size={18} /> Sign out
        </button>
      </Sheet>

      <Sheet open={recentOpen} onClose={() => setRecentOpen(false)} title="Recent scans">
        <RecentScans scans={scanner.recentScans} />
      </Sheet>
    </div>
  );
}
