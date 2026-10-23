import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// The fixture's fake home has a ~/.crit/custom.css that defines a probe
// variable (setup-fixtures-singlefile.sh).
test.describe('Custom stylesheet — Single File Mode', () => {
  test('~/.crit/custom.css is linked last, shown in Settings, and its switch turns it off across reloads', async ({ page }) => {
    const applied = () => page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue('--crit-e2e-custom-css').trim());
    await loadPage(page);

    // It comes after every other stylesheet, so its rules win ties.
    const lastSheet = await page.evaluate(() => {
      const sheets = Array.from(document.head.querySelectorAll('style, link[rel="stylesheet"]'));
      return sheets[sheets.length - 1]?.id;
    });
    expect(lastSheet).toBe('critCustomCSS');
    await expect.poll(applied).toBe('applied');

    await page.locator('#settingsToggle').click();
    const card = page.locator('.config-card', { has: page.locator('.config-card-title', { hasText: 'Custom CSS' }) });
    await expect(card.locator('.config-card-value')).toHaveText('Found');
    // Backslashes on Windows.
    await expect(card.locator('.config-card-cmd')).toContainText(/\.crit[\\/]custom\.css/);

    // The .comments-panel-switch input is hidden behind its track, so flip
    // it through the wrapping label, as a user does (see whitespace.spec.ts).
    await expect(page.locator('#customCSSToggle')).toBeChecked();
    await page.locator('label.comments-panel-switch', { has: page.locator('#customCSSToggle') }).click();
    await expect.poll(applied).toBe('');

    await loadPage(page);
    await expect(page.locator('#critCustomCSS')).toHaveJSProperty('disabled', true);
    expect(await applied()).toBe('');
  });

  test('data-crit-mode follows the theme, so per-mode blocks (and rules nested in them) apply', async ({ page }) => {
    const probe = (name: string, selector = ':root') => page.evaluate(([n, s]) =>
      getComputedStyle(document.querySelector(s)!).getPropertyValue(n).trim(), [name, selector] as const);

    for (const [theme, colorScheme, mode] of [
      ['dark', 'light', 'dark'],
      ['system', 'light', 'light'],
      ['system', 'dark', 'dark'],
    ] as const) {
      await page.emulateMedia({ colorScheme });
      await page.context().addCookies([{
        name: 'crit-settings',
        value: encodeURIComponent(JSON.stringify({ theme })),
        domain: 'localhost',
        path: '/',
      }]);
      await loadPage(page);
      await expect(page.locator('html')).toHaveAttribute('data-crit-mode', mode);
      await expect.poll(() => probe('--crit-e2e-mode')).toBe(mode);
      await expect.poll(() => probe('--crit-e2e-nested', '#filesContainer .document-wrapper')).toBe(mode === 'dark' ? 'dark' : '');
    }
  });
});
