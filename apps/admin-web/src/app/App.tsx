import { useMemo, useState } from 'react';
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
import { workflows, type Workflow } from '../features/workflows/catalog';

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
