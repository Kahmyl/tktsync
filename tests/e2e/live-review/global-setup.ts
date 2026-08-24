import { execFileSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import {
  authStatePath,
  composeEnvPath,
  credentialPath,
  parseEnv,
  repoRoot,
  reviewRoot,
  runId,
  secretStatePath,
  urls,
} from './config';
import { writeRealTicketCameraFixture } from './state';

type SupabaseSession = {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  expires_at?: number;
  token_type: string;
  user: { id: string; [key: string]: unknown };
};

type LiveCredentials = { email: string; password: string };

const storageKey = (supabaseURL: string) =>
  `sb-${new URL(supabaseURL).hostname.split('.')[0]}-auth-token`;

async function exists(file: string) {
  try {
    await stat(file);
    return true;
  } catch {
    return false;
  }
}

async function prepareRealTicketCameraFixture() {
  if (!(await exists(secretStatePath))) return;
  const secrets = JSON.parse(await readFile(secretStatePath, 'utf8')) as Record<string, string>;
  const payload = secrets.ticket1QR;
  if (!payload) return;

  await writeRealTicketCameraFixture(payload);
}

async function prepareRuntime(rootEnv: string) {
  if (!(await exists(composeEnvPath))) {
    const key = () => randomBytes(32).toString('base64url');
    const runtime = [
      rootEnv.trimEnd(),
      '',
      '# Ephemeral live-review runtime. Never commit this file.',
      'REALTIME_ENABLED=true',
      'WEBHOOK_ENABLED=true',
      'SELECTION_KEYRING_ACTIVE_VERSION=1',
      `SELECTION_KEYRING_KEYS=1:${key()}`,
      'RESERVATION_KEYRING_ACTIVE_VERSION=1',
      `RESERVATION_KEYRING_KEYS=1:${key()}`,
      'QR_KEYRING_ACTIVE_VERSION=1',
      `QR_KEYRING_KEYS=1:${key()}`,
      `PARTNER_CREDENTIAL_REPLAY_KEY=${key()}`,
      'WEBHOOK_ENCRYPTION_KEY_VERSION=1',
      `WEBHOOK_ENCRYPTION_KEY=${key()}`,
      '',
    ].join('\n');
    await writeFile(composeEnvPath, runtime, { mode: 0o600 });
  }

  execFileSync(
    'docker',
    [
      'compose',
      '--env-file',
      composeEnvPath,
      'up',
      '-d',
      '--build',
      '--force-recreate',
      '--wait',
      '--wait-timeout',
      '60',
      'api',
      'worker',
      'docs',
    ],
    { cwd: repoRoot, stdio: 'ignore' },
  );
}

async function createRealSession(supabaseURL: string, anonKey: string): Promise<SupabaseSession> {
  const response = await fetch(`${supabaseURL}/auth/v1/signup`, {
    method: 'POST',
    headers: {
      apikey: anonKey,
      Authorization: `Bearer ${anonKey}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ data: { purpose: 'tktsync-live-e2e', run_id: runId } }),
  });
  const body = (await response.json()) as SupabaseSession & {
    error_code?: string;
    msg?: string;
  };
  if (!response.ok || !body.access_token || !body.user?.id) {
    throw new Error(
      `Real Supabase anonymous sign-in failed (${response.status} ${body.error_code ?? 'UNKNOWN'}): ${body.msg ?? 'request rejected'}`,
    );
  }
  return body;
}

async function createPasswordSession(
  supabaseURL: string,
  anonKey: string,
): Promise<SupabaseSession> {
  const credentials = JSON.parse(await readFile(credentialPath, 'utf8')) as LiveCredentials;
  if (!credentials.email || !credentials.password) {
    throw new Error('Live-review credential file is incomplete');
  }
  const response = await fetch(`${supabaseURL}/auth/v1/token?grant_type=password`, {
    method: 'POST',
    headers: {
      apikey: anonKey,
      Authorization: `Bearer ${anonKey}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(credentials),
  });
  const body = (await response.json()) as SupabaseSession & {
    error_code?: string;
    msg?: string;
  };
  if (!response.ok || !body.access_token || !body.user?.id) {
    throw new Error(
      `Real Supabase password sign-in failed (${response.status} ${body.error_code ?? 'UNKNOWN'})`,
    );
  }
  return body;
}

async function readUsableSession(): Promise<SupabaseSession | undefined> {
  if (!(await exists(authStatePath))) return undefined;
  const state = JSON.parse(await readFile(authStatePath, 'utf8')) as {
    origins?: Array<{ localStorage?: Array<{ name: string; value: string }> }>;
  };
  for (const origin of state.origins ?? []) {
    for (const item of origin.localStorage ?? []) {
      if (!item.name.endsWith('-auth-token')) continue;
      const session = JSON.parse(item.value) as SupabaseSession;
      if ((session.expires_at ?? 0) > Date.now() / 1000 + 300) return session;
    }
  }
  return undefined;
}

async function writeAuthState(session: SupabaseSession, supabaseURL: string) {
  const entry = { name: storageKey(supabaseURL), value: JSON.stringify(session) };
  await writeFile(
    authStatePath,
    JSON.stringify(
      {
        cookies: [],
        origins: [
          { origin: urls.admin, localStorage: [entry] },
          { origin: urls.scanner, localStorage: [entry] },
        ],
      },
      null,
      2,
    ),
    { mode: 0o600 },
  );
}

async function writeEmptyAuthState() {
  await writeFile(
    authStatePath,
    JSON.stringify(
      {
        cookies: [],
        origins: [
          { origin: urls.admin, localStorage: [] },
          { origin: urls.scanner, localStorage: [] },
        ],
      },
      null,
      2,
    ),
    { mode: 0o600 },
  );
}

function authorizeLocally(subject: string) {
  const sql = `
    WITH upserted AS (
      INSERT INTO app_users (auth_provider, auth_subject, display_name, state)
      VALUES ('supabase', :'auth_subject', :'display_name', 'ACTIVE')
      ON CONFLICT (auth_provider, auth_subject) DO UPDATE
      SET display_name = EXCLUDED.display_name, state = 'ACTIVE', updated_at = now()
      RETURNING id
    )
    INSERT INTO platform_user_roles (user_id, role)
    SELECT id, 'PLATFORM_ADMIN' FROM upserted
    ON CONFLICT (user_id, role) DO NOTHING;
  `;
  execFileSync(
    'docker',
    [
      'compose',
      '--env-file',
      composeEnvPath,
      'exec',
      '-T',
      'postgres',
      'psql',
      '-v',
      'ON_ERROR_STOP=1',
      '-v',
      `auth_subject=${subject}`,
      '-v',
      `display_name=E2E ${runId} Operator`,
      '-U',
      'tktsync',
      '-d',
      'tktsync',
      '-c',
      sql,
    ],
    { cwd: repoRoot, stdio: 'ignore' },
  );
}

export default async function globalSetup() {
  await Promise.all(
    ['videos', 'screenshots', 'logs', 'html-report', 'test-results', 'sensitive-failures'].map(
      (directory) => mkdir(path.join(reviewRoot, directory), { recursive: true }),
    ),
  );

  const entityPath = path.join(reviewRoot, 'entities.json');
  if (!(await exists(entityPath))) {
    await writeFile(
      entityPath,
      JSON.stringify(
        {
          run_id: runId,
          venues: [],
          layouts: [],
          events: [],
          partners: [],
          partner_credentials: [],
          webhook_endpoints: [],
          reservations: [],
          tickets: [],
          admissions: [],
        },
        null,
        2,
      ),
    );
  }
  const issuePath = path.join(reviewRoot, 'issues.md');
  if (!(await exists(issuePath))) {
    await writeFile(
      issuePath,
      '# Issues found during the authenticated live review\n\nOriginal failures remain documented after remediation.\n',
    );
  }

  const rootEnv = await readFile(path.join(repoRoot, '.env'), 'utf8');
  const values = parseEnv(rootEnv);
  const supabaseURL = values.SUPABASE_URL;
  const anonKey = values.SUPABASE_ANON_KEY;
  if (!supabaseURL || !anonKey)
    throw new Error('Configured Supabase public values are unavailable');

  await prepareRuntime(rootEnv);
  await prepareRealTicketCameraFixture();
  let auth = 'unavailable';
  let authUI = 'blocked: no reusable email/password credential in local configuration';
  if (await exists(credentialPath)) {
    const session = await createPasswordSession(supabaseURL, anonKey);
    await writeAuthState(session, supabaseURL);
    auth = 'real Supabase password session';
    authUI = 'real email/password; visible UI sign-in verified in Workflow 1';
  } else {
    try {
      const session =
        (await readUsableSession()) ?? (await createRealSession(supabaseURL, anonKey));
      await writeAuthState(session, supabaseURL);
      authorizeLocally(session.user.id);
      auth = 'real Supabase anonymous session; local platform-admin bootstrap';
      authUI = 'blocked: no reusable email/password credential in local configuration';
    } catch (error) {
      await writeEmptyAuthState();
      const message = error instanceof Error ? error.message : 'real authentication unavailable';
      auth = `blocked: ${message}`;
      authUI = 'blocked: no reusable email/password credential and anonymous sign-ins are disabled';
    }
  }

  const composeState = execFileSync('docker', ['compose', '--env-file', composeEnvPath, 'ps'], {
    cwd: repoRoot,
    encoding: 'utf8',
  });
  await writeFile(path.join(reviewRoot, 'logs', 'compose-start.txt'), composeState);
  await writeFile(
    path.join(reviewRoot, 'runtime.json'),
    JSON.stringify(
      {
        run_id: runId,
        auth,
        auth_ui_success: authUI,
        realtime_enabled_for_review: true,
        webhooks_enabled_for_review: true,
        urls,
      },
      null,
      2,
    ),
  );
}
