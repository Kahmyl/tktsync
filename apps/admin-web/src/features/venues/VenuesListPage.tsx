import { Building2, Plus, Search } from 'lucide-react';
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
} from '../../components/ui';
import { formatDate, humanName } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useIntentMutation, useVenues } from '../admin/queries';

export function VenuesListPage() {
  const venues = useVenues();
  const [query, setQuery] = useState('');
  const [open, setOpen] = useState(false);
  const createFormRef = useRef<HTMLFormElement>(null);
  const [name, setName] = useState('');
  const [address, setAddress] = useState('');
  const create = useIntentMutation({
    intent: (values: { name: string; address: string }) =>
      `venue:${values.name.trim()}:${values.address.trim()}`,
    mutationFn: (token, key, values) =>
      adminApi.createVenue(token, key, {
        name: values.name.trim(),
        address_text: values.address.trim() || undefined,
      }),
    invalidate: [adminKeys.venues],
  });
  const filtered = useMemo(
    () =>
      (venues.data ?? []).filter((venue) =>
        `${venue.name} ${venue.address_text ?? ''}`
          .toLowerCase()
          .includes(query.trim().toLowerCase()),
      ),
    [venues.data, query],
  );
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const form = createFormRef.current ? new FormData(createFormRef.current) : null;
    const values = {
      name: String(form?.get('name') || name),
      address: String(form?.get('address') || address),
    };
    if (!values.name.trim()) return;
    await create.mutateAsync(values);
    setOpen(false);
    setName('');
    setAddress('');
  };
  return (
    <>
      <PageHeader
        title="Venues"
        description="Manage places and the layout versions events use."
        actions={
          <Button onClick={() => setOpen(true)}>
            <Plus size={16} />
            Add venue
          </Button>
        }
      />
      <div className="filters">
        <label className="search-control">
          <Search size={16} />
          <Input
            aria-label="Search venues"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search venues"
          />
        </label>
      </div>
      <Panel>
        {venues.isLoading ? (
          <LoadingState />
        ) : venues.error ? (
          <ErrorState error={venues.error} onRetry={() => void venues.refetch()} />
        ) : filtered.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Venue</th>
                  <th>Address</th>
                  <th>Added</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {filtered.map((venue) => (
                  <tr key={venue.id}>
                    <td>
                      <Link className="record-link" to={`/venues/${venue.id}`}>
                        <strong>{humanName(venue.name, 'Untitled venue')}</strong>
                        <small>{venue.address_text ?? 'Address not provided'}</small>
                      </Link>
                    </td>
                    <td>{venue.address_text ?? 'Address not provided'}</td>
                    <td>{formatDate(venue.created_at)}</td>
                    <td className="align-right">
                      <Link className="text-link" to={`/venues/${venue.id}`}>
                        View venue
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            icon={<Building2 size={20} />}
            title="No venues yet"
            description="Add your first venue, then create a layout version."
            action={<Button onClick={() => setOpen(true)}>Add venue</Button>}
          />
        )}
      </Panel>
      <Dialog
        open={open}
        title="Add venue"
        description="Create the real venue record first. Layouts are managed from its detail page."
        onClose={() => setOpen(false)}
      >
        <form ref={createFormRef} onSubmit={(event) => void submit(event)}>
          <div className="dialog-body form-stack">
            <Field label="Venue name">
              <Input
                id="venue-name"
                name="name"
                required
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </Field>
            <Field label="Address">
              <Input
                id="venue-address"
                name="address"
                value={address}
                onChange={(event) => setAddress(event.target.value)}
              />
            </Field>
            {create.error ? <ErrorState error={create.error} /> : null}
          </div>
          <DialogActions>
            <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" busy={create.isPending}>
              Add venue
            </Button>
          </DialogActions>
        </form>
      </Dialog>
    </>
  );
}
