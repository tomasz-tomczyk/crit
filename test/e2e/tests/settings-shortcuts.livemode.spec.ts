import { test, expect } from '@playwright/test';
import { clearAllLivePins, getIframe } from './livemode-helpers';

test.describe('Settings shortcuts — Live and Preview controller', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllLivePins(request);
    await page.goto('/live');
  });

  test('custom shortcut can be set and used from live mode', async ({ page }) => {
    await page.locator('#settingsToggle').click();
    await page.locator('.settings-tab[data-tab="shortcuts"]').click();
    const togglePinMode = page.locator('[data-shortcut-id="toggle_pin_mode"]');
    await expect(togglePinMode).toBeVisible();
    await togglePinMode.click();
    await page.keyboard.press('x');
    await page.keyboard.press('x');
    await expect(page.locator('#liveModeShortcut')).toHaveText('X');
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).not.toHaveClass(/active/);
    await page.locator('.settings-overlay').click({ position: { x: 10, y: 10 } });

    await page.keyboard.press('p');
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).not.toHaveClass(/active/);
    await page.keyboard.press('x');
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).toHaveClass(/active/);

    // The iframe relays safe key details to the chrome, which must resolve
    // the configured binding rather than assuming the default P key.
    await page.locator('#liveModeToggle button[data-mode="navigate"]').click();
    const target = getIframe(page).locator('#primary-btn');
    await target.focus();
    await target.press('p');
    await expect(page.locator('#liveModeToggle button[data-mode="navigate"]')).toHaveClass(/active/);
    await target.press('x');
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).toHaveClass(/active/);

    await page.locator('#settingsToggle').click();
    await page.locator('.settings-tab[data-tab="shortcuts"]').click();
    await page.locator('.shortcut-reset-all').click();
  });
});
