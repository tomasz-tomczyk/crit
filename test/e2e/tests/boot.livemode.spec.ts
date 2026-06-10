import { test, expect } from '@playwright/test';
import { clearAllLivePins, getIframe } from './livemode-helpers';

test.describe('boot — agent-ready', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('iframe loads upstream root and chrome shows route /', async ({ page }) => {
    await page.goto('/live');

    // Iframe is wired and the upstream root renders inside it.
    await expect(page.locator('#critLiveIframe')).toBeVisible();
    const frame = getIframe(page);
    await expect(frame.locator('#title')).toHaveText('Upstream');
    await expect(frame.locator('.card')).toHaveCount(3);

    // Chrome reflects the current pathname.
    await expect(page.locator('#liveRouteName')).toHaveText('/');
  });

  test('agent posts agent-ready and unlocks Pin button', async ({ page }) => {
    await page.goto('/live');
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).toBeEnabled();
  });
});
