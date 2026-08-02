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
  async function setTheme(page: Page, theme: 'dark' | 'light', bgPage: string) {
    await page.addStyleTag({ content: '* { transition: none !important; }' });
    await page.evaluate(
      (t) => document.documentElement.setAttribute('data-theme', t),
      theme,
    );
    await page.waitForFunction(
      (expected) =>
        getComputedStyle(document.documentElement)
          .getPropertyValue('--crit-bg-page')
          .trim() === expected,
      bgPage,
    );
  }

  test('should have no critical accessibility violations', async ({ page }) => {
    await page.waitForSelector('.file-section');

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      // nested-interactive: 6 nested interactive controls (tracked separately)
      .disableRules(['nested-interactive'])
      .analyze();

    const violations = results.violations.map(v => ({
      id: v.id,
      impact: v.impact,
      description: v.description,
      nodes: v.nodes.length
    }));

    expect(violations).toEqual([]);
  });

  test('should have no color contrast violations in dark theme', async ({ page }) => {
    await page.waitForSelector('.file-section');
    await setTheme(page, 'dark', '#0e0f13');

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .disableRules(['nested-interactive'])
      .analyze();

    const contrast = results.violations.find(v => v.id === 'color-contrast');
    expect(contrast?.nodes ?? []).toEqual([]);
  });

  test('should have no color contrast violations in light theme', async ({ page }) => {
    await page.waitForSelector('.file-section');
    await setTheme(page, 'light', '#ffffff');

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .disableRules(['nested-interactive'])
      .analyze();

    const contrast = results.violations.find(v => v.id === 'color-contrast');
    expect(contrast?.nodes ?? []).toEqual([]);
  });
});
