import createClient from 'openapi-fetch';

import type { paths } from './generated/schema.js';

export function createTktSyncClient(baseUrl: string) {
  return createClient<paths>({ baseUrl });
}

export type TktSyncClient = ReturnType<typeof createTktSyncClient>;
