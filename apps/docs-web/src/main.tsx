import { StrictMode, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { guides } from './content';
import { partnerOperations } from './generated/docs-model';
import {
  buildRequest,
  codeSample,
  initialRequest,
  materialEdit,
  type RequestOperation,
  type RequestState,
} from './request';
import './styles.css';

type DocField = {
  name: string;
  required: boolean;
  type: string;
  description: string;
  children: readonly DocField[];
};
type Operation = {
  operationId: string;
  group: string;
  route: string;
  method: string;
  path: string;
  successMediaType?: string;
  title: string;
  description: string;
  destructive: boolean;
  security: readonly string[];
  parameters: readonly {
    name: string;
    in: string;
    required: boolean;
    type: string;
    description: string;
    example: unknown;
  }[];
  body?: { required: boolean; fields: readonly DocField[]; example: unknown };
  responses: readonly {
    status: string;
    description: string;
    mediaType?: string;
    fields: readonly DocField[];
    example?: unknown;
  }[];
};
type Result = {
  status: number;
  statusText: string;
  duration: number;
  headers: Record<string, string>;
  body: string;
};
const operationList = partnerOperations as unknown as Operation[];
const guideGroups = [
  { title: 'Start', routes: ['/', '/quickstart'] },
  {
    title: 'Build your ticketing experience',
    routes: [
      '/workflows',
      '/workflows/selector',
      '/workflows/direct',
      '/workflows/tickets',
      '/workflows/recovery',
    ],
  },
  {
    title: 'Core concepts',
    routes: ['/authentication', '/errors', '/idempotency', '/pagination', '/webhooks'],
  },
];
const languages = ['curl', 'javascript', 'go'];
const environmentOptions = [
  {
    id: 'local',
    label: 'Local',
    display: import.meta.env.VITE_DOCS_LOCAL_API_BASE_URL || 'http://localhost:58480',
    executable: true,
  },
  ...(import.meta.env.VITE_DOCS_SANDBOX_API_BASE_URL
    ? [
        {
          id: 'sandbox',
          label: 'Sandbox',
          display: import.meta.env.VITE_DOCS_SANDBOX_API_BASE_URL,
          executable: false,
        },
      ]
    : []),
  ...(import.meta.env.VITE_DOCS_PRODUCTION_API_BASE_URL
    ? [
        {
          id: 'production',
          label: 'Production',
          display: import.meta.env.VITE_DOCS_PRODUCTION_API_BASE_URL,
          executable: false,
        },
      ]
    : []),
];

function usePath() {
  const [path, setPath] = useState(window.location.pathname);
  useEffect(() => {
    const pop = () => setPath(window.location.pathname);
    window.addEventListener('popstate', pop);
    return () => window.removeEventListener('popstate', pop);
  }, []);
  const navigate = (next: string) => {
    if (next !== path) history.pushState({}, '', next);
    setPath(next);
    window.scrollTo(0, 0);
  };
  return [path, navigate] as const;
}

function Icon({ name }: { name: 'search' | 'menu' | 'key' | 'close' | 'copy' | 'arrow' }) {
  const paths = {
    search: 'M11 4a7 7 0 1 0 4.9 12l4 4 1.1-1.1-4-4A7 7 0 0 0 11 4Z',
    menu: 'M4 7h16M4 12h16M4 17h16',
    key: 'M14 8a5 5 0 1 0-4.6 7l2.6 2h3v-3h3v-3h2V8h-6Z',
    close: 'm6 6 12 12M18 6 6 18',
    copy: 'M8 8h11v11H8zM5 16H4V4h12v1',
    arrow: 'm9 18 6-6-6-6',
  };
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path
        d={paths[name]}
        fill={name === 'search' || name === 'key' ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function Header({
  onMenu,
  onSearch,
  onCredential,
  credential,
  environment,
  setEnvironment,
}: {
  onMenu(): void;
  onSearch(): void;
  onCredential(): void;
  credential: string;
  environment: string;
  setEnvironment(value: string): void;
}) {
  return (
    <header className="topbar">
      <button className="icon-button menu-button" onClick={onMenu} aria-label="Open navigation">
        <Icon name="menu" />
      </button>
      <a
        className="brand"
        href="/"
        onClick={(event) => {
          event.preventDefault();
          history.pushState({}, '', '/');
          dispatchEvent(new PopStateEvent('popstate'));
        }}
      >
        <span className="brand-mark">T</span>
        <span>TktSync</span>
        <em>Developers</em>
      </a>
      <button className="search-trigger" onClick={onSearch}>
        <Icon name="search" />
        <span>Search documentation</span>
        <kbd>⌘ K</kbd>
      </button>
      <div className="top-actions">
        <select
          aria-label="API environment"
          value={environment}
          onChange={(event) => setEnvironment(event.target.value)}
        >
          {environmentOptions.map((option) => (
            <option key={option.id} value={option.id}>
              {option.label}
            </option>
          ))}
        </select>
        <button
          className={`credential-button ${credential ? 'connected' : ''}`}
          onClick={onCredential}
        >
          <Icon name="key" />
          {credential ? 'Test credential set' : 'Set Test Credential'}
        </button>
      </div>
    </header>
  );
}

function Navigation({
  path,
  navigate,
  open,
  close,
}: {
  path: string;
  navigate(path: string): void;
  open: boolean;
  close(): void;
}) {
  const endpointGroups = [...new Set(operationList.map((operation) => operation.group))];
  const link = (route: string, title: string, meta?: string) => (
    <button
      key={route}
      className={path === route ? 'nav-link active' : 'nav-link'}
      onClick={() => {
        navigate(route);
        close();
      }}
    >
      <span>{title}</span>
      {meta && <small>{meta}</small>}
    </button>
  );
  return (
    <>
      <div className={`nav-scrim ${open ? 'visible' : ''}`} onClick={close} />
      <aside className={`left-nav ${open ? 'open' : ''}`} aria-label="Documentation navigation">
        <div className="mobile-nav-head">
          <strong>Documentation</strong>
          <button className="icon-button" onClick={close}>
            <Icon name="close" />
          </button>
        </div>
        {guideGroups.map((group) => (
          <section key={group.title}>
            <h2>{group.title}</h2>
            {group.routes.map((route) => {
              const item = guides.find((guide) => guide.route === route);
              return item ? link(route, item.title) : null;
            })}
          </section>
        ))}
        <div className="nav-rule" />
        <p className="nav-caption">API reference</p>
        {endpointGroups.map((group) => (
          <section key={group}>
            <h2>{group}</h2>
            {operationList
              .filter((operation) => operation.group === group)
              .map((operation) => link(operation.route, operation.title, operation.method))}
          </section>
        ))}
      </aside>
    </>
  );
}

function SearchDialog({
  open,
  close,
  navigate,
}: {
  open: boolean;
  close(): void;
  navigate(path: string): void;
}) {
  const [query, setQuery] = useState('');
  useEffect(() => {
    if (!open) setQuery('');
  }, [open]);
  if (!open) return null;
  const records = [
    ...guides.map((guide) => ({ route: guide.route, title: guide.title, detail: guide.summary })),
    ...operationList.map((operation) => ({
      route: operation.route,
      title: operation.title,
      detail: `${operation.method} ${operation.path}`,
    })),
  ];
  const results = records
    .filter((record) =>
      `${record.title} ${record.detail}`.toLowerCase().includes(query.toLowerCase()),
    )
    .slice(0, 12);
  return (
    <div
      className="modal-layer"
      role="dialog"
      aria-modal="true"
      aria-label="Search documentation"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) close();
      }}
    >
      <div className="search-dialog">
        <label>
          <Icon name="search" />
          <input
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search endpoints, concepts, and parameters"
          />
          <button className="escape" onClick={close}>
            Esc
          </button>
        </label>
        <div className="search-results">
          {results.map((result) => (
            <button
              key={result.route}
              onClick={() => {
                navigate(result.route);
                close();
              }}
            >
              <span>
                <strong>{result.title}</strong>
                <small>{result.detail}</small>
              </span>
              <Icon name="arrow" />
            </button>
          ))}
          {results.length === 0 && <p>No matching documentation.</p>}
        </div>
      </div>
    </div>
  );
}

function CredentialDialog({
  open,
  close,
  credential,
  setCredential,
}: {
  open: boolean;
  close(): void;
  credential: string;
  setCredential(value: string): void;
}) {
  const [draft, setDraft] = useState(credential);
  useEffect(() => {
    if (open) setDraft(credential);
  }, [open, credential]);
  if (!open) return null;
  return (
    <div
      className="modal-layer"
      role="dialog"
      aria-modal="true"
      aria-labelledby="credential-title"
      onMouseDown={(event) => {
        if (event.currentTarget === event.target) close();
      }}
    >
      <form
        className="credential-dialog"
        onSubmit={(event) => {
          event.preventDefault();
          setCredential(draft.trim());
          close();
        }}
      >
        <div className="dialog-heading">
          <div>
            <span className="eyebrow">In-memory credential</span>
            <h2 id="credential-title">Connect the request console</h2>
          </div>
          <button type="button" className="icon-button" onClick={close}>
            <Icon name="close" />
          </button>
        </div>
        <p>
          Your Partner credential is held only in this page’s memory and clears on reload. It is
          never written to browser storage, cookies, URLs, logs, or telemetry.
        </p>
        <label>
          Partner API key
          <input
            autoFocus
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="Paste a local Partner credential"
          />
        </label>
        <div className="dialog-actions">
          {credential && (
            <button
              type="button"
              className="danger-link"
              onClick={() => {
                setCredential('');
                setDraft('');
                close();
              }}
            >
              Clear credential
            </button>
          )}
          <span />
          <button type="button" className="secondary" onClick={close}>
            Cancel
          </button>
          <button type="submit" className="primary">
            Use for this tab
          </button>
        </div>
      </form>
    </div>
  );
}

function GuidePage({ route, navigate }: { route: string; navigate(path: string): void }) {
  const guide = guides.find((item) => item.route === route) || guides[0]!;
  return (
    <main className="article guide">
      <div className="article-inner">
        <span className="eyebrow">{guide.eyebrow}</span>
        <h1>{guide.title}</h1>
        <p className="lede">{guide.summary}</p>
        {guide.sections.map((section) => (
          <section className="guide-section" key={section.title}>
            <h2>{section.title}</h2>
            <p>{section.body}</p>
            {section.steps && (
              <ol>
                {section.steps.map((step) => (
                  <li key={step}>{step}</li>
                ))}
              </ol>
            )}
            {section.flow && (
              <div className="workflow-flow" aria-label={`${section.title} workflow`}>
                {section.flow.map((step) => {
                  const content = (
                    <>
                      <span>{step.label}</span>
                      <strong>{step.title}</strong>
                      <p>{step.body}</p>
                      {step.route && <small>Open this step →</small>}
                    </>
                  );
                  return step.route ? (
                    <button
                      key={`${step.label}-${step.title}`}
                      onClick={() => navigate(step.route!)}
                    >
                      {content}
                    </button>
                  ) : (
                    <div key={`${step.label}-${step.title}`}>{content}</div>
                  );
                })}
              </div>
            )}
            {section.code && (
              <div className="guide-code">
                <span>{section.code.label}</span>
                <pre>
                  <code>{section.code.value}</code>
                </pre>
              </div>
            )}
            {section.links && (
              <div className="guide-links">
                {section.links.map((link) => (
                  <button key={link.route} onClick={() => navigate(link.route)}>
                    {link.label}
                    <Icon name="arrow" />
                  </button>
                ))}
              </div>
            )}
            {section.callout && (
              <aside className="callout">
                <strong>Important</strong>
                <p>{section.callout}</p>
              </aside>
            )}
          </section>
        ))}
        {route === '/' && (
          <div className="endpoint-cards">
            {operationList.slice(0, 6).map((operation) => (
              <button key={operation.route} onClick={() => navigate(operation.route)}>
                <span className={`method ${operation.method.toLowerCase()}`}>
                  {operation.method}
                </span>
                <strong>{operation.title}</strong>
                <small>{operation.path}</small>
              </button>
            ))}
          </div>
        )}
      </div>
    </main>
  );
}

function FieldList({ fields, depth = 0 }: { fields: readonly DocField[]; depth?: number }) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  return (
    <div className={`field-list depth-${depth}`}>
      {fields.map((field) => (
        <div className="field" key={field.name}>
          <div className="field-title">
            <code>{field.name}</code>
            <span>{field.type}</span>
            {field.required && <b>required</b>}
          </div>
          {field.description && <p>{field.description}</p>}
          {field.children.length > 0 && (
            <>
              <button
                className="expand-button"
                onClick={() =>
                  setExpanded((value) => ({ ...value, [field.name]: !value[field.name] }))
                }
              >
                {expanded[field.name] ? 'Hide' : 'Show'} child fields <Icon name="arrow" />
              </button>
              {expanded[field.name] && (
                <FieldList fields={field.children as never} depth={depth + 1} />
              )}
            </>
          )}
        </div>
      ))}
    </div>
  );
}

function EndpointArticle({ operation }: { operation: Operation }) {
  const parameterFields = operation.parameters.map((parameter) => ({
    ...parameter,
    children: [] as const,
  }));
  return (
    <main className="article endpoint-article">
      <div className="article-inner">
        <span className="eyebrow">{operation.group}</span>
        <h1>{operation.title}</h1>
        <div className="route-line">
          <span className={`method ${operation.method.toLowerCase()}`}>{operation.method}</span>
          <code>{operation.path}</code>
        </div>
        <p className="lede">
          {operation.description ||
            `Use this Partner operation to ${operation.title.toLowerCase()}. The authenticated Partner and Event scope are verified for every request.`}
        </p>
        <aside className="auth-note">
          <span>
            <Icon name="key" />
          </span>
          <div>
            <strong>Partner authentication required</strong>
            <p>Send a bearer credential. Resource knowledge alone never grants access.</p>
          </div>
        </aside>
        {parameterFields.length > 0 && (
          <section>
            <h2>Parameters</h2>
            <FieldList fields={parameterFields} />
          </section>
        )}
        {operation.body && (
          <section>
            <h2>Request body</h2>
            <FieldList fields={operation.body.fields} />
          </section>
        )}
        <section>
          <h2>Responses</h2>
          {operation.responses.map((response) => (
            <details
              className="response-doc"
              key={response.status}
              open={
                response.status === '200' || response.status === '201' || response.status === '2XX'
              }
            >
              <summary>
                <code>{response.status}</code>
                <span>{response.description}</span>
              </summary>
              {response.fields.length > 0 && <FieldList fields={response.fields} />}
              {response.example !== undefined && (
                <pre>{JSON.stringify(response.example, null, 2)}</pre>
              )}
            </details>
          ))}
        </section>
      </div>
    </main>
  );
}

function InputValue({
  label,
  value,
  onChange,
  secret = false,
}: {
  label: string;
  value: unknown;
  onChange(value: unknown): void;
  secret?: boolean;
}) {
  const complex = typeof value === 'object' && value !== null;
  if (complex)
    return (
      <label className="work-field">
        <span>{label}</span>
        <textarea
          rows={Math.min(9, JSON.stringify(value, null, 2).split('\n').length + 1)}
          value={JSON.stringify(value, null, 2)}
          onChange={(event) => {
            try {
              onChange(JSON.parse(event.target.value));
            } catch {
              /* keep the last valid structured value */
            }
          }}
        />
      </label>
    );
  return (
    <label className="work-field">
      <span>{label}</span>
      <input
        type={secret ? 'password' : 'text'}
        autoComplete="off"
        value={String(value ?? '')}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}

function Workbench({
  operation,
  credential,
  onCredential,
  environment,
}: {
  operation: Operation;
  credential: string;
  onCredential(): void;
  environment: string;
}) {
  const typedOperation = operation as unknown as RequestOperation;
  const [mode, setMode] = useState<'try' | 'code'>('try');
  const [language, setLanguage] = useState('curl');
  const [request, setRequest] = useState<RequestState>(() => initialRequest(typedOperation));
  const [result, setResult] = useState<Result>();
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  useEffect(() => {
    setRequest(initialRequest(operation as unknown as RequestOperation));
    setResult(undefined);
    setConfirming(false);
  }, [operation]);
  const selectedEnvironment =
    environmentOptions.find((option) => option.id === environment) || environmentOptions[0]!;
  const requestFields = operation.parameters.filter(
    (parameter) =>
      parameter.in !== 'header' || !['Idempotency-Key', 'Authorization'].includes(parameter.name),
  );
  const execute = async () => {
    if (!credential) {
      onCredential();
      return;
    }
    if (operation.destructive && !confirming) {
      setConfirming(true);
      return;
    }
    setBusy(true);
    setConfirming(false);
    const start = performance.now();
    try {
      const normalized = buildRequest(typedOperation, request, credential);
      const response = await fetch(normalized.url, {
        method: operation.method,
        headers: normalized.headers,
        body: normalized.body,
        credentials: 'omit',
        cache: 'no-store',
      });
      const headers = Object.fromEntries(
        [...response.headers.entries()].filter(([name]) => !/set-cookie/i.test(name)),
      );
      setResult({
        status: response.status,
        statusText: response.statusText,
        duration: Math.round(performance.now() - start),
        headers,
        body: await response.text(),
      });
    } catch (error) {
      setResult({
        status: 0,
        statusText: 'Network error',
        duration: Math.round(performance.now() - start),
        headers: {},
        body: error instanceof Error ? error.message : 'Request failed',
      });
    } finally {
      setBusy(false);
    }
  };
  return (
    <aside className="workbench" aria-label="Request workbench">
      <div className="workbench-head">
        <div className="mode-tabs">
          <button className={mode === 'try' ? 'active' : ''} onClick={() => setMode('try')}>
            Try it
          </button>
          <button className={mode === 'code' ? 'active' : ''} onClick={() => setMode('code')}>
            Code
          </button>
        </div>
        <span className={`environment-dot ${selectedEnvironment.id}`}>
          {selectedEnvironment.label}
        </span>
      </div>
      {mode === 'code' ? (
        <div className="code-view">
          <div className="language-tabs">
            {languages.map((item) => (
              <button
                key={item}
                className={language === item ? 'active' : ''}
                onClick={() => setLanguage(item)}
              >
                {item === 'javascript' ? 'Node.js' : item}
              </button>
            ))}
          </div>
          <pre>
            <code>
              {codeSample(language, typedOperation, request, selectedEnvironment.display)}
            </code>
          </pre>
          <button
            className="copy-button"
            onClick={() =>
              navigator.clipboard.writeText(
                codeSample(language, typedOperation, request, selectedEnvironment.display),
              )
            }
          >
            <Icon name="copy" /> Copy
          </button>
        </div>
      ) : (
        <div className="try-view">
          <div className="request-summary">
            <span className={`method ${operation.method.toLowerCase()}`}>{operation.method}</span>
            <code>{operation.path}</code>
          </div>
          {requestFields.map((parameter) => (
            <InputValue
              key={parameter.name}
              label={`${parameter.name}${parameter.required ? ' *' : ''}`}
              value={request.values[parameter.name] || ''}
              secret={parameter.name === 'X-TktSync-Reservation-Token'}
              onChange={(value) =>
                setRequest((current) =>
                  materialEdit(current, {
                    values: { ...current.values, [parameter.name]: String(value) },
                  }),
                )
              }
            />
          ))}
          {operation.parameters.some((parameter) => parameter.name === 'Idempotency-Key') && (
            <label className="work-field">
              <span>Idempotency-Key *</span>
              <div className="key-input">
                <input
                  value={request.idempotencyKey}
                  onChange={(event) =>
                    setRequest((current) => ({
                      ...current,
                      idempotencyKey: event.target.value,
                      idempotencyEdited: true,
                      idempotencyIntentChanged: false,
                    }))
                  }
                />
                <button
                  title="Generate a new key"
                  onClick={() =>
                    setRequest((current) => ({
                      ...current,
                      idempotencyKey: crypto.randomUUID(),
                      idempotencyEdited: false,
                      idempotencyIntentChanged: false,
                    }))
                  }
                >
                  ↻
                </button>
              </div>
              <small>Preserved for identical retries.</small>
              {request.idempotencyIntentChanged && (
                <small className="key-warning">
                  Request intent changed. Reusing your manually supplied key may cause an
                  idempotency conflict.
                </small>
              )}
            </label>
          )}
          {operation.body && (
            <div className="body-fields">
              <h3>JSON body</h3>
              {Object.entries(
                (request.body ?? operation.body.example) as Record<string, unknown>,
              ).map(([key, value]) => (
                <InputValue
                  key={key}
                  label={key}
                  value={value}
                  onChange={(next) =>
                    setRequest((current) =>
                      materialEdit(current, {
                        body: {
                          ...((current.body ?? operation.body?.example) as Record<string, unknown>),
                          [key]: next,
                        },
                      }),
                    )
                  }
                />
              ))}
            </div>
          )}{' '}
          {!selectedEnvironment.executable && (
            <div className="disabled-note">
              Live requests are disabled for {selectedEnvironment.label}. Code samples use its
              configured base URL.
            </div>
          )}
          {confirming && (
            <div className="confirm-box">
              <strong>Confirm state-changing request</strong>
              <p>
                This operation may release, void, or otherwise irreversibly change authoritative
                state.
              </p>
              <button onClick={execute}>I understand, send request</button>
              <button onClick={() => setConfirming(false)}>Cancel</button>
            </div>
          )}
          <button
            className="send-button"
            disabled={busy || !selectedEnvironment.executable}
            onClick={execute}
          >
            {busy ? 'Sending…' : credential ? 'Send request' : 'Set credential to send'}{' '}
            <Icon name="arrow" />
          </button>
          {result && (
            <div className="result">
              <div className="result-head">
                <strong
                  className={result.status >= 200 && result.status < 300 ? 'success' : 'failure'}
                >
                  {result.status || 'ERR'} {result.statusText}
                </strong>
                <span>{result.duration} ms</span>
              </div>
              <details>
                <summary>Response headers</summary>
                <pre>{JSON.stringify(result.headers, null, 2)}</pre>
              </details>
              <pre className="response-body">
                {(() => {
                  try {
                    return JSON.stringify(JSON.parse(result.body), null, 2);
                  } catch {
                    return result.body;
                  }
                })()}
              </pre>
            </div>
          )}
        </div>
      )}
    </aside>
  );
}

function NotFound({ navigate }: { navigate(path: string): void }) {
  return (
    <main className="article">
      <div className="article-inner">
        <span className="eyebrow">404</span>
        <h1>Documentation not found</h1>
        <p className="lede">This route is not part of the public Partner API reference.</p>
        <button className="primary" onClick={() => navigate('/')}>
          Return to overview
        </button>
      </div>
    </main>
  );
}

export function App() {
  const [path, navigate] = usePath();
  const [navOpen, setNavOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [credentialOpen, setCredentialOpen] = useState(false);
  const [credential, setCredential] = useState('');
  const [environment, setEnvironment] = useState(environmentOptions[0]!.id);
  const operation = operationList.find((item) => item.route === path);
  const guide = guides.some((item) => item.route === path);
  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setSearchOpen(true);
      }
      if (event.key === 'Escape') {
        setSearchOpen(false);
        setCredentialOpen(false);
        setNavOpen(false);
      }
    };
    addEventListener('keydown', shortcut);
    return () => removeEventListener('keydown', shortcut);
  }, []);
  const content = operation ? (
    <>
      <EndpointArticle operation={operation} />
      <Workbench
        operation={operation}
        credential={credential}
        onCredential={() => setCredentialOpen(true)}
        environment={environment}
      />
    </>
  ) : guide ? (
    <GuidePage route={path} navigate={navigate} />
  ) : (
    <NotFound navigate={navigate} />
  );
  return (
    <>
      <Header
        onMenu={() => setNavOpen(true)}
        onSearch={() => setSearchOpen(true)}
        onCredential={() => setCredentialOpen(true)}
        credential={credential}
        environment={environment}
        setEnvironment={setEnvironment}
      />
      <Navigation path={path} navigate={navigate} open={navOpen} close={() => setNavOpen(false)} />
      <div className={`page-grid ${operation ? 'has-workbench' : ''}`}>{content}</div>
      <SearchDialog open={searchOpen} close={() => setSearchOpen(false)} navigate={navigate} />
      <CredentialDialog
        open={credentialOpen}
        close={() => setCredentialOpen(false)}
        credential={credential}
        setCredential={setCredential}
      />
    </>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
