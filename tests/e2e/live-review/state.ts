import { execFileSync } from 'node:child_process';
import { appendFile, chmod, readFile, rename, writeFile } from 'node:fs/promises';
import path from 'node:path';
import type { Page } from '@playwright/test';
import {
  authStatePath,
  composeEnvPath,
  repoRoot,
  reviewRoot,
  runId,
  secretStatePath,
  urls,
} from './config';

type SecretState = Record<string, string>;

export type EntityLedger = {
  run_id: string;
  venues: string[];
  layouts: string[];
  events: string[];
  partners: string[];
  partner_credentials: string[];
  webhook_endpoints: string[];
  reservations: string[];
  tickets: string[];
  admissions: string[];
};

const entityPath = path.join(reviewRoot, 'entities.json');

export async function operatorToken() {
  const state = JSON.parse(await readFile(authStatePath, 'utf8')) as {
    origins: Array<{ localStorage: Array<{ name: string; value: string }> }>;
  };
  const item = state.origins
    .flatMap((origin) => origin.localStorage)
    .find((entry) => entry.name.endsWith('-auth-token'));
  if (!item) throw new Error('Real Supabase session is unavailable');
  const session = JSON.parse(item.value) as { access_token: string };
  return session.access_token;
}

export async function readSecrets(): Promise<SecretState> {
  try {
    return JSON.parse(await readFile(secretStatePath, 'utf8')) as SecretState;
  } catch {
    return {};
  }
}

export async function setSecret(name: string, value: string) {
  const secrets = await readSecrets();
  secrets[name] = value;
  await writeFile(secretStatePath, JSON.stringify(secrets), { mode: 0o600 });
  await chmod(secretStatePath, 0o600);
}

export async function getSecret(name: string) {
  const value = (await readSecrets())[name];
  if (!value) throw new Error(`Required in-memory review secret is unavailable: ${name}`);
  return value;
}

export async function addEntity(kind: keyof Omit<EntityLedger, 'run_id'>, id: string) {
  const ledger = JSON.parse(await readFile(entityPath, 'utf8')) as EntityLedger;
  if (!ledger[kind].includes(id)) ledger[kind].push(id);
  await writeFile(entityPath, JSON.stringify(ledger, null, 2));
}

export async function entities() {
  return JSON.parse(await readFile(entityPath, 'utf8')) as EntityLedger;
}

export async function apiJSON<T>(
  pathName: string,
  options: {
    method?: string;
    token?: string;
    partnerCredential?: string;
    reservationToken?: string;
    body?: unknown;
    idempotencyKey?: string;
  } = {},
): Promise<{ data: T; status: number; requestId: string | null }> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const bearer = options.partnerCredential ?? options.token;
  if (bearer) headers.Authorization = `Bearer ${bearer}`;
  if (options.reservationToken) headers['X-TktSync-Reservation-Token'] = options.reservationToken;
  if (options.idempotencyKey) headers['Idempotency-Key'] = options.idempotencyKey;
  if (options.body !== undefined) headers['Content-Type'] = 'application/json';
  const response = await fetch(`${urls.api}${pathName}`, {
    method: options.method ?? 'GET',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  const text = await response.text();
  const data = text ? (JSON.parse(text) as T) : ({} as T);
  if (!response.ok) {
    const safe = data as { error?: { code?: string; message?: string } };
    throw new Error(
      `Live API ${options.method ?? 'GET'} ${pathName} failed (${response.status} ${safe.error?.code ?? 'UNKNOWN'}): ${safe.error?.message ?? 'request failed'}`,
    );
  }
  return { data, status: response.status, requestId: response.headers.get('x-request-id') };
}

export async function screenshot(page: Page, name: string, fullPage = false) {
  await page.screenshot({ path: path.join(reviewRoot, 'screenshots', `${name}.png`), fullPage });
}

export async function saveVideo(page: Page, name: string) {
  const video = page.video();
  if (!page.isClosed()) await page.close();
  if (video) await video.saveAs(path.join(reviewRoot, 'videos', name));
}

export async function recordIssue(title: string, details: string) {
  const issuePath = path.join(reviewRoot, 'issues.md');
  const current = await readFile(issuePath, 'utf8');
  if (current.includes(`## ${title}`)) return;
  await appendFile(issuePath, `\n## ${title}\n\n${details.trim()}\n`);
}

export function queryDatabase(sql: string) {
  return execFileSync(
    'docker',
    [
      'compose',
      '--env-file',
      composeEnvPath,
      'exec',
      '-T',
      'postgres',
      'psql',
      '-At',
      '-U',
      'tktsync',
      '-d',
      'tktsync',
      '-c',
      sql,
    ],
    { cwd: repoRoot, encoding: 'utf8' },
  ).trim();
}

export async function renameFirstVideo(fromDirectory: string, name: string) {
  const { readdir } = await import('node:fs/promises');
  const files = (await readdir(fromDirectory)).filter((file) => file.endsWith('.webm'));
  if (files[0])
    await rename(path.join(fromDirectory, files[0]), path.join(reviewRoot, 'videos', name));
}

export const reviewNames = {
  venue: `E2E ${runId} Venue`,
  event: `E2E ${runId} Championship Night`,
  wrongEvent: `E2E ${runId} Wrong Event`,
  partner: `E2E ${runId} Partner`,
  operator: `E2E ${runId} Operator`,
};
