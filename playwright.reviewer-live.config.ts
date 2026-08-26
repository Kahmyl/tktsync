import { defineConfig } from '@playwright/test';

const runId = process.env.LIVE_REVIEW_RUN_ID ?? 'reviewer-demo-final';

export default defineConfig({
  testDir: './tests/e2e/live-review',
  testMatch: '08-reviewer-demo-experience.spec.ts',
  timeout: 240_000,
  expect: { timeout: 15_000 },
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    headless: true,
    ignoreHTTPSErrors: true,
    storageState: `/tmp/tktsync-${runId}-auth-state.json`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
});
