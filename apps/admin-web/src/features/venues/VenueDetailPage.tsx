import { Edit3, Layers3, Plus, Send } from 'lucide-react';
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
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
  PanelBody,
  SectionHeading,
  Select,
  StatusPill,
} from '../../components/ui';
import { formatDateTime } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useIntentMutation, useVenue } from '../admin/queries';

type DraftSection = { object_key: string; name: string; kind: 'RESERVED' | 'GA' };

export function VenueDetailPage() {
  const { venueId = '' } = useParams();
  const { venue, layouts } = useVenue(venueId);
  const [editLayoutId, setEditLayoutId] = useState('');
  const [sectionName, setSectionName] = useState('');
  const [sectionKind, setSectionKind] = useState<'RESERVED' | 'GA'>('RESERVED');
  const [sections, setSections] = useState<DraftSection[]>([]);
  const [publishId, setPublishId] = useState('');
  const invalidate = [adminKeys.layouts(venueId), adminKeys.venue(venueId), adminKeys.venues];
  const create = useIntentMutation({
    intent: () => `${venueId}:new-layout`,
    mutationFn: (token, key) => adminApi.createLayout(token, key, venueId),
    invalidate,
  });
  const replace = useIntentMutation({
    intent: () => `${editLayoutId}:replace:${JSON.stringify(sections)}`,
    mutationFn: (token, key) => adminApi.replaceLayout(token, key, editLayoutId, sections),
    invalidate,
  });
  const publish = useIntentMutation({
    intent: () => `${publishId}:publish`,
    mutationFn: (token, key) => adminApi.publishLayout(token, key, publishId),
    invalidate,
  });

  if (venue.isLoading || layouts.isLoading) return <LoadingState rows={7} />;
  if (venue.error || layouts.error || !venue.data)
    return (
      <ErrorState
        error={venue.error ?? layouts.error}
        onRetry={() => {
          void venue.refetch();
          void layouts.refetch();
        }}
      />
    );
  const openEditor = (id: string) => {
    setEditLayoutId(id);
    setSections([]);
    setSectionName('');
  };
  const addSection = () => {
    if (!sectionName.trim()) return;
    const objectKey =
      sectionName
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-|-$/g, '') || `section-${sections.length + 1}`;
    setSections((items) => [
      ...items,
      {
        object_key: `${objectKey}-${items.length + 1}`,
        name: sectionName.trim(),
        kind: sectionKind,
      },
    ]);
    setSectionName('');
  };
  const saveDraft = async () => {
    await replace.mutateAsync(undefined);
    setEditLayoutId('');
    setSections([]);
  };
  const publishConfirmed = async () => {
    await publish.mutateAsync(undefined);
    setPublishId('');
  };

  return (
    <>
      <PageHeader
        title={venue.data.name}
        description={venue.data.address_text ?? 'No address provided'}
        actions={
          <Link className="button button-secondary button-normal" to="/venues">
            All venues
          </Link>
        }
      />
      <div className="metric-grid three">
        <div className="metric-card">
          <div className="metric-label">Layout versions</div>
          <p className="metric-value">{layouts.data?.length ?? 0}</p>
          <p className="metric-hint">Versioned and immutable after publish</p>
        </div>
        <div className="metric-card">
          <div className="metric-label">Published</div>
          <p className="metric-value">
            {layouts.data?.filter((layout) => layout.state === 'PUBLISHED').length ?? 0}
          </p>
          <p className="metric-hint">Available to materialize</p>
        </div>
        <div className="metric-card">
          <div className="metric-label">Drafts</div>
          <p className="metric-value">
            {layouts.data?.filter((layout) => layout.state === 'DRAFT').length ?? 0}
          </p>
          <p className="metric-hint">Editable layout versions</p>
        </div>
      </div>
      <Panel>
        <SectionHeading
          title="Layout versions"
          description="Build a structured draft, then publish it for event use."
          actions={
            <Button
              size="small"
              busy={create.isPending}
              onClick={() => void create.mutateAsync(undefined)}
            >
              <Plus size={15} />
              New layout version
            </Button>
          }
        />
        <div className="panel-divider" />
        {create.error ? (
          <PanelBody>
            <ErrorState error={create.error} />
          </PanelBody>
        ) : null}
        {layouts.data?.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Version</th>
                  <th>Status</th>
                  <th>Created</th>
                  <th>Published</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {[...layouts.data]
                  .sort((a, b) => b.version_number - a.version_number)
                  .map((layout) => (
                    <tr key={layout.id}>
                      <td>
                        <strong>Version {layout.version_number}</strong>
                        <small className="table-subline num">{layout.id}</small>
                      </td>
                      <td>
                        <StatusPill
                          label={
                            layout.state === 'PUBLISHED'
                              ? 'Published'
                              : layout.state === 'DRAFT'
                                ? 'Draft'
                                : 'Retired'
                          }
                          tone={
                            layout.state === 'PUBLISHED'
                              ? 'positive'
                              : layout.state === 'DRAFT'
                                ? 'warning'
                                : 'neutral'
                          }
                        />
                      </td>
                      <td>{formatDateTime(layout.created_at)}</td>
                      <td>{formatDateTime(layout.published_at)}</td>
                      <td className="align-right">
                        <div className="table-actions">
                          {layout.state === 'DRAFT' ? (
                            <>
                              <Button
                                variant="secondary"
                                size="small"
                                onClick={() => openEditor(layout.id)}
                              >
                                <Edit3 size={14} />
                                Edit draft
                              </Button>
                              <Button size="small" onClick={() => setPublishId(layout.id)}>
                                <Send size={14} />
                                Publish
                              </Button>
                            </>
                          ) : null}
                        </div>
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyState
            icon={<Layers3 size={20} />}
            title="No layout versions"
            description="Create a draft layout version to define sections."
            action={
              <Button onClick={() => void create.mutateAsync(undefined)}>
                Create layout version
              </Button>
            }
          />
        )}
      </Panel>
      <Dialog
        open={Boolean(editLayoutId)}
        title="Edit draft layout"
        description="Define the supported structured sections. No storage JSON is exposed."
        onClose={() => setEditLayoutId('')}
        className="wide-dialog"
      >
        <div className="dialog-body form-stack">
          <InlineNotice>
            Saving replaces the current draft structure. Published versions cannot be edited.
          </InlineNotice>
          <div className="inline-form">
            <Field label="Section name">
              <Input
                id="section-name"
                value={sectionName}
                onChange={(event) => setSectionName(event.target.value)}
                placeholder="e.g. Main floor"
              />
            </Field>
            <Field label="Seating type">
              <Select
                id="section-kind"
                value={sectionKind}
                onChange={(event) => setSectionKind(event.target.value as 'RESERVED' | 'GA')}
              >
                <option value="RESERVED">Reserved seats</option>
                <option value="GA">General admission</option>
              </Select>
            </Field>
            <Button
              type="button"
              variant="secondary"
              onClick={addSection}
              disabled={!sectionName.trim()}
            >
              Add section
            </Button>
          </div>
          {sections.length ? (
            <ul className="draft-sections">
              {sections.map((section, index) => (
                <li key={section.object_key}>
                  <div>
                    <strong>{section.name}</strong>
                    <small>{section.kind === 'GA' ? 'General admission' : 'Reserved seats'}</small>
                  </div>
                  <button
                    type="button"
                    onClick={() =>
                      setSections((items) => items.filter((_, itemIndex) => itemIndex !== index))
                    }
                  >
                    Remove
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState
              title="No sections in this draft"
              description="Add each supported seating area before saving."
            />
          )}
          {replace.error ? <ErrorState error={replace.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setEditLayoutId('')}>
            Cancel
          </Button>
          <Button
            busy={replace.isPending}
            disabled={!sections.length}
            onClick={() => void saveDraft()}
          >
            Save draft layout
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={Boolean(publishId)}
        title="Publish layout"
        description="Publishing makes this immutable version available for event materialization."
        onClose={() => setPublishId('')}
      >
        <div className="dialog-body">
          <InlineNotice tone="warning">
            Review the structured draft first. Publishing cannot be undone.
          </InlineNotice>
          {publish.error ? <ErrorState error={publish.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setPublishId('')}>
            Keep draft
          </Button>
          <Button busy={publish.isPending} onClick={() => void publishConfirmed()}>
            Publish layout
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
