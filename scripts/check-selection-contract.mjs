import fs from 'node:fs';

const doc = JSON.parse(
  fs.readFileSync(new URL('../openapi/tktsync.v1.json', import.meta.url), 'utf8'),
);

const componentParameters = doc.components?.parameters ?? {};

function resolveParameter(parameter) {
  if (!parameter?.$ref) return parameter;

  const name = parameter.$ref.split('/').at(-1);

  return componentParameters[name];
}

function effectiveHeaders(route, method) {
  const item = doc.paths[route];
  const operation = item[method];

  return [...(item.parameters ?? []), ...(operation.parameters ?? [])]
    .map(resolveParameter)
    .filter((parameter) => parameter?.in === 'header')
    .map((parameter) => ({
      name: parameter.name,
      required: parameter.required === true,
    }));
}

function requireHeader(route, method, name) {
  const headers = effectiveHeaders(route, method);

  const found = headers.find((header) => header.name === name);

  if (!found || !found.required) {
    throw new Error(`${method.toUpperCase()} ${route} must require ${name}`);
  }
}

requireHeader('/api/v1/partner/selection-sessions', 'post', 'Idempotency-Key');

requireHeader('/api/v1/selection/reservations', 'post', 'Idempotency-Key');

for (const [route, method] of [
  ['/api/v1/selection/reservations/{reservation_id}', 'patch'],
  ['/api/v1/selection/reservations/{reservation_id}/release', 'post'],
]) {
  requireHeader(route, method, 'Idempotency-Key');

  requireHeader(route, method, 'X-TktSync-Reservation-Token');
}

for (const [route, item] of Object.entries(doc.paths)) {
  if (!route.startsWith('/api/v1/selection/')) {
    continue;
  }

  for (const method of ['get', 'post', 'put', 'patch', 'delete']) {
    const operation = item[method];

    if (!operation) continue;

    const security = JSON.stringify(operation.security);

    const expected = JSON.stringify([
      {
        SelectionBearer: [],
      },
    ]);

    if (security !== expected) {
      throw new Error(`${method.toUpperCase()} ${route} must require SelectionBearer`);
    }
  }
}

console.log('Selection security and mutation-header contract: PASS');
