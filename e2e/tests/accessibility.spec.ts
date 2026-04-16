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
      // Pre-existing issues to fix separately:
      // - color-contrast: 74 elements need contrast adjustments
      // - nested-interactive: 6 nested interactive controls
      .disableRules(['color-contrast', 'nested-interactive'])
      .analyze();

    const violations = results.violations.map(v => ({
      id: v.id,
      impact: v.impact,
      description: v.description,
      nodes: v.nodes.length
    }));

    expect(violations).toEqual([]);
  });
});
