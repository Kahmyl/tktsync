import { readFile, writeFile } from 'node:fs/promises';
import { format } from 'prettier';

const openapiPath = new URL('../openapi/tktsync.v1.json', import.meta.url);
const outputPath = new URL('../apps/docs-web/src/generated/docs-model.ts', import.meta.url);
const spec = JSON.parse(await readFile(openapiPath, 'utf8'));

const routes = {
  partnerGetEvent: ['Events', '/api/events/retrieve', 'Retrieve an event'],
  partnerGetEventLayout: ['Events', '/api/events/layout', 'Retrieve event layout'],
  partnerGetAvailability: ['Events', '/api/events/availability', 'Retrieve availability'],
  partnerCreateReservation: ['Reservations', '/api/reservations/create', 'Create a reservation'],
  partnerGetReservation: ['Reservations', '/api/reservations/retrieve', 'Retrieve a reservation'],
  partnerModifyReservation: ['Reservations', '/api/reservations/update', 'Modify a reservation'],
  partnerBeginReservationCheckout: [
    'Reservations',
    '/api/reservations/begin-checkout',
    'Begin checkout',
  ],
  partnerReportReservationPaymentFailure: [
    'Reservations',
    '/api/reservations/payment-failure',
    'Report payment failure',
  ],
  partnerReleaseReservation: ['Reservations', '/api/reservations/release', 'Release a reservation'],
  partnerConfirmReservation: ['Reservations', '/api/reservations/confirm', 'Confirm a reservation'],
  partnerGetTicketCredential: [
    'Tickets',
    '/api/tickets/retrieve-credential',
    'Retrieve a ticket credential',
  ],
  partnerGetTicketQR: ['Tickets', '/api/tickets/retrieve-qr', 'Retrieve a ticket QR image'],
  partnerVoidTicket: ['Tickets', '/api/tickets/void', 'Void a ticket'],
  partnerReissueTicketCredential: [
    'Tickets',
    '/api/tickets/reissue-credential',
    'Reissue a ticket credential',
  ],
  partnerReReleaseTicketInventory: [
    'Tickets',
    '/api/tickets/re-release-inventory',
    'Re-release ticket inventory',
  ],
  partnerCreateSelectionSession: [
    'Selection sessions',
    '/api/selection-sessions/create',
    'Create a selection session',
  ],
  partnerGetEventInventoryReport: [
    'Reporting',
    '/api/reporting/inventory',
    'Retrieve inventory report',
  ],
  partnerGetEventSalesReport: ['Reporting', '/api/reporting/sales', 'Retrieve sales report'],
  partnerListEventActivity: ['Reporting', '/api/reporting/activity', 'List event activity'],
};

const methods = new Set(['get', 'post', 'patch', 'put', 'delete']);
const dereference = (value) => {
  if (!value?.$ref) return value;
  return value.$ref
    .replace(/^#\//, '')
    .split('/')
    .reduce((node, key) => node?.[key], spec);
};
const validateRefs = (value, location = 'operation') => {
  if (!value || typeof value !== 'object') return;
  if (value.$ref && !dereference(value))
    throw new Error(`Unresolved OpenAPI reference ${value.$ref} in ${location}`);
  for (const [key, child] of Object.entries(value)) validateRefs(child, `${location}.${key}`);
};
const schemaType = (schema) => {
  schema = dereference(schema) || {};
  if (schema.type === 'array') return `array<${schemaType(schema.items)}>`;
  if (schema.enum) return schema.enum.join(' | ');
  const baseType = Array.isArray(schema.type) ? schema.type.join(' | ') : schema.type || 'object';
  return schema.format ? `${baseType} · ${schema.format}` : baseType;
};
const fields = (schema, depth = 0) => {
  schema = dereference(schema) || {};
  if (schema.type === 'array') schema = dereference(schema.items) || {};
  const required = new Set(schema.required || []);
  return Object.entries(schema.properties || {}).map(([name, raw]) => {
    const resolved = dereference(raw) || raw;
    return {
      name,
      required: required.has(name),
      type: schemaType(raw),
      description: resolved.description || '',
      enum: resolved.enum || undefined,
      children: depth < 2 ? fields(raw, depth + 1) : [],
    };
  });
};
const sampleValue = (schema, name = 'value', depth = 0) => {
  schema = dereference(schema) || {};
  if ('example' in schema) return schema.example;
  if ('default' in schema) return schema.default;
  if (schema.enum?.length) return schema.enum[0];
  if (schema.type === 'array') return depth > 2 ? [] : [sampleValue(schema.items, name, depth + 1)];
  if (schema.type === 'object' || schema.properties) {
    const result = {};
    for (const [key, value] of Object.entries(schema.properties || {})) {
      if ((schema.required || []).includes(key) || Object.keys(result).length < 3)
        result[key] = sampleValue(value, key, depth + 1);
    }
    return result;
  }
  if (schema.type === 'integer' || schema.type === 'number') return name === 'quantity' ? 1 : 1000;
  if (schema.type === 'boolean') return true;
  if (schema.format === 'date-time') return '2026-08-23T12:00:00Z';
  const encodedExample = 'AAAAAAAAAAAAAAAAAAAAAA';
  const prefixes = {
    event_id: `evt_${encodedExample}`,
    reservation_id: `res_${encodedExample}`,
    ticket_id: `tkt_${encodedExample}`,
    offer_id: `off_${encodedExample}`,
    checkout_attempt_id: `chk_${encodedExample}`,
  };
  return prefixes[name] || `${name}_example`;
};
const jsonContent = (content = {}) =>
  content['application/json'] || content['application/problem+json'];
const titleFrom = (operationId) =>
  operationId
    .replace(/^partner/, '')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/^./, (value) => value.toUpperCase());

const operations = [];
for (const [path, pathItem] of Object.entries(spec.paths || {})) {
  if (!path.startsWith('/api/v1/partner/')) continue;
  for (const [method, rawOperation] of Object.entries(pathItem)) {
    if (!methods.has(method)) continue;
    const operation = dereference(rawOperation);
    const operationId = operation.operationId;
    if (!routes[operationId])
      throw new Error(`Partner operation ${operationId} has no documentation route`);
    const [group, route, curatedTitle] = routes[operationId];
    validateRefs(operation, operationId);
    for (const requirement of operation.security || spec.security || []) {
      for (const scheme of Object.keys(requirement)) {
        if (!spec.components?.securitySchemes?.[scheme])
          throw new Error(`Unknown security scheme ${scheme} on ${operationId}`);
        if (scheme !== 'PartnerBearer')
          throw new Error(`Non-Partner security scheme ${scheme} leaked into ${operationId}`);
      }
    }
    const parameters = [...(pathItem.parameters || []), ...(operation.parameters || [])].map(
      dereference,
    );
    const bodyContent = jsonContent(dereference(operation.requestBody)?.content);
    const bodySchema = bodyContent?.schema;
    const responses = Object.entries(operation.responses || {}).map(([status, rawResponse]) => {
      const response = dereference(rawResponse);
      const content = jsonContent(response.content);
      return {
        status,
        description: response.description || '',
        fields: fields(content?.schema),
        example:
          content?.example ||
          (content?.schema ? sampleValue(content.schema, 'response') : undefined),
      };
    });
    operations.push({
      operationId,
      group,
      route,
      method: method.toUpperCase(),
      path,
      title: operation.summary || curatedTitle || titleFrom(operationId),
      description: operation.description || '',
      destructive: /Void|Release|ReRelease|PaymentFailure/.test(operationId),
      security: (operation.security || spec.security || []).flatMap((entry) => Object.keys(entry)),
      parameters: parameters.map((parameter) => ({
        name: parameter.name,
        in: parameter.in,
        required: !!parameter.required,
        type: schemaType(parameter.schema),
        description: parameter.description || '',
        example: parameter.example ?? sampleValue(parameter.schema, parameter.name),
      })),
      body: bodySchema
        ? {
            required: !!dereference(operation.requestBody)?.required,
            fields: fields(bodySchema),
            example: bodyContent.example || sampleValue(bodySchema, 'request'),
          }
        : undefined,
      responses,
    });
  }
}

const partnerIds = new Set(operations.map((operation) => operation.operationId));
for (const operationId of Object.keys(routes))
  if (!partnerIds.has(operationId))
    throw new Error(`Documentation route ${operationId} has no Partner operation`);
const routeValues = operations.map((operation) => operation.route);
if (new Set(routeValues).size !== routeValues.length)
  throw new Error('Partner documentation routes must be unique');

const generated = await format(
  `// Generated by scripts/generate-partner-docs.mjs. Do not edit.\nexport const partnerOperations = ${JSON.stringify(operations, null, 2)} as const;\n`,
  { parser: 'typescript', singleQuote: true, trailingComma: 'all', printWidth: 100 },
);
if (process.argv.includes('--check')) {
  const current = await readFile(outputPath, 'utf8').catch(() => '');
  if (current !== generated)
    throw new Error('Partner documentation model is stale; run pnpm docs:generate');
  console.log(
    `Partner docs contract: ${operations.length} operations, ${routeValues.length} unique routes`,
  );
} else {
  await writeFile(outputPath, generated);
  console.log(`Generated ${operations.length} Partner documentation operations`);
}
