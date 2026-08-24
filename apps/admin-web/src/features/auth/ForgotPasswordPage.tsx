import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useOperator } from '../../auth/OperatorSession';
import { Button, Field, InlineNotice, Input, Logo } from '../../components/ui';

export function ForgotPasswordPage() {
  const auth = useOperator();
  const [email, setEmail] = useState('');
  const [validation, setValidation] = useState('');
  const [sent, setSent] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!email.includes('@')) {
      setValidation('Enter a valid email address.');
      return;
    }
    setValidation('');
    if (await auth.sendPasswordReset(email)) setSent(true);
  };

  return (
    <div className="sign-in-shell">
      <aside className="sign-in-brand">
        <Logo size={32} />
        <div>
          <h1>Recover your administrator account securely.</h1>
          <p>We’ll send a time-limited link to the email address associated with your account.</p>
        </div>
        <small>© 2026 TktSync</small>
      </aside>
      <main className="sign-in-form">
        <div className="sign-in-card">
          <div className="mobile-sign-in-logo">
            <Logo size={32} />
          </div>
          <h1>Reset your password</h1>
          <p>Enter your administrator email address.</p>
          {sent ? (
            <div className="form-stack">
              <InlineNotice tone="success">
                If an account exists for <strong>{email.trim()}</strong>, a password-reset link has
                been sent.
              </InlineNotice>
              <Link className="text-link" to="/sign-in">
                Return to sign in
              </Link>
            </div>
          ) : (
            <form onSubmit={(event) => void submit(event)} noValidate>
              <Field label="Email" error={validation}>
                <Input
                  id="recovery-email"
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                />
              </Field>
              {auth.error ? <InlineNotice tone="error">{auth.error}</InlineNotice> : null}
              <Button type="submit" busy={auth.loading} className="sign-in-button">
                Send reset link
              </Button>
              <Link className="text-link centered-link" to="/sign-in">
                Return to sign in
              </Link>
            </form>
          )}
        </div>
      </main>
    </div>
  );
}
