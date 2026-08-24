import { expect, test } from '@playwright/test';
import { saveVideo, screenshot } from './state';
import { urls } from './config';

const viewports = [
  [1440, 900],
  [1280, 800],
  [1024, 768],
  [768, 1024],
  [430, 932],
  [390, 844],
  [360, 800],
] as const;

test('06 Responsive live smoke', async ({ page }) => {
  test.setTimeout(240_000);
  const surfaces = [
    { id: 'admin-sign-in', url: `${urls.admin}/sign-in`, marker: 'Welcome back' },
    {
      id: 'docs-request',
      url: `${urls.docs}/api/events/retrieve`,
      marker: 'Retrieve an Event',
    },
    { id: 'selector-invalid', url: urls.selector, marker: 'This ticket selection link' },
    { id: 'scanner-sign-in', url: urls.scanner, marker: 'Scanner sign in' },
  ];

  for (const [width, height] of viewports) {
    await page.setViewportSize({ width, height });
    for (const surface of surfaces) {
      await test.step(`${surface.id} at ${width}x${height}`, async () => {
        await page.goto(surface.url);
        await expect(page.getByText(surface.marker, { exact: false }).first()).toBeVisible();
        const overflow = await page.evaluate(
          () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
        );
        expect(overflow).toBeLessThanOrEqual(0);
        await screenshot(page, `responsive-${surface.id}-${width}x${height}`);
      });
    }
  }
  await saveVideo(page, '06-responsive-smoke.webm');
});
