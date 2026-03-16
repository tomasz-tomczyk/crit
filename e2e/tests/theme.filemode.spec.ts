import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Theme Tests (file mode)
// ============================================================
test.describe('Theme — File Mode', () => {
  test.beforeEach(async ({ page, context, request }) => {
    await clearAllComments(request);
    // Clear theme cookie before each test
    await context.clearCookies();
    await loadPage(page);
  });

  test('clicking light theme button sets data-theme="light" on <html>', async ({ page }) => {
    const lightBtn = page.locator('.theme-pill-btn[data-for-theme="light"]');
    await lightBtn.click();

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('light');
  });

  test('clicking dark theme button sets data-theme="dark" on <html>', async ({ page }) => {
    const darkBtn = page.locator('.theme-pill-btn[data-for-theme="dark"]');
    await darkBtn.click();

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('dark');
  });

  test('clicking system theme button removes data-theme from <html>', async ({ page }) => {
    // First set to dark, then switch to system
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    await page.locator('.theme-pill-btn[data-for-theme="system"]').click();

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBeNull();
  });

  test('theme persists across page reload', async ({ page }) => {
    // Set dark theme
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    // Reload
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Should still be dark
    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('dark');
  });

  test('theme pill indicator moves when theme changes', async ({ page }) => {
    const indicator = page.locator('.theme-pill-indicator');

    // System theme: indicator at 0%
    const systemLeft = await indicator.evaluate(el => el.style.left);
    expect(systemLeft).toBe('0%');

    // Switch to light
    await page.locator('.theme-pill-btn[data-for-theme="light"]').click();
    const lightLeft = await indicator.evaluate(el => el.style.left);
    expect(lightLeft).toBe('33.333%');

    // Switch to dark
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    const darkLeft = await indicator.evaluate(el => el.style.left);
    expect(darkLeft).toBe('66.666%');
  });
});
