import { Plus, Search, ShieldCheck } from 'lucide-react';
import { useState } from 'react';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  Field,
  InlineNotice,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  Select,
  StatusPill,
} from '../../components/ui';
import { timeAgo } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useIntentMutation, usePlatformAdmins } from '../admin/queries';
import type { PlatformAdminUser } from '../admin/types';
import { isEmailAddress } from './validation';

export function UsersPage() {
  const [query, setQuery] = useState('');
  const [state, setState] = useState('');
  const [addOpen, setAddOpen] = useState(false);
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [selected, setSelected] = useState<PlatformAdminUser | null>(null);
  const [reason, setReason] = useState('');
  const users = usePlatformAdmins(query, state);

  const create = useIntentMutation({
    intent: (input: { email: string; display_name: string }) =>
      `platform-admin:create:${input.email}`,
    mutationFn: (token, key, input: { email: string; display_name: string }) =>
      adminApi.createPlatformAdmin(token, key, input),
    invalidate: [adminKeys.usersRoot],
  });
  const changeState = useIntentMutation({
    intent: (input: { user: PlatformAdminUser; enabled: boolean; reason: string }) =>
      `platform-admin:${input.user.id}:${input.enabled}:${input.reason}`,
    mutationFn: (
      token,
      key,
      input: { user: PlatformAdminUser; enabled: boolean; reason: string },
    ) => adminApi.setPlatformAdminState(token, key, input.user.id, input.enabled, input.reason),
    invalidate: [adminKeys.usersRoot],
  });

  const closeAdd = () => {
    if (create.isPending) return;
    setAddOpen(false);
    create.reset();
  };
  const submitAdd = async () => {
    const input = { email: email.trim().toLowerCase(), display_name: displayName.trim() };
    if (!input.display_name || !isEmailAddress(input.email)) return;
    const created = await create.mutateAsync(input);
    setConfirmation(
      created.invitation_sent
        ? `Invitation sent to ${input.email}. They can use the email link to finish setting up their account.`
        : `${input.display_name} now has administrator access.`,
    );
    setDisplayName('');
    setEmail('');
    setAddOpen(false);
  };
  const closeStateDialog = () => {
    if (changeState.isPending) return;
    setSelected(null);
    setReason('');
    changeState.reset();
  };
  const submitStateChange = async () => {
    if (!selected || !reason.trim()) return;
    await changeState.mutateAsync({
      user: selected,
      enabled: selected.state === 'DISABLED',
      reason: reason.trim(),
    });
    closeStateDialog();
  };

  return (
    <>
      <PageHeader
        title="Administrators"
        description="Control who has full Platform Admin access to TktSync."
        actions={
          <Button onClick={() => setAddOpen(true)}>
            <Plus size={16} />
            Add administrator
          </Button>
        }
      />

      {confirmation ? (
        <InlineNotice tone="success">
          <strong>Administrator added.</strong> {confirmation}
        </InlineNotice>
      ) : null}

      <div className="filters admin-user-filters">
        <label className="search-control">
          <Search size={16} />
          <Input
            aria-label="Search administrators"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search administrators"
          />
        </label>
        <Select
          aria-label="Filter administrator status"
          value={state}
          onChange={(event) => setState(event.target.value)}
        >
          <option value="">All statuses</option>
          <option value="ACTIVE">Active</option>
          <option value="DISABLED">Disabled</option>
        </Select>
      </div>

      <Panel>
        {users.isLoading ? (
          <LoadingState />
        ) : users.error ? (
          <ErrorState error={users.error} onRetry={() => void users.refetch()} />
        ) : users.data?.items.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Administrator</th>
                  <th>Email</th>
                  <th>Status</th>
                  <th>Added</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {users.data.items.map((user) => (
                  <tr key={user.id}>
                    <td>
                      <strong>{user.display_name || 'Unnamed administrator'}</strong>
                      <small className="table-subline">
                        Platform Admin{user.is_current_user ? ' · You' : ''}
                      </small>
                    </td>
                    <td>{user.email || 'Seeded administrator'}</td>
                    <td>
                      <StatusPill
                        label={user.state === 'ACTIVE' ? 'Active' : 'Disabled'}
                        tone={user.state === 'ACTIVE' ? 'positive' : 'critical'}
                      />
                    </td>
                    <td>{timeAgo(user.created_at)}</td>
                    <td className="align-right">
                      <Button
                        variant="secondary"
                        size="small"
                        disabled={user.is_current_user}
                        title={
                          user.is_current_user ? 'You cannot disable your own access' : undefined
                        }
                        onClick={() => {
                          setSelected(user);
                          setReason('');
                        }}
                      >
                        {user.state === 'ACTIVE' ? 'Disable' : 'Enable'}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            icon={<ShieldCheck size={20} />}
            title="No administrators match"
            description="Clear the filters or add a Platform Admin."
            action={<Button onClick={() => setAddOpen(true)}>Add administrator</Button>}
          />
        )}
      </Panel>

      <Dialog
        open={addOpen}
        title="Add administrator"
        description="Invite a trusted person to become a TktSync administrator."
        onClose={closeAdd}
      >
        <div className="dialog-body form-stack">
          <InlineNotice tone="warning">
            Platform Admins can access every event and perform privileged operations. Grant this
            role only to trusted operators.
          </InlineNotice>
          <Field label="Display name">
            <Input
              id="admin-display-name"
              autoComplete="name"
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              placeholder="e.g. Ada Okafor"
            />
          </Field>
          <Field
            label="Work email"
            hint="We’ll send an invitation if they do not already have an account."
            error={email && !isEmailAddress(email) ? 'Enter a valid email address.' : undefined}
          >
            <Input
              id="admin-email"
              type="email"
              autoComplete="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              placeholder="ada@example.com"
            />
          </Field>
          {create.error ? <ErrorState error={create.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={closeAdd}>
            Cancel
          </Button>
          <Button
            busy={create.isPending}
            disabled={!displayName.trim() || !isEmailAddress(email)}
            onClick={() => void submitAdd()}
          >
            Invite administrator
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={Boolean(selected)}
        title={`${selected?.state === 'ACTIVE' ? 'Disable' : 'Enable'} administrator`}
        description={
          selected?.state === 'ACTIVE'
            ? `Immediately revoke TktSync access for ${selected.display_name || 'this administrator'}.`
            : `Restore TktSync access for ${selected?.display_name || 'this administrator'}.`
        }
        onClose={closeStateDialog}
      >
        <div className="dialog-body form-stack">
          <Field label="Reason" hint="This reason is recorded in the append-only audit history.">
            <Input
              id="admin-state-reason"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder="Why is this access changing?"
            />
          </Field>
          {changeState.error ? <ErrorState error={changeState.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={closeStateDialog}>
            Cancel
          </Button>
          <Button
            variant={selected?.state === 'ACTIVE' ? 'danger' : 'primary'}
            busy={changeState.isPending}
            disabled={!reason.trim()}
            onClick={() => void submitStateChange()}
          >
            {selected?.state === 'ACTIVE' ? 'Disable access' : 'Enable access'}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
