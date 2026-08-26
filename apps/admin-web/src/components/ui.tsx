import {
  Children,
  cloneElement,
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactElement,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from 'react';
import { AlertCircle, AlertTriangle, Check, Copy, LoaderCircle } from 'lucide-react';
import { AdminApiError } from '../features/admin/api';
import { eventStateMeta, formatNumber } from '../lib/format';
import type { EventState } from '../features/admin/types';

export function Logo({
  showWordmark = true,
  size = 30,
}: {
  showWordmark?: boolean;
  size?: number;
}) {
  return (
    <span className="logo">
      <svg width={size} height={size} viewBox="0 0 32 32" fill="none" aria-hidden="true">
        <rect x="1" y="6" width="30" height="20" rx="5" className="logo-fill" />
        <path
          d="M12 6v3.2M12 14.2v3.6M12 22.8V26"
          className="logo-stroke"
          strokeWidth="1.6"
          strokeLinecap="round"
          opacity="0.85"
        />
        <path
          d="M25 14.4a5.2 5.2 0 0 0-9.4-1.7"
          className="logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
        />
        <path
          d="M15.6 17.6a5.2 5.2 0 0 0 9.4 1.7"
          className="logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
          opacity="0.6"
        />
        <path
          d="M25.4 11.4v3.2h-3.2"
          className="logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M15.2 20.6v-3.2h3.2"
          className="logo-stroke"
          strokeWidth="1.9"
          strokeLinecap="round"
          strokeLinejoin="round"
          opacity="0.6"
        />
      </svg>
      {showWordmark ? (
        <span className="logo-word">
          Tkt<span>Sync</span>
        </span>
      ) : null}
    </span>
  );
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'normal' | 'small' | 'icon';
  busy?: boolean;
};

export function Button({
  variant = 'primary',
  size = 'normal',
  busy,
  className = '',
  children,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      className={`button button-${variant} button-${size} ${className}`}
      disabled={disabled || busy}
      {...props}
    >
      {busy ? <LoaderCircle className="spin" size={16} aria-hidden="true" /> : null}
      {children}
    </button>
  );
}

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={`input ${props.className ?? ''}`} {...props} />;
}

export function Select(props: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={`input select ${props.className ?? ''}`} {...props} />;
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={`input textarea ${props.className ?? ''}`} {...props} />;
}

export function Field({
  label,
  error,
  hint,
  children,
}: {
  label: string;
  error?: string;
  hint?: string;
  children: ReactElement<{ id?: string }>;
}) {
  const id = children.props.id as string | undefined;
  return (
    <label className="field" htmlFor={id}>
      <span className="field-label">{label}</span>
      {cloneElement(children, { 'aria-invalid': Boolean(error) } as Record<string, unknown>)}
      {error ? (
        <span className="field-error">{error}</span>
      ) : hint ? (
        <span className="field-hint">{hint}</span>
      ) : null}
    </label>
  );
}

export function Panel({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <section className={`panel ${className}`}>{children}</section>;
}

export function PanelBody({
  children,
  className = '',
}: {
  children: ReactNode;
  className?: string;
}) {
  return <div className={`panel-body ${className}`}>{children}</div>;
}

export function SectionHeading({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="section-heading">
      <div>
        <h2>{title}</h2>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="section-actions">{actions}</div> : null}
    </div>
  );
}

export function PageHeader({
  title,
  description,
  actions,
  eyebrow,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  eyebrow?: ReactNode;
}) {
  return (
    <div className="page-header">
      <div>
        {eyebrow ? <div className="page-eyebrow">{eyebrow}</div> : null}
        <h1>{title}</h1>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="page-actions">{actions}</div> : null}
    </div>
  );
}

export function MetricCard({
  label,
  value,
  hint,
  icon,
}: {
  label: string;
  value: number | string;
  hint?: string;
  icon?: ReactNode;
}) {
  return (
    <div className="metric-card">
      <div className="metric-label">
        <span>{label}</span>
        {icon}
      </div>
      <p className="metric-value">{typeof value === 'number' ? formatNumber(value) : value}</p>
      {hint ? <p className="metric-hint">{hint}</p> : null}
    </div>
  );
}

export function ProgressBar({
  value,
  total,
  label,
}: {
  value: number;
  total: number;
  label?: string;
}) {
  const percent = total ? Math.min(100, Math.round((value / total) * 100)) : 0;
  return (
    <div className="progress-wrap">
      <div className="progress">
        <span style={{ width: `${percent}%` }} />
      </div>
      {label ? <small>{label}</small> : null}
    </div>
  );
}

export function StatusPill({ label, tone = 'neutral' }: { label: string; tone?: string }) {
  return (
    <span className={`status-pill status-${tone}`}>
      <i />
      {label}
    </span>
  );
}

export function EventStatus({ state }: { state: EventState }) {
  const meta = eventStateMeta[state];
  return <StatusPill label={meta.label} tone={meta.tone} />;
}

export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="empty-state">
      {icon ? <div className="empty-icon">{icon}</div> : null}
      <h3>{title}</h3>
      {description ? <p>{description}</p> : null}
      {action ? <div>{action}</div> : null}
    </div>
  );
}

export function LoadingState({ rows = 5 }: { rows?: number }) {
  return (
    <div className="loading-state" aria-label="Loading">
      {Array.from({ length: rows }).map((_, index) => (
        <span key={index} />
      ))}
    </div>
  );
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const apiError = error instanceof AdminApiError ? error : null;
  return (
    <div className="error-state" role="alert">
      <AlertCircle size={20} />
      <div>
        <strong>We couldn't load this view.</strong>
        <p>{error instanceof Error ? error.message : 'Try again in a moment.'}</p>
        {apiError?.requestId ? <small>Request ID: {apiError.requestId}</small> : null}
      </div>
      {onRetry ? (
        <Button variant="secondary" size="small" onClick={onRetry}>
          Try again
        </Button>
      ) : null}
    </div>
  );
}

export function InlineNotice({
  children,
  tone = 'info',
}: {
  children: ReactNode;
  tone?: 'info' | 'success' | 'warning' | 'error';
}) {
  return <div className={`inline-notice notice-${tone}`}>{children}</div>;
}

export function Dialog({
  open,
  title,
  description,
  children,
  onClose,
  className = '',
}: {
  open: boolean;
  title: string;
  description?: string;
  children: ReactNode;
  onClose: () => void;
  className?: string;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const dialogRef = useRef<HTMLElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  useEffect(() => {
    if (!open) return;
    const previousFocus =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focusableSelector =
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
    const focusDialog = window.requestAnimationFrame(() => {
      const preferred = dialogRef.current?.querySelector<HTMLElement>('[data-autofocus]');
      const first = dialogRef.current?.querySelector<HTMLElement>(focusableSelector);
      (preferred ?? first ?? dialogRef.current)?.focus();
    });
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCloseRef.current();
        return;
      }
      if (event.key !== 'Tab' || !dialogRef.current) return;
      const focusable = Array.from(
        dialogRef.current.querySelectorAll<HTMLElement>(focusableSelector),
      );
      if (!focusable.length) {
        event.preventDefault();
        dialogRef.current.focus();
        return;
      }
      const first = focusable[0]!;
      const last = focusable[focusable.length - 1]!;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener('keydown', handleKey);
    return () => {
      window.cancelAnimationFrame(focusDialog);
      window.removeEventListener('keydown', handleKey);
      previousFocus?.focus();
    };
  }, [open]);
  if (!open) return null;
  return (
    <div
      className="dialog-backdrop"
      role="presentation"
      onMouseDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <section
        ref={dialogRef}
        className={`dialog ${className}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
      >
        <div className="dialog-header">
          <h2 id={titleId}>{title}</h2>
          {description ? <p id={descriptionId}>{description}</p> : null}
        </div>
        {children}
      </section>
    </div>
  );
}

export function DialogActions({ children }: { children: ReactNode }) {
  return <div className="dialog-actions">{Children.toArray(children)}</div>;
}

export function ConfirmationDialog({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel = 'Cancel',
  tone = 'warning',
  busy = false,
  children,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description: string;
  confirmLabel: string;
  cancelLabel?: string;
  tone?: 'warning' | 'danger';
  busy?: boolean;
  children?: ReactNode;
  onConfirm: () => void | Promise<void>;
  onCancel: () => void;
}) {
  const close = () => {
    if (!busy) onCancel();
  };
  return (
    <Dialog
      open={open}
      title={title}
      description={description}
      onClose={close}
      className={`confirmation-dialog confirmation-${tone}`}
    >
      <div className="dialog-body confirmation-body">
        <span className="confirmation-icon" aria-hidden="true">
          <AlertTriangle size={20} />
        </span>
        <div>
          <strong>
            {tone === 'danger' ? 'This action has an immediate effect' : 'Review before continuing'}
          </strong>
          {children ? <div className="confirmation-detail">{children}</div> : null}
        </div>
      </div>
      <DialogActions>
        <Button variant="secondary" data-autofocus onClick={close} disabled={busy}>
          {cancelLabel}
        </Button>
        <Button
          variant={tone === 'danger' ? 'danger' : 'primary'}
          busy={busy}
          onClick={() => void onConfirm()}
        >
          {confirmLabel}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

export function SecretDialog({
  open,
  title,
  description,
  secret,
  onClose,
}: {
  open: boolean;
  title: string;
  description: string;
  secret: string;
  onClose: () => void;
}) {
  const [copied, setCopied] = useCopyFeedback(open);
  return (
    <Dialog
      open={open}
      title={title}
      description={description}
      onClose={onClose}
      className="secret-dialog"
    >
      <div className="dialog-body">
        <InlineNotice tone="warning">
          This value is shown once. Store it in the partner's secret manager before closing.
        </InlineNotice>
        <div className="secret-value" data-testid="one-time-secret">
          {secret}
        </div>
        <Button
          type="button"
          variant="secondary"
          onClick={() => {
            void navigator.clipboard.writeText(secret);
            setCopied(true);
          }}
        >
          {copied ? <Check size={16} /> : <Copy size={16} />}
          {copied ? 'Copied' : 'Copy secret'}
        </Button>
      </div>
      <DialogActions>
        <Button type="button" onClick={onClose}>
          I have stored it
        </Button>
      </DialogActions>
    </Dialog>
  );
}

function useCopyFeedback(reset: boolean): [boolean, (value: boolean) => void] {
  const [state, setState] = useState(false);
  useEffect(() => setState(false), [reset, setState]);
  return [state, setState];
}
