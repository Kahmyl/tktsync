import { Edit3, Layers3, Plus, Send } from 'lucide-react';
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useOperator } from '../../auth/OperatorSession';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  InlineNotice,
  LoadingState,
  PageHeader,
  Panel,
  PanelBody,
  SectionHeading,
  StatusPill,
} from '../../components/ui';
import { formatDateTime, humanName } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useIntentMutation, useVenue } from '../admin/queries';
import type { ReplaceLayoutBody, VenueLayoutDetail } from '../admin/types';
import { LayoutBuilder } from './layout-builder/LayoutBuilder';

export function VenueDetailPage() {
  const { venueId = '' } = useParams();
  const auth = useOperator();
  const { venue, layouts } = useVenue(venueId);
  const [editLayout, setEditLayout] = useState<VenueLayoutDetail | null>(null);
  const [editorLoading, setEditorLoading] = useState(false);
  const [publishId, setPublishId] = useState('');
  const invalidate = [adminKeys.layouts(venueId), adminKeys.venue(venueId), adminKeys.venues];
  const create = useIntentMutation({
    intent: () => `${venueId}:new-layout`,
    mutationFn: (token, key) => adminApi.createLayout(token, key, venueId),
    invalidate,
  });
  const replace = useIntentMutation({
    intent: (body: ReplaceLayoutBody) => `${editLayout?.id}:replace:${JSON.stringify(body)}`,
    mutationFn: (token, key, body) =>
      adminApi.replaceLayout(token, key, editLayout?.id ?? '', body),
    invalidate,
  });
  const publish = useIntentMutation({
    intent: (layoutId: string) => `${layoutId}:publish`,
    mutationFn: (token, key, layoutId) => adminApi.publishLayout(token, key, layoutId),
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
  const openEditor = async (id: string) => {
    setEditorLoading(true);
    try {
      setEditLayout(await adminApi.layout(auth.token, id));
    } finally {
      setEditorLoading(false);
    }
  };
  const publishConfirmed = async () => {
    await publish.mutateAsync(publishId);
    setPublishId('');
  };

  if (editLayout && venue.data)
    return (
      <LayoutBuilder
        venueName={humanName(venue.data.name, 'Untitled venue')}
        layout={editLayout}
        saving={replace.isPending}
        publishing={publish.isPending}
        error={replace.error ?? publish.error}
        onClose={() => setEditLayout(null)}
        onSave={async (body) => {
          await replace.mutateAsync(body);
        }}
        onPublish={async (body) => {
          if (!window.confirm('Publish this layout? Published layouts cannot be edited.')) return;
          await replace.mutateAsync(body);
          await publish.mutateAsync(editLayout.id);
          setEditLayout(null);
        }}
      />
    );

  return (
    <>
      <PageHeader
        title={humanName(venue.data.name, 'Untitled venue')}
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
                        <small className="table-subline">Venue layout</small>
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
                                busy={editorLoading}
                                onClick={() => void openEditor(layout.id)}
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
