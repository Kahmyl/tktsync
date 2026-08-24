import { Plus, Search, Users } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  Field,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  StatusPill,
} from '../../components/ui';
import { formatNumber, humanName, timeAgo } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useIntentMutation, usePartners } from '../admin/queries';

export function PartnersListPage() {
  const partners = usePartners();
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const [name, setName] = useState('');
  const create = useIntentMutation({
    intent: () => `partner:${name.trim()}`,
    mutationFn: (token, key) => adminApi.createPartner(token, key, name.trim()),
    invalidate: [adminKeys.partners()],
  });
  const filtered = useMemo(
    () =>
      (partners.data?.items ?? []).filter((partner) =>
        partner.name.toLowerCase().includes(query.trim().toLowerCase()),
      ),
    [partners.data, query],
  );
  const submit = async () => {
    if (!name.trim()) return;
    await create.mutateAsync(undefined);
    setName('');
    setOpen(false);
  };
  return (
    <>
      <PageHeader
        title="Partners"
        description="Manage ticketing partners, credentials and event access."
        actions={
          <Button onClick={() => setOpen(true)}>
            <Plus size={16} />
            Add partner
          </Button>
        }
      />
      <div className="filters">
        <label className="search-control">
          <Search size={16} />
          <Input
            aria-label="Search partners"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search partners"
          />
        </label>
      </div>
      <Panel>
        {partners.isLoading ? (
          <LoadingState />
        ) : partners.error ? (
          <ErrorState error={partners.error} onRetry={() => void partners.refetch()} />
        ) : filtered.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Partner</th>
                  <th>Status</th>
                  <th className="align-right">Events</th>
                  <th className="align-right">Credentials</th>
                  <th className="align-right">Endpoints</th>
                  <th>Last activity</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((partner) => (
                  <tr key={partner.id}>
                    <td>
                      <Link className="record-link" to={`/partners/${partner.id}`}>
                        <strong>{humanName(partner.name, 'Untitled partner')}</strong>
                        <small>Ticketing partner</small>
                      </Link>
                    </td>
                    <td>
                      <StatusPill
                        label={partner.state === 'ACTIVE' ? 'Active' : 'Disabled'}
                        tone={partner.state === 'ACTIVE' ? 'positive' : 'critical'}
                      />
                    </td>
                    <td className="align-right num">{formatNumber(partner.active_event_count)}</td>
                    <td className="align-right num">
                      {formatNumber(partner.active_credential_count)}
                    </td>
                    <td className="align-right num">
                      {formatNumber(partner.active_endpoint_count)}
                    </td>
                    <td>{timeAgo(partner.last_activity_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            icon={<Users size={20} />}
            title="No partners yet"
            description="Add a ticketing partner to begin integrating."
            action={<Button onClick={() => setOpen(true)}>Add partner</Button>}
          />
        )}
      </Panel>
      <Dialog
        open={open}
        title="Add partner"
        description="Create a partner first, then grant event access and issue credentials deliberately."
        onClose={() => setOpen(false)}
      >
        <div className="dialog-body form-stack">
          <Field label="Partner name">
            <Input
              id="partner-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          {create.error ? <ErrorState error={create.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button busy={create.isPending} disabled={!name.trim()} onClick={() => void submit()}>
            Add partner
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
