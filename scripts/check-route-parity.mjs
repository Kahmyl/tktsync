import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const openapi = JSON.parse(readFileSync(join(root, 'openapi/tktsync.v1.json'), 'utf8'));
const runtime = new Set();
const documented = new Set();

function walk(directory) {
  for (const entry of readdirSync(directory)) {
    const path = join(directory, entry);
    if (statSync(path).isDirectory()) walk(path);
    else if (path.endsWith('.go') && !path.endsWith('_test.go')) {
      const source = readFileSync(path, 'utf8');
      for (const match of source.matchAll(/"(GET|POST|PUT|PATCH|DELETE) (\/api\/v1\/[^" ]+)"/g)) {
        runtime.add(`${match[1]} ${match[2]}`);
      }
    }
  }
}

walk(join(root, 'backend'));
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
console.log(`Route parity complete: ${runtime.size} runtime operations match OpenAPI.`);
