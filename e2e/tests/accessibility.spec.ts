import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { loadPage } from './helpers';

test.describe('Accessibility', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('should have no critical accessibility violations', async ({ page }) => {
    // Wait for content to render
    await page.waitForSelector('.file-section');

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .analyze();

    // Report violations with details for debugging
    const violations = results.violations.map(v => ({
      id: v.id,
      impact: v.impact,
      description: v.description,
      nodes: v.nodes.length
    }));

    expect(violations).toEqual([]);
  });
});
