import { useState, type FormEvent } from 'react';
import { Link, Navigate, useNavigate } from 'react-router-dom';
import { useOperator } from '../../auth/OperatorSession';
import { Button, Field, InlineNotice, Input, Logo } from '../../components/ui';

export function SignInPage() {
  const auth = useOperator();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [validation, setValidation] = useState<{ email?: string; password?: string }>({});
  if (!auth.loading && auth.authenticated)
    return <Navigate to={auth.requiresPasswordSetup ? '/set-password' : '/'} replace />;
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const errors: typeof validation = {};
    if (!email.includes('@')) errors.email = 'Enter a valid email address.';
    if (!password) errors.password = 'Enter your password to continue.';
    setValidation(errors);
    if (Object.keys(errors).length) return;
    if (await auth.signIn(email, password)) navigate('/', { replace: true });
  };
  return (
    <div className="sign-in-shell">
      <aside className="sign-in-brand">
        <Logo size={32} />
        <div>
          <h1>Everything your box office needs, in one calm place.</h1>
          <p>
            Plan events, control inventory, work with ticketing partners and follow entry activity
            in real time.
          </p>
        </div>
        <small>© 2026 TktSync</small>
      </aside>
      <main className="sign-in-form">
        <div className="sign-in-card">
          <div className="mobile-sign-in-logo">
            <Logo size={32} />
          </div>
          <h1>Welcome back</h1>
          <p>Sign in to manage events, partners and ticketing operations.</p>
          <form onSubmit={(event) => void submit(event)} noValidate>
            <Field label="Email" error={validation.email}>
              <Input
                id="email"
                type="email"
                autoComplete="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
              />
            </Field>
            <Field label="Password" error={validation.password}>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </Field>
            <Link className="text-link password-recovery-link" to="/forgot-password">
              Forgot password?
            </Link>
            {auth.error ? <InlineNotice tone="error">{auth.error}</InlineNotice> : null}
            <Button type="submit" busy={auth.loading} className="sign-in-button">
              Sign in
            </Button>
          </form>
        </div>
      </main>
    </div>
  );
}
