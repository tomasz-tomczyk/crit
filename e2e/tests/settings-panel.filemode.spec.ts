import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

test.describe('Settings Panel — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('gear icon opens panel to Settings tab', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await expect(page.locator('.settings-tab.active')).toHaveText('Settings');
    await expect(page.locator('.settings-pane[data-pane="settings"]')).toHaveClass(/active/);
  });

  test('? key opens panel to Shortcuts tab', async ({ page }) => {
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await expect(page.locator('.settings-tab.active')).toHaveText('Shortcuts');
  });

  test('Escape closes the panel', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await page.keyboard.press('Escape');
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);
  });

  test('clicking outside closes the panel', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await page.locator('.settings-overlay').click({ position: { x: 10, y: 10 } });
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);
  });

  test('tab switching works', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('.settings-tab[data-tab="shortcuts"]');
    await expect(page.locator('.settings-pane[data-pane="shortcuts"]')).toHaveClass(/active/);
    await page.click('.settings-tab[data-tab="about"]');
    await expect(page.locator('.settings-pane[data-pane="about"]')).toHaveClass(/active/);
    await page.click('.settings-tab[data-tab="settings"]');
    await expect(page.locator('.settings-pane[data-pane="settings"]')).toHaveClass(/active/);
  });

  test('? key toggles shortcuts tab when panel is open on shortcuts', async ({ page }) => {
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);
  });

  test('? key switches to shortcuts tab when panel is open on different tab', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-tab.active')).toHaveText('Settings');
    await page.keyboard.press('?');
    await expect(page.locator('.settings-tab.active')).toHaveText('Shortcuts');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
  });

  test('theme toggle in settings panel changes theme', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="dark"]');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.click('[data-settings-theme="light"]');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });

  test('no old shortcuts overlay in DOM', async ({ page }) => {
    await expect(page.locator('#shortcutsOverlay')).toHaveCount(0);
  });
});
