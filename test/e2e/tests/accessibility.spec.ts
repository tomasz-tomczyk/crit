import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { loadPage } from './helpers';

test.describe('Accessibility', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  // Switch to an explicit theme and wait until it has fully applied.
  //
  // `.toggle-btn` and other chrome elements transition colors over 150ms
  // (`transition: all 0.15s` in style.css). Axe would otherwise measure the
  // buttons mid-transition — light-theme tokens interpolating toward dark on
  // dark surfaces — and report thousands of bogus color-contrast violations.
  // Disable transitions so the audit sees the settled theme, exactly what a
  // user sees after the theme switch completes.
  async function setTheme(page: Page, theme: 'dark' | 'light') {
    await page.addStyleTag({ content: '* { transition: none !important; }' });
    // Go through the app's theme switch (what the settings pill calls) so the
    // Pierre diff surface follows the page theme, not just the chrome.
    await page.evaluate(
      (t) => (window as unknown as { applyTheme: (c: string) => void }).applyTheme(t),
      theme,
    );
    await page.waitForFunction(
      () => {
        const root = document.documentElement;
        const css = getComputedStyle(root);
        return !!root.dataset.critPalette && css.getPropertyValue('--crit-bg-page').trim() === css.getPropertyValue('--crit-palette-bg').trim();
      },
    );
  }

  // Syntax colours belong to the theme and are shown as designed, so with
  // the boost off the contrast audit skips token text (spans carrying Pierre's
  // --diffs-token-* properties). Everything Crit draws itself is audited.
  // With "Boost low-contrast syntax colours" on, token text is audited too.
  const isToken = (html: string) => /^<span style="--diffs-token-/.test(html);

  async function audit(page: Page, boost: boolean) {
    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      // nested-interactive: 6 nested interactive controls (tracked separately)
      .disableRules(['nested-interactive'])
      .analyze();
    return results.violations.flatMap(v => v.nodes
      .filter(n => boost || v.id !== 'color-contrast' || !isToken(n.html))
      .map(n => `${v.id}: ${n.target.join(' ')} ${n.html.slice(0, 80)}`));
  }

  async function setBoost(page: Page, on: boolean) {
    await page.context().addCookies([{
      name: 'crit-settings',
      value: encodeURIComponent(JSON.stringify({ diffScope: 'all', boostContrast: on ? 'on' : 'off' })),
      domain: 'localhost',
      path: '/',
    }]);
    await loadPage(page);
  }

  for (const boost of [false, true]) {
    const label = boost ? ' (syntax boost on)' : '';

    test('should have no critical accessibility violations' + label, async ({ page }) => {
      await setBoost(page, boost);
      await expect(page.locator('diffs-container [data-line]').first()).toBeVisible();
      expect(await audit(page, boost)).toEqual([]);
    });

    for (const theme of ['dark', 'light'] as const) {
      test(`should have no color contrast violations in ${theme} theme${label}`, async ({ page }) => {
        await setBoost(page, boost);
        await expect(page.locator('diffs-container [data-line]').first()).toBeVisible();
        await setTheme(page, theme);
        expect(await audit(page, boost)).toEqual([]);
      });
    }
  }
});
