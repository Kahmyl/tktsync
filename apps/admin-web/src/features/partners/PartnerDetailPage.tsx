import { KeyRound, Link2, Plus, ShieldAlert, Webhook } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  LoadingState,
  PageHeader,
  Panel,
  PanelBody,
  SecretDialog,
  SectionHeading,
  Select,
  StatusPill,
} from '../../components/ui';
import { formatDateTime, friendlyOperation, timeAgo } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useEvents, useIntentMutation, usePartner, useWebhooks } from '../admin/queries';

type PartnerTab = 'overview' | 'access' | 'credentials' | 'webhooks' | 'activity';

export function PartnerDetailPage() {
  const { partnerId = '' } = useParams();
  const partner = usePartner(partnerId);
  const events = useEvents();
  const webhooks = useWebhooks();
  const [tab, setTab] = useState<PartnerTab>('overview');
  const [secret, setSecret] = useState('');
  const [grantEventId, setGrantEventId] = useState('');
  const [revokeId, setRevokeId] = useState('');
  const [stateConfirm, setStateConfirm] = useState(false);
  const invalidate = [adminKeys.partner(partnerId), adminKeys.partners(), adminKeys.webhooks];
  const issue = useIntentMutation({
    intent: () => `${partnerId}:credential`,
    mutationFn: (token, key) => adminApi.issuePartnerCredential(token, key, partnerId),
    invalidate,
  });
  const revoke = useIntentMutation({
    intent: () => `${partnerId}:revoke:${revokeId}`,
    mutationFn: (token, key) => adminApi.revokePartnerCredential(token, key, revokeId),
    invalidate,
  });
  const state = useIntentMutation({
    intent: (enabled: boolean) => `${partnerId}:enabled:${enabled}`,
    mutationFn: (token, key, enabled) => adminApi.setPartnerState(token, key, partnerId, enabled),
    invalidate,
  });
  const access = useIntentMutation({
    intent: (variables: { eventId: string; enabled: boolean }) =>
      `${partnerId}:access:${variables.eventId}:${variables.enabled}`,
    mutationFn: (token, key, variables) =>
      adminApi.setEventAccess(token, key, variables.eventId, partnerId, variables.enabled),
    invalidate,
  });
  const partnerWebhooks = useMemo(
    () => (webhooks.data?.items ?? []).filter((endpoint) => endpoint.partner_id === partnerId),
    [partnerId, webhooks.data],
  );

  if (partner.isLoading) return <LoadingState rows={8} />;
  if (partner.error || !partner.data)
    return <ErrorState error={partner.error} onRetry={() => void partner.refetch()} />;
  const data = partner.data;
  const issueCredential = async () => {
    const created = await issue.mutateAsync(undefined);
    setSecret(created.credential);
    setTab('credentials');
  };
  const revokeCredential = async () => {
    await revoke.mutateAsync(undefined);
    setRevokeId('');
  };
  const changeState = async () => {
    await state.mutateAsync(data.state !== 'ACTIVE');
    setStateConfirm(false);
  };
  const grant = async () => {
    if (!grantEventId) return;
    await access.mutateAsync({ eventId: grantEventId, enabled: true });
    setGrantEventId('');
  };
  const availableEvents = (events.data?.items ?? []).filter(
    (event) =>
      !data.event_access.some(
        (record) => record.event_id === event.id && record.state === 'ACTIVE',
      ),
  );

  return (
    <>
      <PageHeader
        title={data.name}
        description={`Partner since ${formatDateTime(data.created_at)}`}
        eyebrow={
          <StatusPill
            label={data.state === 'ACTIVE' ? 'Active' : 'Disabled'}
            tone={data.state === 'ACTIVE' ? 'positive' : 'critical'}
          />
        }
        actions={
          <>
            <Button variant="secondary" onClick={() => setStateConfirm(true)}>
              {data.state === 'ACTIVE' ? 'Disable partner' : 'Enable partner'}
            </Button>
            <Button onClick={() => void issueCredential()} busy={issue.isPending}>
              <KeyRound size={16} />
              Issue credential
            </Button>
          </>
        }
      />
      {issue.error || state.error || access.error || revoke.error ? (
        <ErrorState error={issue.error ?? state.error ?? access.error ?? revoke.error} />
      ) : null}
      <div className="tabs" role="tablist">
        {(
          [
            ['overview', 'Overview'],
            ['access', 'Event access'],
            ['credentials', 'Credentials'],
            ['webhooks', 'Webhooks'],
            ['activity', 'Activity'],
          ] as const
        ).map(([value, label]) => (
          <button
            type="button"
            role="tab"
            aria-selected={tab === value}
            className={tab === value ? 'active' : ''}
            onClick={() => setTab(value)}
            key={value}
          >
            {label}
          </button>
        ))}
      </div>
      {tab === 'overview' ? (
        <div className="detail-grid">
          <Panel>
            <SectionHeading title="Partner overview" />
            <div className="panel-divider" />
            <PanelBody>
              <dl className="definition-grid">
                <div>
                  <dt>Status</dt>
                  <dd>{data.state === 'ACTIVE' ? 'Active' : 'Disabled'}</dd>
                </div>
                <div>
                  <dt>Created</dt>
                  <dd>{formatDateTime(data.created_at)}</dd>
                </div>
                <div>
                  <dt>Active event access</dt>
                  <dd>{data.event_access.filter((item) => item.state === 'ACTIVE').length}</dd>
                </div>
                <div>
                  <dt>Active credentials</dt>
                  <dd>{data.credentials.filter((item) => item.state === 'ACTIVE').length}</dd>
                </div>
              </dl>
            </PanelBody>
          </Panel>
          <Panel>
            <SectionHeading title="Integration health" />
            <div className="panel-divider" />
            <PanelBody>
              <dl className="definition-grid">
                <div>
                  <dt>Endpoints</dt>
                  <dd>{partnerWebhooks.length}</dd>
                </div>
                <div>
                  <dt>Active endpoints</dt>
                  <dd>{partnerWebhooks.filter((item) => item.state === 'ACTIVE').length}</dd>
                </div>
                <div>
                  <dt>Delivered (24h)</dt>
                  <dd>{partnerWebhooks.reduce((sum, item) => sum + item.delivered_24h, 0)}</dd>
                </div>
                <div>
                  <dt>Failed (24h)</dt>
                  <dd>{partnerWebhooks.reduce((sum, item) => sum + item.failed_24h, 0)}</dd>
                </div>
              </dl>
            </PanelBody>
          </Panel>
        </div>
      ) : null}
      {tab === 'access' ? (
        <Panel>
          <SectionHeading
            title="Event access"
            description="Grant or disable this partner's ability to acquire new inventory."
            actions={
              <div className="inline-form compact">
                <Select
                  aria-label="Event to grant"
                  value={grantEventId}
                  onChange={(event) => setGrantEventId(event.target.value)}
                >
                  <option value="">Choose event</option>
                  {availableEvents.map((event) => (
                    <option value={event.id} key={event.id}>
                      {event.name}
                    </option>
                  ))}
                </Select>
                <Button
                  size="small"
                  disabled={!grantEventId || data.state !== 'ACTIVE'}
                  busy={access.isPending}
                  onClick={() => void grant()}
                >
                  <Plus size={14} />
                  Grant access
                </Button>
              </div>
            }
          />
          <div className="panel-divider" />
          {data.event_access.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Event</th>
                    <th>Event status</th>
                    <th>Access</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {data.event_access.map((record) => (
                    <tr key={record.event_id}>
                      <td>
                        <Link className="record-link" to={`/events/${record.event_id}`}>
                          <strong>{record.event_name}</strong>
                        </Link>
                      </td>
                      <td>{record.event_state.replaceAll('_', ' ').toLowerCase()}</td>
                      <td>
                        <StatusPill
                          label={record.state === 'ACTIVE' ? 'Enabled' : 'Disabled'}
                          tone={record.state === 'ACTIVE' ? 'positive' : 'neutral'}
                        />
                      </td>
                      <td className="align-right">
                        {record.state === 'ACTIVE' ? (
                          <Button
                            variant="secondary"
                            size="small"
                            busy={access.isPending && access.variables?.eventId === record.event_id}
                            onClick={() =>
                              void access.mutateAsync({ eventId: record.event_id, enabled: false })
                            }
                          >
                            Disable access
                          </Button>
                        ) : (
                          <Button
                            variant="secondary"
                            size="small"
                            disabled={data.state !== 'ACTIVE'}
                            onClick={() =>
                              void access.mutateAsync({ eventId: record.event_id, enabled: true })
                            }
                          >
                            Enable access
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              icon={<Link2 size={20} />}
              title="No event access"
              description="Choose an event to let this partner begin selling."
            />
          )}
        </Panel>
      ) : null}
      {tab === 'credentials' ? (
        <Panel>
          <SectionHeading
            title="Credentials"
            description="Secret material is never returned after initial issuance."
            actions={
              <Button size="small" onClick={() => void issueCredential()} busy={issue.isPending}>
                <Plus size={14} />
                Issue credential
              </Button>
            }
          />
          <div className="panel-divider" />
          {data.credentials.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Key ID</th>
                    <th>Status</th>
                    <th>Created</th>
                    <th>Last used</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {data.credentials.map((credential) => (
                    <tr key={credential.id}>
                      <td className="num">
                        <strong>{credential.key_id}</strong>
                      </td>
                      <td>
                        <StatusPill
                          label={credential.state === 'ACTIVE' ? 'Active' : 'Revoked'}
                          tone={credential.state === 'ACTIVE' ? 'positive' : 'critical'}
                        />
                      </td>
                      <td>{formatDateTime(credential.created_at)}</td>
                      <td>{timeAgo(credential.last_used_at)}</td>
                      <td className="align-right">
                        {credential.state === 'ACTIVE' ? (
                          <Button
                            variant="ghost"
                            size="small"
                            onClick={() => setRevokeId(credential.id)}
                          >
                            Revoke
                          </Button>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              icon={<KeyRound size={20} />}
              title="No credentials"
              description="Issue a credential only when the partner is ready to store it securely."
              action={<Button onClick={() => void issueCredential()}>Issue credential</Button>}
            />
          )}
        </Panel>
      ) : null}
      {tab === 'webhooks' ? (
        <Panel>
          <SectionHeading
            title="Webhooks"
            description="Signed event notifications configured for this partner."
            actions={
              <Link className="button button-secondary button-small" to="/integrations">
                <Webhook size={14} />
                Manage integrations
              </Link>
            }
          />
          <div className="panel-divider" />
          {partnerWebhooks.length ? (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Endpoint</th>
                    <th>Status</th>
                    <th>Subscriptions</th>
                    <th>Last delivery</th>
                  </tr>
                </thead>
                <tbody>
                  {partnerWebhooks.map((endpoint) => (
                    <tr key={endpoint.id}>
                      <td>
                        <strong>{endpoint.url}</strong>
                      </td>
                      <td>
                        <StatusPill
                          label={endpoint.state === 'ACTIVE' ? 'Active' : 'Disabled'}
                          tone={endpoint.state === 'ACTIVE' ? 'positive' : 'neutral'}
                        />
                      </td>
                      <td>{endpoint.subscriptions.length}</td>
                      <td>{timeAgo(endpoint.last_delivery_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              icon={<Webhook size={20} />}
              title="No webhook endpoints"
              description="Add one from Integrations when this partner is ready to receive signed events."
              action={
                <Link className="button button-primary button-normal" to="/integrations">
                  Add endpoint
                </Link>
              }
            />
          )}
        </Panel>
      ) : null}
      {tab === 'activity' ? (
        <Panel>
          <SectionHeading
            title="Activity"
            description="Authoritative audit events for this partner."
          />
          <div className="panel-divider" />
          {data.activity.length ? (
            <ul className="activity-list padded">
              {data.activity.map((item, index) => (
                <li key={`${item.operation}-${item.occurred_at}-${index}`}>
                  <span />
                  <div>
                    <strong>{friendlyOperation(item.operation)}</strong>
                    <small>
                      {item.entity_type} · {timeAgo(item.occurred_at)}
                    </small>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState
              title="No partner activity"
              description="Credential, access and webhook changes will appear here."
            />
          )}
        </Panel>
      ) : null}
      <SecretDialog
        open={Boolean(secret)}
        title="Partner credential created"
        description="TktSync will not show this credential again."
        secret={secret}
        onClose={() => setSecret('')}
      />
      <Dialog
        open={Boolean(revokeId)}
        title="Revoke credential"
        description="The partner will immediately lose access through this credential."
        onClose={() => setRevokeId('')}
      >
        <div className="dialog-body">
          <div className="danger-block">
            <ShieldAlert size={20} />
            <p>Confirm the partner has another valid credential before revoking this one.</p>
          </div>
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setRevokeId('')}>
            Keep credential
          </Button>
          <Button variant="danger" busy={revoke.isPending} onClick={() => void revokeCredential()}>
            Revoke credential
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog
        open={stateConfirm}
        title={data.state === 'ACTIVE' ? 'Disable partner' : 'Enable partner'}
        description={
          data.state === 'ACTIVE'
            ? 'Disabling blocks new Partner API activity across every event.'
            : 'Enabling restores Partner API activity where event access remains active.'
        }
        onClose={() => setStateConfirm(false)}
      >
        <div className="dialog-body">
          <div className="danger-block">
            <ShieldAlert size={20} />
            <p>This changes authoritative partner state.</p>
          </div>
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setStateConfirm(false)}>
            Cancel
          </Button>
          <Button
            variant={data.state === 'ACTIVE' ? 'danger' : 'primary'}
            busy={state.isPending}
            onClick={() => void changeState()}
          >
            {data.state === 'ACTIVE' ? 'Disable partner' : 'Enable partner'}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
