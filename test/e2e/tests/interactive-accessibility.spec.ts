import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { addComment, clearAllComments, getMdPath, loadPage } from './helpers';

test.describe('Interactive accessibility', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('open settings dialog is accessible and its tabs support arrow navigation', async ({ page }) => {
    await page.locator('#settingsToggle').click();

    const overlay = page.locator('#settingsOverlay');
    const settingsTab = page.locator('#tab-settings');
    const shortcutsTab = page.locator('#tab-shortcuts');
    await expect(overlay).toHaveClass(/active/);
    await expect(settingsTab).toBeFocused();

    await page.keyboard.press('ArrowRight');
    await expect(shortcutsTab).toBeFocused();
    await expect(shortcutsTab).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator('#shortcutsPane')).toHaveClass(/active/);

    const results = await new AxeBuilder({ page })
      .include('#settingsOverlay')
      .withTags(['wcag2a', 'wcag2aa'])
      // Match the existing audit's exclusion for known, separately tracked controls.
      .disableRules(['nested-interactive'])
      .analyze();

    expect(results.violations.map(({ id, impact }) => ({ id, impact }))).toEqual([]);
  });

  test('comments filter follows the ARIA radiogroup keyboard pattern', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Open filter comment');
    const resolved = await addComment(request, mdPath, 3, 'Resolved filter comment');
    const resolveResponse = await request.put(
      `/api/comment/${resolved.id}/resolve?path=${encodeURIComponent(mdPath)}`,
      { data: { resolved: true } },
    );
    expect(resolveResponse.ok()).toBeTruthy();

    await loadPage(page);
    await page.locator('#commentCount').click();

    const all = page.locator('#commentsFilterPill [data-filter="all"]');
    const open = page.locator('#commentsFilterPill [data-filter="open"]');
    const resolvedFilter = page.locator('#commentsFilterPill [data-filter="resolved"]');
    await expect(page.locator('#commentsFilterPill')).toBeVisible();
    await all.focus();

    await page.keyboard.press('ArrowRight');
    await expect(open).toBeFocused();
    await expect(open).toHaveAttribute('aria-checked', 'true');
    await expect(open).toHaveAttribute('tabindex', '0');
    await expect(all).toHaveAttribute('tabindex', '-1');
    await expect(page.locator('.panel-comment-block .comment-body')).toHaveText('Open filter comment');

    await page.keyboard.press('End');
    await expect(resolvedFilter).toBeFocused();
    await expect(resolvedFilter).toHaveAttribute('aria-checked', 'true');
    await expect(page.locator('.panel-comment-block .comment-body')).toHaveText('Resolved filter comment');
  });
});
