import path from 'node:path';

export const runId = process.env.LIVE_REVIEW_RUN_ID ?? 'live-e2e-local';
export const repoRoot = process.cwd();
export const reviewRoot = path.resolve(repoRoot, 'artifacts/live-e2e-review', runId);
export const authStatePath = path.join('/tmp', `tktsync-${runId}-auth-state.json`);
export const credentialPath =
  process.env.LIVE_REVIEW_CREDENTIAL_PATH ?? path.join('/tmp', `tktsync-${runId}-credentials.json`);
export const secretStatePath = path.join('/tmp', `tktsync-${runId}-secrets.json`);
export const composeEnvPath = path.join('/tmp', `tktsync-${runId}-compose.env`);
export const cameraPath = path.join('/tmp', `tktsync-${runId}-camera.y4m`);
export const tlsKeyPath = path.join('/tmp', `tktsync-${runId}-receiver.key`);
export const tlsCertPath = path.join('/tmp', `tktsync-${runId}-receiver.crt`);

export const urls = {
  admin: process.env.LIVE_ADMIN_URL ?? 'http://localhost:54470',
  selector: process.env.LIVE_SELECTOR_URL ?? 'http://localhost:54471',
  scanner: process.env.LIVE_SCANNER_URL ?? 'http://localhost:54472',
  docs: process.env.LIVE_DOCS_URL ?? 'http://localhost:54473',
  api: process.env.LIVE_API_URL ?? 'http://localhost:58480',
};

export function parseEnv(raw: string) {
  const values: Record<string, string> = {};
  for (const sourceLine of raw.split(/\r?\n/)) {
    const line = sourceLine.trim();
    if (!line || line.startsWith('#')) continue;
    const equals = line.indexOf('=');
    if (equals < 1) continue;
    const key = line.slice(0, equals).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) continue;
    values[key] = line.slice(equals + 1).trim();
  }
  return values;
}
