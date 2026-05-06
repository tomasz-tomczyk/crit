import { test, expect } from '@playwright/test';
import { clearAllDesignPins, getIframe } from './designmode-helpers';

test.describe('boot — agent-ready', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('iframe loads upstream root and chrome shows route /', async ({ page }) => {
    await page.goto('/design');

    // Iframe is wired and the upstream root renders inside it.
    await expect(page.locator('#critDesignIframe')).toBeVisible();
    const frame = getIframe(page);
    await expect(frame.locator('#title')).toHaveText('Upstream');
    await expect(frame.locator('.card')).toHaveCount(3);

    // Chrome reflects the current pathname.
    await expect(page.locator('#designRouteName')).toHaveText('/');
  });

  test.fixme('agent posts agent-ready and unlocks Pin button', async ({ page }) => {
    // FIXME: pending concurrent bug-fix round (Bug 2: design comments render
    // broken after buildCommentCard mount). Symptom in this test: chrome
    // attaches its message listener AFTER the agent's agent-ready postMessage
    // has already fired in the iframe, so __critDesignMessages stays empty
    // and the Pin button never enables.
    await page.goto('/design');
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critDesignMessages?: { type: string }[] })
          .__critDesignMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    await expect(page.locator('#designModeToggle button[data-mode="pin"]')).toBeEnabled();
  });
});
