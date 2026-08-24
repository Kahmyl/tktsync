import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useOperator } from '../../auth/OperatorSession';
import { Button, Field, InlineNotice, Input, LoadingState, Logo } from '../../components/ui';
import { minimumPasswordLength, passwordValidation } from './password';

export function SetPasswordPage() {
  const auth = useOperator();
  const navigate = useNavigate();
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [validation, setValidation] = useState('');
  const [saved, setSaved] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const error = passwordValidation(password, confirmation);
    setValidation(error);
    if (error) return;
    if (await auth.updatePassword(password)) setSaved(true);
  };

  return (
    <div className="sign-in-shell">
      <aside className="sign-in-brand">
        <Logo size={32} />
        <div>
          <h1>Protect your TktSync administrator account.</h1>
          <p>Create a strong password you can use whenever you return to TktSync.</p>
        </div>
        <small>© 2026 TktSync</small>
      </aside>
      <main className="sign-in-form">
        <div className="sign-in-card">
          <div className="mobile-sign-in-logo">
            <Logo size={32} />
          </div>
          {auth.loading && !auth.authenticated ? (
            <LoadingState rows={3} />
          ) : !auth.authenticated ? (
            <div className="form-stack">
              <h1>Link expired or invalid</h1>
              <p>Request a new secure link to continue.</p>
              <Link className="button button-primary button-normal" to="/forgot-password">
                Request another link
              </Link>
            </div>
          ) : saved ? (
            <div className="form-stack">
              <h1>Password created</h1>
              <InlineNotice tone="success">
                Your password is ready. You can now sign out and return using your email and this
                password.
              </InlineNotice>
              <Button onClick={() => navigate('/', { replace: true })}>Continue to TktSync</Button>
            </div>
          ) : (
            <>
              <h1>Create your password</h1>
              <p>Use at least {minimumPasswordLength} characters. A long passphrase works well.</p>
              <form onSubmit={(event) => void submit(event)} noValidate>
                <Field label="New password">
                  <Input
                    id="new-password"
                    type="password"
                    autoComplete="new-password"
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                  />
                </Field>
                <Field label="Confirm password" error={validation}>
                  <Input
                    id="confirm-password"
                    type="password"
                    autoComplete="new-password"
                    value={confirmation}
                    onChange={(event) => setConfirmation(event.target.value)}
                  />
                </Field>
                {auth.error ? <InlineNotice tone="error">{auth.error}</InlineNotice> : null}
                <Button type="submit" busy={auth.loading} className="sign-in-button">
                  Save password
                </Button>
              </form>
            </>
          )}
        </div>
      </main>
    </div>
  );
}
