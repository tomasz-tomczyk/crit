import { test, expect } from '@playwright/test';

test.skip(true, 'phase F design-mode runner brings these online');

test.describe('design-mode markers', () => {
  test('renders one marker per pin on current pathname', async ({ page }) => {
    await page.goto('/design');
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await expect(frame.locator('#crit-marker-root')).toBeAttached();
  });

  test('marker positions update when DOM mutates', async ({ page }) => {
    await page.goto('/design');
    const frame = page.frameLocator('iframe.crit-design-iframe');
    const before = await frame.locator('.crit-design-marker').first().boundingBox();
    await frame.locator('#shift-down-btn').click();
    await expect.poll(async () => {
      const b = await frame.locator('.crit-design-marker').first().boundingBox();
      return b ? b.y : null;
    }).not.toEqual(before?.y);
  });

  test('200-mutation budget triggers full re-resolve', async ({ page }) => {
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await frame.locator('#mass-mutate-btn').click();
    const marker = frame.locator('.crit-design-marker').first();
    await expect(marker).toBeVisible();
  });

  test('attribute changes do NOT trigger reposition', async ({ page }) => {
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await page.evaluate(() => ((window as any).__critDesignMessages = []));
    await frame.locator('#class-thrash-btn').click();
    const msgs = await page.evaluate(() => (window as any).__critDesignMessages || []);
    expect(msgs.filter((m: any) => m.type === 'pin-resolution-result')).toHaveLength(0);
  });

  test('marker click posts pin-clicked', async ({ page }) => {
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await frame.locator('.crit-design-marker').first().click();
    await expect(page.locator('[data-comment-id]')).toHaveClass(/crit-design-thread-highlight/);
  });

  test('Enter key on marker activates same as click', async ({ page }) => {
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await frame.locator('.crit-design-marker').first().focus();
    await frame.locator('.crit-design-marker').first().press('Enter');
    await expect(page.locator('[data-comment-id]')).toHaveClass(/crit-design-thread-highlight/);
  });

  test('drifted-recoverable shows in tray with re-anchor button', async ({ page }) => {
    await page.goto('/design');
    const tray = page.locator('.crit-design-drifted-tray');
    await expect(tray).toBeVisible();
    await expect(tray.locator('.crit-design-drifted-badge--recoverable')).toHaveCount(1);
    await expect(tray.locator('.crit-design-reanchor-btn')).toHaveCount(1);
  });

  test('drifted (lost) shows badge but no re-anchor button', async ({ page }) => {
    const tray = page.locator('.crit-design-drifted-tray');
    await expect(tray.locator('.crit-design-drifted-badge--lost')).toHaveCount(1);
    await expect(tray.locator('.crit-design-reanchor-btn')).toHaveCount(0);
  });

  test('clicking re-anchor button → next iframe click updates pin and tray clears', async ({ page }) => {
    await page.locator('.crit-design-reanchor-btn').first().click();
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await frame.locator('#new-target').click();
    await expect(page.locator('.crit-design-drifted-tray .crit-design-drifted-row')).toHaveCount(0);
  });

  test('re-anchor flow updates pin via PUT', async ({ page }) => {
    let putHit = false;
    await page.route('**/api/comment/**', (route) => {
      if (route.request().method() === 'PUT') putHit = true;
      route.continue();
    });
    await page.locator('.crit-design-reanchor-btn').first().click();
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await frame.locator('#new-target').click();
    await expect.poll(() => putHit).toBe(true);
  });

  test('end-to-end three-status walkthrough', async ({ page }) => {
    await page.goto('/design');
    const tray = page.locator('.crit-design-drifted-tray');
    await expect(tray.locator('.crit-design-drifted-badge--recoverable')).toHaveCount(1);
    await expect(tray.locator('.crit-design-drifted-badge--lost')).toHaveCount(1);
    await tray.locator('.crit-design-reanchor-btn').first().click();
    const frame = page.frameLocator('iframe.crit-design-iframe');
    await frame.locator('#new-target').click();
    await expect(tray.locator('.crit-design-drifted-badge--recoverable')).toHaveCount(0);
  });
});
