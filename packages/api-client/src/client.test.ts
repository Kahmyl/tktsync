import { describe, expect, it } from 'vitest';

import { createTktSyncClient } from './client.js';

describe('generated TktSync API client', () => {
  it('creates a typed OpenAPI client', () => {
    const client = createTktSyncClient('https://tktsync.example');

    expect(client.GET).toBeTypeOf('function');
    expect(client.POST).toBeTypeOf('function');
    expect(client.PATCH).toBeTypeOf('function');
    expect(client.PUT).toBeTypeOf('function');
  });
});
