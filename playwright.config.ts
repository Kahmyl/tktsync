import { defineConfig } from '@playwright/test';

const apiOrigin = 'http://localhost:48080';
const authOrigin = 'http://localhost:48081';

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  expect: {
    timeout: 7_500,
  },
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  outputDir: '/tmp/tktsync-playwright-results',
  use: {
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: [
    {
      command: `VITE_API_BASE_URL=${apiOrigin} VITE_SUPABASE_URL=${authOrigin} VITE_SUPABASE_ANON_KEY=test-anon pnpm --filter @tktsync/admin-web dev --host 127.0.0.1 --port 4173 --strictPort`,
      url: 'http://127.0.0.1:4173',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: `VITE_API_BASE_URL=${apiOrigin} pnpm --filter @tktsync/selector-web dev --host 127.0.0.1 --port 4174 --strictPort`,
      url: 'http://127.0.0.1:4174',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: `VITE_API_BASE_URL=${apiOrigin} VITE_SUPABASE_URL=${authOrigin} VITE_SUPABASE_ANON_KEY=test-anon pnpm --filter @tktsync/scanner-web dev --host 127.0.0.1 --port 4175 --strictPort`,
      url: 'http://127.0.0.1:4175',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
});
