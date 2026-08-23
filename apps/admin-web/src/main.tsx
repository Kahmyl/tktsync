/* eslint-disable react-refresh/only-export-components */
import { StrictMode, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { createTktSyncClient } from '@tktsync/api-client';
import {
  Button,
  FormField,
  InlineNotice,
  Metric,
  PageHeader,
  Panel,
  ProductShell,
  StatusPill,
} from '@tktsync/ui';
import './styles.css';

type Workflow = {
  group: string;
  title: string;
  description: string;
  method: 'GET' | 'POST' | 'PATCH' | 'PUT';
  path: string;
  body: string;
};
export const workflows: Workflow[] = [
  {
    group: 'Build',
    title: 'Venues',
    description: 'Create venues and inspect operational inventory.',
    method: 'POST',
    path: '/api/v1/admin/venues',
    body: '{"name":"Harbour Arena","timezone_name":"Africa/Lagos"}',
  },
  {
    group: 'Build',
    title: 'Venue layouts',
    description: 'Version, edit and publish floor plans.',
    method: 'POST',
    path: '/api/v1/admin/venues/{venue_id}/layout-versions',
    body: '{"name":"Main bowl v1"}',
  },
  {
    group: 'Build',
    title: 'Events',
    description: 'Create events and control their lifecycle.',
    method: 'POST',
    path: '/api/v1/admin/events',
    body: '{"venue_id":"ven_…","name":"Saturday Live","admission_policy":"SINGLE_ENTRY"}',
  },
  {
    group: 'Commerce',
    title: 'Pricing',
    description: 'Define tiers and assign commercial terms.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/price-tiers',
    body: '{"code":"STANDARD","name":"Standard","amount_minor":250000,"currency":"NGN"}',
  },
  {
    group: 'Commerce',
    title: 'Inventory',
    description: 'Materialize the published layout into event inventory.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/materialize-layout',
    body: '{}',
  },
  {
    group: 'Commerce',
    title: 'Blocks',
    description: 'Protect operational inventory and release safely.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/blocks',
    body: '{"purpose":"PRODUCTION","reason":"Sightline hold","reserved_unit_ids":[]}',
  },
  {
    group: 'Commerce',
    title: 'Allocations',
    description: 'Assign neutral channel or non-public inventory.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/allocations',
    body: '{"mode":"CHANNEL","partner_id":"ptr_…","purpose":"CHANNEL","release_destination":{"kind":"SHARED"}}',
  },
  {
    group: 'Partners',
    title: 'Partner configuration',
    description: 'Credentials, access, return destinations and webhooks.',
    method: 'PUT',
    path: '/api/v1/admin/partners/{partner_id}/allowed-return-urls',
    body: '{"urls":["https://partner.example/checkout/return"]}',
  },
  {
    group: 'Live event',
    title: 'Sales lifecycle',
    description: 'Open sales only after the event passes its readiness gate.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/open-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Pause sales',
    description: 'Stop new acquisition while existing protected transactions resolve.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/pause-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Resume sales',
    description: 'Resume acquisition from an explicitly paused event.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/resume-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Close sales',
    description: 'Close acquisition without erasing existing commercial history.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/close-sales',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Cancel event',
    description: 'Cancel with an explicit auditable operational reason.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/cancel',
    body: '{"reason":"Event cancelled by promoter"}',
  },
  {
    group: 'Live event',
    title: 'Complete event',
    description: 'Mark closed event operations complete while retaining all history.',
    method: 'POST',
    path: '/api/v1/admin/events/{event_id}/complete',
    body: '{}',
  },
  {
    group: 'Live event',
    title: 'Ticket operations',
    description: 'Void, rotate credentials, or explicitly release capacity.',
    method: 'POST',
    path: '/api/v1/admin/tickets/{ticket_id}/void',
    body: '{"reason":"Customer support correction"}',
  },
  {
    group: 'Live event',
    title: 'Admission operations',
    description: 'Supervised overrides and auditable admission reversal.',
    method: 'POST',
    path: '/api/v1/admin/admissions/manual-override',
    body: '{"event_id":"evt_…","ticket_id":"tkt_…","reason":"Accessibility desk verification","supervisor_user_id":"usr_…"}',
  },
  {
    group: 'Operations',
    title: 'Inventory reporting',
    description: 'Reconcile current inventory obligations separately from historical sales.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/reports/inventory',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Commercial reporting',
    description:
      'Inspect immutable Sale facts and current sold capacity without conflating issuance.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/reports/sales',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Admission reporting',
    description: 'Review active and reversed Admissions plus every scan outcome.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/reports/admission',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Audit explorer',
    description: 'Search a bounded, stable page of append-only material state changes.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/audit?limit=50',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Accreditation export',
    description: 'Generate a read-only CSV snapshot without QR credentials or unnecessary PII.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/accreditation-export',
    body: '',
  },
  {
    group: 'Operations',
    title: 'Metrics and alerts',
    description: 'Inspect advisory operational signals derived from authoritative records.',
    method: 'GET',
    path: '/api/v1/admin/events/{event_id}/metrics',
    body: '',
  },
];
type ApiResult = { status: number; data: unknown; error?: unknown };

export function App() {
  const [active, setActive] = useState(workflows[0]!);
  const [path, setPath] = useState(active.path);
  const [body, setBody] = useState(active.body);
  const [token, setToken] = useState(() => sessionStorage.getItem('tktsync.admin.token') ?? '');
  const [result, setResult] = useState<ApiResult>();
  const [busy, setBusy] = useState(false);
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  const select = (item: Workflow) => {
    setActive(item);
    setPath(item.path);
    setBody(item.body);
    setResult(undefined);
  };
  const execute = async () => {
    setBusy(true);
    setResult(undefined);
    sessionStorage.setItem('tktsync.admin.token', token);
    try {
      const request = client.request as unknown as (
        method: string,
        path: string,
        options: Record<string, unknown>,
      ) => Promise<{ data?: unknown; error?: unknown; response: Response }>;
      const response = await request(active.method, path, {
        headers: {
          Authorization: `Bearer ${token}`,
          'Idempotency-Key': crypto.randomUUID(),
          'X-Request-ID': crypto.randomUUID(),
        },
        body: active.method === 'GET' ? undefined : JSON.parse(body),
      });
      setResult({ status: response.response.status, data: response.data, error: response.error });
    } catch (error) {
      setResult({
        status: 0,
        data: null,
        error: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setBusy(false);
    }
  };
  const groups = [...new Set(workflows.map((item) => item.group))];
  return (
    <ProductShell
      product="Control room"
      eyebrow="TktSync Admin"
      actions={
        <>
          <StatusPill tone={token ? 'success' : 'warning'}>
            {token ? 'Authenticated' : 'Session required'}
          </StatusPill>
          <Button className="secondary hide-mobile" onClick={() => setToken('')}>
            Clear session
          </Button>
        </>
      }
      navigation={
        <aside className="admin-nav">
          {groups.map((group) => (
            <div key={group}>
              <span>{group}</span>
              {workflows
                .filter((item) => item.group === group)
                .map((item) => (
                  <button
                    className={active.title === item.title ? 'active' : ''}
                    key={item.title}
                    onClick={() => select(item)}
                  >
                    {item.title}
                  </button>
                ))}
            </div>
          ))}
        </aside>
      }
    >
      <PageHeader
        title={active.title}
        description={active.description}
        actions={
          <Button onClick={execute} disabled={busy || !token}>
            {busy ? 'Sending…' : `${active.method} command`}
          </Button>
        }
      />
      <div className="metrics">
        <Metric label="Authority" value="PostgreSQL" detail="Live source of truth" />
        <Metric label="Contract" value="82 routes" detail="Runtime parity certified" />
        <Metric label="Realtime" value="Advisory" detail="Always re-fetch state" />
        <Metric label="Command safety" value="Idempotent" detail="Fresh key per intent" />
      </div>
      <div className="workspace">
        <Panel
          title="Operation"
          description="Commands are sent through the generated OpenAPI client. Replace path placeholders with public IDs."
        >
          <div className="form-stack">
            <FormField
              label="Admin bearer"
              hint="Held in sessionStorage only for this browser tab."
            >
              <input
                type="password"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Paste human bearer token"
                autoComplete="off"
              />
            </FormField>
            <div className="method-path">
              <strong>{active.method}</strong>
              <FormField label="Contract path">
                <input value={path} onChange={(e) => setPath(e.target.value)} />
              </FormField>
            </div>
            {active.method !== 'GET' && (
              <FormField label="JSON request">
                <textarea
                  value={body}
                  onChange={(e) => setBody(e.target.value)}
                  spellCheck={false}
                />
              </FormField>
            )}
            <InlineNotice title="Authority check">
              Roles and event scope are evaluated by the API for every request. Knowing a resource
              ID does not grant access.
            </InlineNotice>
          </div>
        </Panel>
        <Panel
          title="Response"
          description="Machine-readable results stay visible while you reconcile the authoritative state."
        >
          {result ? (
            <>
              <StatusPill tone={result.status >= 200 && result.status < 300 ? 'success' : 'danger'}>
                {result.status || 'Client error'}
              </StatusPill>
              <pre className="response">{JSON.stringify(result.error ?? result.data, null, 2)}</pre>
            </>
          ) : (
            <div className="empty-state">
              <span>↗</span>
              <strong>No command sent</strong>
              <p>Complete the operation form and send a command.</p>
            </div>
          )}
        </Panel>
      </div>
    </ProductShell>
  );
}
const root = typeof document === 'undefined' ? null : document.getElementById('root');
if (root)
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
