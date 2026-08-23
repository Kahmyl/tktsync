export type RequestOperation = {
  method: string;
  path: string;
  parameters: readonly { name: string; in: string; required: boolean; example: unknown }[];
  body?: { example: unknown };
};

export type RequestState = {
  values: Record<string, string>;
  body: unknown;
  idempotencyKey: string;
  idempotencyEdited: boolean;
};

const scalar = (value: unknown) => (typeof value === 'string' ? value : JSON.stringify(value));

export function initialRequest(operation: RequestOperation): RequestState {
  return {
    values: Object.fromEntries(
      operation.parameters.map((parameter) => [
        parameter.name,
        parameter.required ? scalar(parameter.example) : '',
      ]),
    ),
    body: operation.body ? structuredClone(operation.body.example) : undefined,
    idempotencyKey: crypto.randomUUID(),
    idempotencyEdited: false,
  };
}

export function materialEdit(
  state: RequestState,
  edit: Partial<Pick<RequestState, 'values' | 'body'>>,
): RequestState {
  return {
    ...state,
    ...edit,
    idempotencyKey: state.idempotencyEdited ? state.idempotencyKey : crypto.randomUUID(),
  };
}

export function buildRequest(
  operation: RequestOperation,
  state: RequestState,
  credential: string,
  basePath = '/__docs-exec',
) {
  let path = operation.path;
  const query = new URLSearchParams();
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (credential) headers.Authorization = `Bearer ${credential}`;
  for (const parameter of operation.parameters) {
    const value = state.values[parameter.name] || '';
    if (!value) continue;
    if (parameter.in === 'path')
      path = path.replace(`{${parameter.name}}`, encodeURIComponent(value));
    if (parameter.in === 'query') query.set(parameter.name, value);
    if (parameter.in === 'header')
      headers[parameter.name] = parameter.name === 'Idempotency-Key' ? state.idempotencyKey : value;
  }
  if (operation.body) headers['Content-Type'] = 'application/json';
  const suffix = query.size ? `?${query}` : '';
  return {
    url: `${basePath}${path}${suffix}`,
    displayUrl: `${path}${suffix}`,
    headers,
    body: operation.body ? JSON.stringify(state.body) : undefined,
  };
}

const shell = (value: string) => `'${value.replaceAll("'", "'\\''")}'`;
export function codeSample(
  language: string,
  operation: RequestOperation,
  state: RequestState,
  baseUrl: string,
) {
  const request = buildRequest(operation, state, '<PARTNER_API_KEY>', baseUrl);
  const headers = Object.entries(request.headers);
  if (language === 'javascript')
    return `const response = await fetch(${JSON.stringify(request.url)}, {\n  method: ${JSON.stringify(operation.method)},\n  headers: ${JSON.stringify(request.headers, null, 2).replaceAll('\n', '\n  ')},${request.body ? `\n  body: ${JSON.stringify(request.body)},` : ''}\n});\n\nconst data = await response.json();`;
  if (language === 'go')
    return `payload := strings.NewReader(${JSON.stringify(request.body || '')})\nreq, err := http.NewRequest(${JSON.stringify(operation.method)}, ${JSON.stringify(request.url)}, payload)\nif err != nil { log.Fatal(err) }\n${headers.map(([key, value]) => `req.Header.Set(${JSON.stringify(key)}, ${JSON.stringify(value)})`).join('\n')}\nresp, err := http.DefaultClient.Do(req)`;
  return [
    `curl --request ${operation.method} ${shell(request.url)}`,
    ...headers.map(([key, value]) => `  --header ${shell(`${key}: ${value}`)}`),
    ...(request.body ? [`  --data ${shell(request.body)}`] : []),
  ].join(' \\\n');
}
