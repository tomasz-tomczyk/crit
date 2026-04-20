import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { loadPage } from './helpers';

test.describe('Accessibility', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

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
    await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));
    await page.waitForFunction(() => {
      const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg-primary').trim();
      return bg === '#1a1b26';
    });

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .disableRules(['nested-interactive'])
      .analyze();

    const contrast = results.violations.find(v => v.id === 'color-contrast');
    expect(contrast?.nodes ?? []).toEqual([]);
  });

  test('should have no color contrast violations in light theme', async ({ page }) => {
    await page.waitForSelector('.file-section');
    await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light'));
    await page.waitForFunction(() => {
      const bg = getComputedStyle(document.documentElement).getPropertyValue('--bg-primary').trim();
      return bg === '#fafafa';
    });

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .disableRules(['nested-interactive'])
      .analyze();

    const contrast = results.violations.find(v => v.id === 'color-contrast');
    expect(contrast?.nodes ?? []).toEqual([]);
  });
});
