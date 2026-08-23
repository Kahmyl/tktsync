import { useMemo, useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
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
  Spinner,
} from '@tktsync/ui';
import { useOperatorSession } from '../auth/useOperatorSession';
import {
  initialWorkflowValues,
  materializeWorkflow,
  workflowFields,
  workflows,
  type Workflow,
  type WorkflowValues,
} from '../features/workflows/catalog';

type ApiResult = {
  status: number;
  data: unknown;
  error?: unknown;
};

const destructiveOperations = new Set([
  'Pause sales',
  'Close sales',
  'Cancel event',
  'Complete event',
  'Ticket operations',
  'Admission operations',
]);

export function App() {
  const auth = useOperatorSession();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [active, setActive] = useState(workflows[0]!);
  const [values, setValues] = useState<WorkflowValues>(() => initialWorkflowValues(workflows[0]!));
  const [result, setResult] = useState<ApiResult>();
  const intentKeys = useRef(new Map<string, string>());

  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);

  const command = useMutation({
    retry: false,
    mutationFn: (
      executeRequest: () => Promise<{
        data?: unknown;
        error?: unknown;
        response: Response;
      }>,
    ) => executeRequest(),
  });

  const busy = command.isPending;
  const fields = workflowFields(active);
  const groups = [...new Set(workflows.map((workflow) => workflow.group))];

  const select = (workflow: Workflow) => {
    setActive(workflow);
    setValues(initialWorkflowValues(workflow));
    setResult(undefined);
  };

  const updateField = (key: string, value: string) => {
    setValues((current) => ({ ...current, [key]: value }));
  };

  const execute = async () => {
    setResult(undefined);

    let materialized: ReturnType<typeof materializeWorkflow>;

    try {
      materialized = materializeWorkflow(active, values);
    } catch (error) {
      setResult({
        status: 0,
        data: null,
        error: error instanceof Error ? error.message : String(error),
      });
      return;
    }

    if (
      destructiveOperations.has(active.title) &&
      !window.confirm(`Confirm ${active.title.toLowerCase()}?`)
    ) {
      return;
    }

    const request = client.request as unknown as (
      method: string,
      path: string,
      options: Record<string, unknown>,
    ) => Promise<{ data?: unknown; error?: unknown; response: Response }>;

    const intent = JSON.stringify([active.method, materialized.path, materialized.body ?? null]);

    const idempotencyKey =
      active.method === 'GET' ? undefined : (intentKeys.current.get(intent) ?? crypto.randomUUID());

    if (idempotencyKey) {
      intentKeys.current.set(intent, idempotencyKey);
    }

    try {
      const headers: Record<string, string> = {
        Authorization: `Bearer ${auth.token}`,
        'X-Request-ID': crypto.randomUUID(),
      };

      if (idempotencyKey) {
        headers['Idempotency-Key'] = idempotencyKey;
      }

      const response = await command.mutateAsync(() =>
        request(active.method, materialized.path, {
          headers,
          body: materialized.body,
        }),
      );

      intentKeys.current.delete(intent);

      setResult({
        status: response.response.status,
        data: response.data,
        error: response.error,
      });
    } catch (error) {
      setResult({
        status: 0,
        data: null,
        error:
          error instanceof Error
            ? `${error.message} Retry is safe with the retained idempotency key.`
            : 'Network failure. Retry is safe with the retained idempotency key.',
      });
    }
  };

  if (auth.loading) {
    return (
      <ProductShell product="Control room" eyebrow="TktSync Admin">
        <Panel title="Restoring operator session">
          <Spinner label="Restoring operator session" />
        </Panel>
      </ProductShell>
    );
  }

  if (!auth.authenticated) {
    return (
      <ProductShell product="Control room" eyebrow="TktSync Admin">
        <div className="auth-shell">
          <PageHeader
            title="Operator sign in"
            description="Authenticate with your TktSync operator identity before accessing administrative workflows."
          />
          <Panel
            title="Secure operator session"
            description="Credentials are exchanged with the configured identity provider. The Admin product does not accept pasted bearer tokens."
          >
            <div className="form-stack">
              <FormField label="Email">
                <input
                  type="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  autoComplete="username"
                />
              </FormField>
              <FormField label="Password">
                <input
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete="current-password"
                />
              </FormField>
              {auth.error && (
                <InlineNotice tone="warning" title="Authentication">
                  {auth.error}
                </InlineNotice>
              )}
              <Button
                disabled={auth.loading || !email.trim() || !password}
                onClick={() => void auth.signIn(email, password)}
              >
                {auth.loading ? 'Signing in…' : 'Sign in'}
              </Button>
            </div>
          </Panel>
        </div>
      </ProductShell>
    );
  }

  return (
    <ProductShell
      product="Control room"
      eyebrow="TktSync Admin"
      actions={
        <>
          <StatusPill tone="success">Authenticated</StatusPill>
          <span className="operator-label">{auth.userLabel}</span>
          <Button className="secondary hide-mobile" onClick={() => void auth.signOut()}>
            Sign out
          </Button>
        </>
      }
      navigation={
        <aside className="admin-nav">
          {groups.map((group) => (
            <div key={group}>
              <span>{group}</span>
              {workflows
                .filter((workflow) => workflow.group === group)
                .map((workflow) => (
                  <button
                    className={active.title === workflow.title ? 'active' : ''}
                    key={workflow.title}
                    onClick={() => select(workflow)}
                  >
                    {workflow.title}
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
          <Button onClick={() => void execute()} disabled={busy}>
            {busy ? (
              <>
                <Spinner label="Sending command" /> Sending…
              </>
            ) : active.method === 'GET' ? (
              'Load'
            ) : (
              'Submit'
            )}
          </Button>
        }
      />

      <div className="metrics">
        <Metric label="Authority" value="PostgreSQL" detail="Live source of truth" />
        <Metric label="Contract" value="OpenAPI" detail="Generated transport" />
        <Metric label="Realtime" value="Advisory" detail="Always re-fetch authority" />
        <Metric label="Command safety" value="Idempotent" detail="Protected mutation retries" />
      </div>

      <div className="workspace">
        <Panel
          title="Operation"
          description="Complete the domain fields below. API paths and request bodies are generated by the product."
        >
          <div className="form-stack">
            {fields.length === 0 ? (
              <InlineNotice title="Ready">
                This operation does not require additional parameters.
              </InlineNotice>
            ) : (
              fields.map((field) => (
                <FormField key={field.key} label={field.label}>
                  {field.kind === 'boolean' ? (
                    <select
                      value={values[field.key] ?? ''}
                      onChange={(event) => updateField(field.key, event.target.value)}
                    >
                      <option value="true">True</option>
                      <option value="false">False</option>
                    </select>
                  ) : field.kind === 'json' ? (
                    <textarea
                      value={values[field.key] ?? ''}
                      onChange={(event) => updateField(field.key, event.target.value)}
                      spellCheck={false}
                    />
                  ) : (
                    <input
                      type={field.kind === 'number' ? 'number' : 'text'}
                      value={values[field.key] ?? ''}
                      onChange={(event) => updateField(field.key, event.target.value)}
                      autoComplete="off"
                    />
                  )}
                </FormField>
              ))
            )}

            {destructiveOperations.has(active.title) && (
              <InlineNotice tone="warning" title="Confirmation required">
                This operation changes live event or admission state and requires explicit
                confirmation before submission.
              </InlineNotice>
            )}

            <InlineNotice title="Authority check">
              Roles and event scope are evaluated by the API for every request. Knowing a resource
              ID does not grant access.
            </InlineNotice>
          </div>
        </Panel>

        <Panel
          title="Result"
          description="The authoritative operation result remains visible for reconciliation."
        >
          {result ? (
            <>
              <StatusPill
                tone={
                  result.status >= 200 && result.status < 300
                    ? 'success'
                    : result.status === 0
                      ? 'warning'
                      : 'danger'
                }
              >
                {result.status || 'Client error'}
              </StatusPill>
              <pre className="response">{JSON.stringify(result.error ?? result.data, null, 2)}</pre>
            </>
          ) : (
            <div className="empty-state">
              <span>↗</span>
              <strong>No operation submitted</strong>
              <p>Complete the workflow and submit it when ready.</p>
            </div>
          )}
        </Panel>
      </div>
    </ProductShell>
  );
}
