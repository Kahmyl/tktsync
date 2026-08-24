import { KeyRound, Plus, ShieldAlert, Webhook } from 'lucide-react';
import { useState } from 'react';
import {
  Button,
  Dialog,
  DialogActions,
  EmptyState,
  ErrorState,
  Field,
  Input,
  LoadingState,
  MetricCard,
  PageHeader,
  Panel,
  PanelBody,
  SecretDialog,
  SectionHeading,
  Select,
  StatusPill,
} from '../../components/ui';
import { formatNumber, timeAgo } from '../../lib/format';
import { adminApi } from '../admin/api';
import { adminKeys, useIntentMutation, usePartners, useWebhooks } from '../admin/queries';

const availableSubscriptions = [
  'reservation.confirmed',
  'ticket.voided',
  'ticket.credential_reissued',
  'admission.admitted',
  'admission.reversed',
  'event.opened_for_sale',
  'event.sales_closed',
];

export function IntegrationsPage() {
  const webhooks = useWebhooks();
  const partners = usePartners();
  const [open, setOpen] = useState(false);
  const [partnerId, setPartnerId] = useState('');
  const [url, setUrl] = useState('');
  const [subscriptions, setSubscriptions] = useState<string[]>([
    'reservation.confirmed',
    'ticket.voided',
  ]);
  const [secret, setSecret] = useState('');
  const [secretTitle, setSecretTitle] = useState('Signing secret created');
  const [disableId, setDisableId] = useState('');
  const invalidate = [adminKeys.webhooks, adminKeys.partners()];
  const create = useIntentMutation({
    intent: () => `${partnerId}:webhook:${url}:${subscriptions.join(',')}`,
    mutationFn: (token, key) =>
      adminApi.createWebhook(token, key, partnerId, url.trim(), subscriptions),
    invalidate,
  });
  const disable = useIntentMutation({
    intent: () => `${disableId}:disable`,
    mutationFn: (token, key) =>
      adminApi.disableWebhook(token, key, disableId, 'Disabled by Admin operator'),
    invalidate,
  });
  const rotate = useIntentMutation({
    intent: (endpointId: string) => `${endpointId}:rotate`,
    mutationFn: (token, key, endpointId) => adminApi.rotateWebhook(token, key, endpointId),
    invalidate,
  });
  const submit = async () => {
    if (!partnerId || !url.startsWith('https://') || !subscriptions.length) return;
    const endpoint = await create.mutateAsync(undefined);
    setSecretTitle('Signing secret created');
    setSecret(endpoint.signing_secret);
    setOpen(false);
    setUrl('');
    setPartnerId('');
    setSubscriptions(['reservation.confirmed', 'ticket.voided']);
  };
  const rotateSecret = async (endpointId: string) => {
    const rotated = await rotate.mutateAsync(endpointId);
    setSecretTitle('Signing secret rotated');
    setSecret(rotated.signing_secret);
  };
  const disableEndpoint = async () => {
    await disable.mutateAsync(undefined);
    setDisableId('');
  };
  const items = webhooks.data?.items ?? [];
  const delivered = items.reduce((sum, item) => sum + item.delivered_24h, 0);
  const failed = items.reduce((sum, item) => sum + item.failed_24h, 0);
  return (
    <>
      <PageHeader
        title="Integrations"
        description="Notify partner systems with signed, authoritative ticket and admission events."
        actions={
          <Button onClick={() => setOpen(true)}>
            <Plus size={16} />
            Add endpoint
          </Button>
        }
      />
      <div className="metric-grid three">
        <MetricCard
          label="Endpoints"
          value={items.length}
          hint={`${items.filter((item) => item.state === 'ACTIVE').length} active`}
        />
        <MetricCard label="Delivered (24h)" value={delivered} />
        <MetricCard
          label="Failed (24h)"
          value={failed}
          hint={failed ? 'Needs attention' : 'No dead letters in the last day'}
        />
      </div>
      {webhooks.isLoading ? (
        <LoadingState rows={7} />
      ) : webhooks.error ? (
        <ErrorState error={webhooks.error} onRetry={() => void webhooks.refetch()} />
      ) : items.length ? (
        <div className="tab-stack">
          {items.map((endpoint) => (
            <Panel key={endpoint.id}>
              <SectionHeading
                title={endpoint.partner_name}
                description={endpoint.url}
                actions={
                  <StatusPill
                    label={endpoint.state === 'ACTIVE' ? 'Active' : 'Disabled'}
                    tone={endpoint.state === 'ACTIVE' ? 'positive' : 'neutral'}
                  />
                }
              />
              <div className="panel-divider" />
              <PanelBody>
                <div className="integration-metrics">
                  <div>
                    <small>Delivered (24h)</small>
                    <strong>{formatNumber(endpoint.delivered_24h)}</strong>
                  </div>
                  <div>
                    <small>Failed (24h)</small>
                    <strong>{formatNumber(endpoint.failed_24h)}</strong>
                  </div>
                  <div>
                    <small>Signing</small>
                    <strong>Configured</strong>
                  </div>
                  <div>
                    <small>Last delivery</small>
                    <strong>{timeAgo(endpoint.last_delivery_at)}</strong>
                  </div>
                </div>
                <div className="topic-list">
                  {endpoint.subscriptions.map((subscription) => (
                    <span key={subscription}>{subscription}</span>
                  ))}
                </div>
                <div className="panel-button-row">
                  {endpoint.state === 'ACTIVE' ? (
                    <>
                      <Button
                        variant="secondary"
                        size="small"
                        busy={rotate.isPending && rotate.variables === endpoint.id}
                        onClick={() => void rotateSecret(endpoint.id)}
                      >
                        <KeyRound size={14} />
                        Rotate signing secret
                      </Button>
                      <Button
                        variant="ghost"
                        size="small"
                        onClick={() => setDisableId(endpoint.id)}
                      >
                        Disable endpoint
                      </Button>
                    </>
                  ) : null}
                </div>
              </PanelBody>
            </Panel>
          ))}
        </div>
      ) : (
        <Panel>
          <EmptyState
            icon={<Webhook size={20} />}
            title="No endpoints yet"
            description="Add an endpoint so partner systems stay in sync with ticket activity."
            action={<Button onClick={() => setOpen(true)}>Add endpoint</Button>}
          />
        </Panel>
      )}
      <Dialog
        open={open}
        title="Add endpoint"
        description="TktSync will send signed notifications to this HTTPS URL."
        onClose={() => setOpen(false)}
        className="wide-dialog"
      >
        <div className="dialog-body form-stack">
          <Field label="Partner">
            <Select
              id="webhook-partner"
              value={partnerId}
              onChange={(event) => setPartnerId(event.target.value)}
            >
              <option value="">Select a partner</option>
              {partners.data?.items
                .filter((partner) => partner.state === 'ACTIVE')
                .map((partner) => (
                  <option value={partner.id} key={partner.id}>
                    {partner.name}
                  </option>
                ))}
            </Select>
          </Field>
          <Field label="Endpoint URL">
            <Input
              id="webhook-url"
              type="url"
              value={url}
              onChange={(event) => setUrl(event.target.value)}
              placeholder="https://partner.example/hooks/tktsync"
            />
          </Field>
          <fieldset className="checkbox-grid">
            <legend>Subscriptions</legend>
            {availableSubscriptions.map((topic) => (
              <label key={topic}>
                <input
                  type="checkbox"
                  checked={subscriptions.includes(topic)}
                  onChange={(event) =>
                    setSubscriptions((items) =>
                      event.target.checked
                        ? [...items, topic]
                        : items.filter((item) => item !== topic),
                    )
                  }
                />
                {topic}
              </label>
            ))}
          </fieldset>
          {create.error ? <ErrorState error={create.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button
            busy={create.isPending}
            disabled={!partnerId || !url.startsWith('https://') || !subscriptions.length}
            onClick={() => void submit()}
          >
            Add endpoint
          </Button>
        </DialogActions>
      </Dialog>
      <SecretDialog
        open={Boolean(secret)}
        title={secretTitle}
        description="TktSync will not expose this signing secret after this dialog closes."
        secret={secret}
        onClose={() => setSecret('')}
      />
      <Dialog
        open={Boolean(disableId)}
        title="Disable endpoint"
        description="No new webhook deliveries will be scheduled for this endpoint."
        onClose={() => setDisableId('')}
      >
        <div className="dialog-body">
          <div className="danger-block">
            <ShieldAlert size={20} />
            <p>
              This cannot be resumed through the current backend contract; create a replacement
              endpoint when needed.
            </p>
          </div>
          {disable.error ? <ErrorState error={disable.error} /> : null}
        </div>
        <DialogActions>
          <Button variant="secondary" onClick={() => setDisableId('')}>
            Keep endpoint
          </Button>
          <Button variant="danger" busy={disable.isPending} onClick={() => void disableEndpoint()}>
            Disable endpoint
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
