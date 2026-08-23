import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const contract = JSON.parse(
  readFileSync(resolve(import.meta.dirname, '../openapi/tktsync.v1.json'), 'utf8'),
);

const adminRoutes = [
  '/api/v1/admin/events/{event_id}/reports/inventory',
  '/api/v1/admin/events/{event_id}/reports/sales',
  '/api/v1/admin/events/{event_id}/reports/admission',
  '/api/v1/admin/events/{event_id}/audit',
  '/api/v1/admin/events/{event_id}/accreditation-export',
  '/api/v1/admin/events/{event_id}/metrics',
];
const partnerRoutes = [
  '/api/v1/partner/events/{event_id}/reports/inventory',
  '/api/v1/partner/events/{event_id}/reports/sales',
  '/api/v1/partner/events/{event_id}/activity',
];

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

for (const path of adminRoutes) {
  const item = contract.paths[path];
  assert(item?.get, `missing Reporting GET ${path}`);
  assert(Object.keys(item).length === 1, `Reporting report path must be read-only: ${path}`);
  assert(
    item.get.security?.some((entry) => 'AdminBearer' in entry),
    `missing AdminBearer: ${path}`,
  );
  assert(
    item.get.parameters?.some((entry) => entry.$ref?.endsWith('/EventID')),
    `missing EventID: ${path}`,
  );
}
for (const path of partnerRoutes) {
  const operation = contract.paths[path]?.get;
  assert(operation, `missing Reporting GET ${path}`);
  assert(
    operation.security?.some((entry) => 'PartnerBearer' in entry),
    `missing PartnerBearer: ${path}`,
  );
}

const auditParameters = contract.paths['/api/v1/admin/events/{event_id}/audit'].get.parameters;
for (const name of [
  'limit',
  'cursor',
  'operation',
  'entity_type',
  'actor_kind',
  'reservation_id',
  'sale_id',
  'ticket_id',
  'correlation_id',
  'from',
  'to',
  'search',
]) {
  assert(
    auditParameters.some((parameter) => parameter.name === name),
    `missing audit parameter ${name}`,
  );
}
const csv =
  contract.paths['/api/v1/admin/events/{event_id}/accreditation-export'].get.responses['200'];
assert(csv.content?.['text/csv'], 'accreditation export must declare text/csv');
assert(
  csv.headers?.['X-TktSync-Generated-At'],
  'accreditation export must declare generation timestamp',
);

const dimensions = contract.components.schemas.InventoryDimensions;
for (const field of [
  'available',
  'held',
  'committing',
  'payment_retry',
  'reconciling',
  'blocked',
  'allocated',
  'sold_current',
  'issued_current',
  'voided_tickets',
  'capacity_consumed',
  'historical_sold',
  'historical_issued',
]) {
  assert(dimensions.required.includes(field), `missing inventory reporting dimension ${field}`);
}
assert(
  contract.components.schemas.OperationalMetrics.properties.authority.const ===
    'ADVISORY_DERIVED_READ',
  'metrics must be explicitly advisory',
);

console.log('Reporting contract assertions complete.');
