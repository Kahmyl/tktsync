import { defineConfig } from '@playwright/test';
import path from 'node:path';

const runId = process.env.LIVE_REVIEW_RUN_ID ?? 'live-e2e-local';
const reviewRoot = path.resolve('artifacts/live-e2e-review', runId);

export default defineConfig({
  testDir: './tests/e2e/live-review',
  timeout: 600_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  globalSetup: './tests/e2e/live-review/global-setup.ts',
  reporter: [
    ['list'],
    ['html', { outputFolder: path.join(reviewRoot, 'html-report'), open: 'never' }],
  ],
  outputDir: path.join(reviewRoot, 'test-results'),
  use: {
    headless: true,
    ignoreHTTPSErrors: true,
    storageState: path.join('/tmp', `tktsync-${runId}-auth-state.json`),
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'on',
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
    launchOptions: {
      slowMo: Number(process.env.LIVE_REVIEW_SLOW_MO ?? 450),
      args: [
        '--use-fake-device-for-media-stream',
        '--use-fake-ui-for-media-stream',
        `--use-file-for-fake-video-capture=${path.join('/tmp', `tktsync-${runId}-camera.y4m`)}`,
      ],
    },
  },
});
