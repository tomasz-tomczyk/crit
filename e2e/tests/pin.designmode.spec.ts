import { test, expect } from '@playwright/test';

// Phase F runner not online yet. Scenarios scaffolded for later activation.

test.describe('design-mode pin composer — M11 sustained highlight', () => {
  // Pin-mode scenarios depend on the agent-ready handshake which is currently
  // broken (Bug 2 in concurrent fix-round). Marked fixme so failures are
  // tracked, not silenced.
  test.fixme(true, 'pending agent-ready handshake (Bug 2)');

  test('clicking an element in Pin mode keeps it outlined while composer is open', async ({ page }) => {
    await page.goto('/design');
    // Switch to Pin mode (M5)
    await page.locator('[data-mode="pin"]').click();
    // Click an element inside the iframe (placeholder selector — depends on
    // Phase F fixture target site).
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    // Composer should be visible
    await expect(page.locator('.crit-design-composer')).toBeVisible();
    // Captured element should carry the pending-highlight class
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(1);
  });

  test('Cancel removes the highlight', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(1);
    await page.locator('.crit-design-composer-cancel').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('Escape removes the highlight', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    await page.locator('.crit-design-composer-body').focus();
    await page.keyboard.press('Escape');
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('Save removes the highlight after the comment is created', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    await page.locator('.crit-design-composer-body').fill('A pin comment.');
    await page.locator('.crit-design-composer-save').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('route change auto-clears the highlight', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    // Trigger an SPA navigation inside the iframe.
    await iframe.locator('[data-test="nav-other"]').click();
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });
});

test.describe('design-mode composer — M16 keyboard shortcuts', () => {
  // Pin-mode scenarios depend on the agent-ready handshake which is currently
  // broken (Bug 2 in concurrent fix-round). Marked fixme so failures are
  // tracked, not silenced.
  test.fixme(true, 'pending agent-ready handshake (Bug 2)');

  test('Cmd/Ctrl+Enter submits the composer', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    const ta = page.locator('.crit-design-composer-body');
    await ta.fill('Submit via shortcut');
    await ta.press('Meta+Enter'); // Ctrl+Enter on non-mac
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
  });

  test('reply composer in panel honours the same shortcuts', async ({ page }) => {
    await page.goto('/design');
    const reply = page.locator('#commentsPanelBody [data-action="reply"]').first();
    await reply.click();
    const ta = page.locator('#commentsPanelBody textarea').first();
    await ta.fill('A reply');
    await ta.press('Meta+Enter');
    await expect(page.locator('#commentsPanelBody textarea')).toHaveCount(0);
  });
});

test.describe('design-mode composer — M17 confirm-before-discard on Esc', () => {
  // Pin-mode scenarios depend on the agent-ready handshake which is currently
  // broken (Bug 2 in concurrent fix-round). Marked fixme so failures are
  // tracked, not silenced.
  test.fixme(true, 'pending agent-ready handshake (Bug 2)');

  test('Esc on empty composer cancels immediately', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    await page.locator('.crit-design-composer-body').focus();
    await page.keyboard.press('Escape');
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
  });

  test('Esc on dirty composer triggers confirm and respects user choice', async ({ page }) => {
    await page.goto('/design');
    await page.locator('[data-mode="pin"]').click();
    const iframe = page.frameLocator('#critDesignIframe');
    await iframe.locator('[data-test="pin-target"]').click();
    const ta = page.locator('.crit-design-composer-body');
    await ta.fill('partial draft');

    // Decline confirm — composer must remain.
    page.once('dialog', async (d) => { await d.dismiss(); });
    await ta.press('Escape');
    await expect(page.locator('.crit-design-composer')).toBeVisible();

    // Accept confirm — composer closes, highlight cleared.
    page.once('dialog', async (d) => { await d.accept(); });
    await ta.press('Escape');
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await expect(iframe.locator('.crit-design-pending-highlight')).toHaveCount(0);
  });
});
