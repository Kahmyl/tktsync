import { defineConfig } from '@playwright/test';

const apiOrigin = 'http://localhost:48080';
const authOrigin = 'http://localhost:48081';

export default defineConfig({
  testDir: './tests/e2e',
  testIgnore: '**/live-review/**',
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
    {
      command: `VITE_DOCS_LOCAL_API_BASE_URL=http://localhost:58480 pnpm --filter @tktsync/docs-web dev --host 127.0.0.1 --port 4176 --strictPort`,
      url: 'http://127.0.0.1:4176',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: `VITE_ADMIN_PUBLIC_URL=https://admin.example.test VITE_PARTNER_DEMO_PUBLIC_URL=http://127.0.0.1:4180 VITE_SELECTOR_PUBLIC_URL=https://selector.example.test VITE_SCANNER_PUBLIC_URL=https://scanner.example.test VITE_DOCS_PUBLIC_URL=https://docs.example.test pnpm --filter @tktsync/reviewer-web dev --host 127.0.0.1 --port 4177 --strictPort`,
      url: 'http://127.0.0.1:4177',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: `pnpm --filter @tktsync/partner-demo-web exec vite --host 127.0.0.1 --port 4180 --strictPort`,
      url: 'http://127.0.0.1:4180',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
});
