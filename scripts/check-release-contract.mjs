import { readFileSync } from 'node:fs';

const root = new URL('../', import.meta.url);
const spec = JSON.parse(readFileSync(new URL('openapi/tktsync.v1.json', root), 'utf8'));
const lifecycle = [
  ['pause-sales', 'adminPauseSales'],
  ['resume-sales', 'adminResumeSales'],
  ['close-sales', 'adminCloseSales'],
  ['cancel', 'adminCancelEvent'],
  ['complete', 'adminCompleteEvent'],
];
for (const [suffix, operationId] of lifecycle) {
  const operation = spec.paths[`/api/v1/admin/events/{event_id}/${suffix}`]?.post;
  if (!operation || operation.operationId !== operationId)
    throw new Error(`missing ${operationId}`);
  if (!operation.security?.some((entry) => Object.hasOwn(entry, 'AdminBearer'))) {
    throw new Error(`${operationId} must use AdminBearer`);
  }
  const parameterRefs = new Set(operation.parameters?.map((entry) => entry.$ref));
  for (const required of [
    '#/components/parameters/EventID',
    '#/components/parameters/IdempotencyKey',
  ]) {
    if (!parameterRefs.has(required)) throw new Error(`${operationId} is missing ${required}`);
  }
}
const cancelSchema = spec.components.schemas.CancelEventRequest;
if (
  !cancelSchema?.required?.includes('reason') ||
  cancelSchema.properties?.reason?.minLength !== 1
) {
  throw new Error('cancellation reason is not required by the contract');
}
const htmlFiles = [
  'apps/admin-web/index.html',
  'apps/selector-web/index.html',
  'apps/scanner-web/index.html',
];
for (const file of htmlFiles) {
  const html = readFileSync(new URL(file, root), 'utf8');
  if (
    !/Content-Security-Policy/i.test(html) ||
    !/name=["']referrer["'][^>]*content=["']no-referrer["']/i.test(html)
  ) {
    throw new Error(`${file} must define CSP and no-referrer policy`);
  }
}
const runbook = readFileSync(new URL('docs/operations/release-runbook.md', root), 'utf8');
for (const section of [
  'Deployment',
  'Rollback',
  'Backup',
  'Recovery',
  'Key rotation',
  'Alert response',
]) {
  if (!runbook.includes(`## ${section}`)) throw new Error(`operations runbook missing ${section}`);
}
console.log('Release lifecycle, browser-security, and operations contract assertions passed.');
