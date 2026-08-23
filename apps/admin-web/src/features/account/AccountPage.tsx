import { LogOut } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { useOperator } from '../../auth/OperatorSession';
import {
  Button,
  Field,
  Input,
  PageHeader,
  Panel,
  PanelBody,
  SectionHeading,
} from '../../components/ui';
import { initials } from '../../lib/format';

export function AccountPage() {
  const { user, signOut } = useOperator();
  const navigate = useNavigate();
  const logout = async () => {
    await signOut();
    navigate('/sign-in', { replace: true });
  };
  return (
    <>
      <PageHeader title="Account" description="Your authenticated TktSync operator session." />
      <div className="account-grid">
        <Panel>
          <SectionHeading
            title="Profile"
            description="Values supplied by the active Supabase session"
          />
          <div className="panel-divider" />
          <PanelBody className="form-stack">
            <div className="profile-summary">
              <span className="avatar large">{initials(user?.displayName ?? 'Operator')}</span>
              <div>
                <strong>{user?.displayName ?? 'Operator'}</strong>
                <small>Authenticated operator</small>
              </div>
            </div>
            <div className="form-grid two">
              <Field label="Display name">
                <Input id="account-name" value={user?.displayName ?? ''} readOnly />
              </Field>
              <Field label="Email">
                <Input id="account-email" type="email" value={user?.email ?? ''} readOnly />
              </Field>
            </div>
            <p className="field-hint">
              Profile editing is not exposed by the current authoritative account contract.
            </p>
          </PanelBody>
        </Panel>
        <Panel className="session-panel">
          <SectionHeading title="Session" />
          <div className="panel-divider" />
          <PanelBody className="form-stack">
            <p className="muted-copy">
              You're signed in as <strong>{user?.email}</strong>.
            </p>
            <Button variant="secondary" onClick={() => void logout()}>
              <LogOut size={16} />
              Sign out
            </Button>
          </PanelBody>
        </Panel>
      </div>
    </>
  );
}
