import { test, expect } from '@playwright/test';

// Live and preview mode share the review page's theme palettes: the saved
// light/dark theme colours the chrome, and Settings offers the theme choice.
test.describe('theme palette — live mode', () => {
  test('applies the saved theme and switches it from Settings', async ({ page, context, baseURL }) => {
    await context.addCookies([{
      name: 'crit-settings',
      value: encodeURIComponent(JSON.stringify({ theme: 'dark', darkPalette: 'dracula-soft' })),
      url: baseURL!,
    }]);
    await page.goto('/live');
    const html = page.locator('html');
    await expect(html).toHaveAttribute('data-crit-palette', 'dracula-soft');
    await expect(page.locator('.header')).toHaveCSS('background-color', await page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue('--crit-palette-surface').trim()
        .replace(/^#(..)(..)(..)$/, (_, r, g, b) => `rgb(${parseInt(r, 16)}, ${parseInt(g, 16)}, ${parseInt(b, 16)})`)));

    await page.locator('#settingsToggle').click();
    const select = page.locator('#darkPaletteSelect');
    await expect(select).toHaveValue('dracula-soft');
    // Code-only options are not offered in live mode.
    await expect(page.locator('#lineNumbersSelect')).toHaveCount(0);
    await select.selectOption('nord');
    await expect(html).toHaveAttribute('data-crit-palette', 'nord');
  });
});
