import { Plus, Search, Users } from 'lucide-react';
import { useMemo, useRef, useState, type FormEvent } from 'react';
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
  const createFormRef = useRef<HTMLFormElement>(null);
  const [name, setName] = useState('');
  const create = useIntentMutation({
    intent: (value: string) => `partner:${value.trim()}`,
    mutationFn: (token, key, value) => adminApi.createPartner(token, key, value.trim()),
    invalidate: [adminKeys.partners()],
  });
  const filtered = useMemo(
    () =>
      (partners.data?.items ?? []).filter((partner) =>
        partner.name.toLowerCase().includes(query.trim().toLowerCase()),
      ),
    [partners.data, query],
  );
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = createFormRef.current ? new FormData(createFormRef.current) : null;
    const value = String(form?.get('name') || name);
    if (!value.trim()) return;
    await create.mutateAsync(value);
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
        <form ref={createFormRef} onSubmit={(event) => void submit(event)}>
          <div className="dialog-body form-stack">
            <Field label="Partner name">
              <Input
                id="partner-name"
                name="name"
                required
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </Field>
            {create.error ? <ErrorState error={create.error} /> : null}
          </div>
          <DialogActions>
            <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" busy={create.isPending}>
              Add partner
            </Button>
          </DialogActions>
        </form>
      </Dialog>
    </>
  );
}
