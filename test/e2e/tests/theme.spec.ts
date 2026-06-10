import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Theme Tests (git mode)
//
// Theme toggle has moved from the header into the settings panel.
// Open the panel first, then interact with theme buttons via
// [data-settings-theme="..."] selectors.
// ============================================================
test.describe('Theme — Git Mode', () => {
  test.beforeEach(async ({ page, context, request }) => {
    await clearAllComments(request);
    // Clear theme cookie before each test
    await context.clearCookies();
    await loadPage(page);
  });

  test('clicking light theme button sets data-theme="light" on <html>', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="light"]');

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('light');
  });

  test('clicking dark theme button sets data-theme="dark" on <html>', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="dark"]');

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('dark');
  });

  test('clicking system theme button removes data-theme from <html>', async ({ page }) => {
    // First set to dark, then switch to system
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="dark"]');
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    await page.click('[data-settings-theme="system"]');

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBeNull();
  });

  test('theme persists across page reload', async ({ page }) => {
    // Set dark theme
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="dark"]');
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    // Reload the page
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Should still be dark
    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('dark');
  });

  test('theme pill indicator moves when theme changes', async ({ page }) => {
    await page.click('#settingsToggle');
    const indicator = page.locator('#settingsThemeIndicator');

    // System theme: indicator at 0%
    const systemLeft = await indicator.evaluate(el => el.style.left);
    expect(systemLeft).toBe('0%');

    // Switch to light: indicator moves to ~33%
    await page.click('[data-settings-theme="light"]');
    const lightLeft = await indicator.evaluate(el => parseFloat(el.style.left));
    expect(lightLeft).toBeCloseTo(33.333, 1);

    // Switch to dark: indicator moves to ~67%
    await page.click('[data-settings-theme="dark"]');
    const darkLeft = await indicator.evaluate(el => parseFloat(el.style.left));
    expect(darkLeft).toBeCloseTo(66.666, 1);
  });
});
