import type { ButtonHTMLAttributes, PropsWithChildren, ReactNode } from 'react';

export function ProductShell({
  product,
  eyebrow,
  navigation,
  actions,
  children,
}: PropsWithChildren<{
  product: string;
  eyebrow: string;
  navigation?: ReactNode;
  actions?: ReactNode;
}>) {
  return (
    <div className="product-shell">
      <header className="topbar">
        <a className="brand" href="/" aria-label={`${product} home`}>
          <span className="brand-mark">T</span>
          <span>
            <small>{eyebrow}</small>
            {product}
          </span>
        </a>
        <div className="top-actions">{actions}</div>
      </header>
      {navigation}
      <main className="product-main">{children}</main>
    </div>
  );
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <div className="page-header">
      <div>
        <p className="eyebrow">TktSync operations</p>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </div>
  );
}

export function Panel({
  title,
  description,
  children,
  className = '',
}: PropsWithChildren<{ title?: string; description?: string; className?: string }>) {
  return (
    <section className={`panel ${className}`}>
      {title && (
        <div className="panel-heading">
          <h2>{title}</h2>
          {description && <p>{description}</p>}
        </div>
      )}
      {children}
    </section>
  );
}

export function Button({ className = '', ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button className={`button ${className}`} {...props} />;
}
export function StatusPill({
  tone = 'neutral',
  children,
}: PropsWithChildren<{ tone?: 'neutral' | 'success' | 'danger' | 'warning' | 'info' }>) {
  return (
    <span className={`status-pill ${tone}`}>
      <i />
      {children}
    </span>
  );
}
export function Metric({
  label,
  value,
  detail,
}: {
  label: string;
  value: string;
  detail?: string;
}) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
      {detail && <small>{detail}</small>}
    </div>
  );
}
export function FormField({
  label,
  hint,
  children,
}: PropsWithChildren<{ label: string; hint?: string }>) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
      {hint && <small>{hint}</small>}
    </label>
  );
}
export function InlineNotice({
  tone = 'info',
  title,
  children,
}: PropsWithChildren<{ tone?: 'info' | 'danger' | 'success' | 'warning'; title: string }>) {
  return (
    <div className={`notice ${tone}`}>
      <strong>{title}</strong>
      <div>{children}</div>
    </div>
  );
}
