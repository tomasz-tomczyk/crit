import { test, expect } from '@playwright/test';
import { clearAllLivePins } from './livemode-helpers';

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
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).not.toHaveClass(/active/);
    await page.locator('.settings-overlay').click({ position: { x: 10, y: 10 } });

    await page.keyboard.press('p');
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).not.toHaveClass(/active/);
    await page.keyboard.press('x');
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).toHaveClass(/active/);

    await page.keyboard.press('?');
    await page.locator('.shortcut-reset-all').click();
  });
});
