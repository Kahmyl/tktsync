import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { execFileSync } from 'node:child_process';

const root = resolve(import.meta.dirname, '..');
const openapi = JSON.parse(readFileSync(join(root, 'openapi/tktsync.v1.json'), 'utf8'));
const runtime = new Set();
const documented = new Set();

const registrations = execFileSync(
  'go',
  ['run', join(root, 'scripts/registered-routes.go'), join(root, 'backend')],
  {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, GOCACHE: process.env.GOCACHE || '/tmp/tktsync-route-gocache' },
  },
);
for (const route of registrations
  .split('\n')
  .map((value) => value.trim())
  .filter(Boolean)) {
  if (runtime.has(route)) {
    console.error(`Duplicate runtime route registration: ${route}`);
    process.exit(1);
  }
  runtime.add(route);
}
for (const [path, operations] of Object.entries(openapi.paths)) {
  for (const method of Object.keys(operations)) {
    if (['get', 'post', 'put', 'patch', 'delete'].includes(method)) {
      documented.add(`${method.toUpperCase()} ${path}`);
    }
  }
}

const missingFromContract = [...runtime].filter((route) => !documented.has(route)).sort();
const missingFromRuntime = [...documented].filter((route) => !runtime.has(route)).sort();
if (missingFromContract.length || missingFromRuntime.length) {
  if (missingFromContract.length)
    console.error('Runtime-only routes:\n' + missingFromContract.join('\n'));
  if (missingFromRuntime.length)
    console.error('OpenAPI-only routes:\n' + missingFromRuntime.join('\n'));
  process.exit(1);
}
console.log(
  `Route parity complete: ${runtime.size} registered ServeMux operations match OpenAPI (Go AST registration audit).`,
);
