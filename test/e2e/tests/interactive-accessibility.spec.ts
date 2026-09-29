import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { addComment, clearAllComments, fileHeader, fileItem, getMdPath, goSection, loadPage } from './helpers';

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

    // Audit the settled dialog: its open animation and the tab color
    // transitions blend foreground into background while running, which axe
    // reports as contrast failures (seen on main too, not a product issue).
    await overlay.evaluate(async (el) => {
      await Promise.all(el.getAnimations({ subtree: true }).map((a) => a.finished));
    });

    const results = await new AxeBuilder({ page })
      .include('#settingsOverlay')
      .withTags(['wcag2a', 'wcag2aa'])
      // Known product issues (same stance as accessibility.spec.ts):
      // nested-interactive — nested controls in the overlay
      // aria-required-children — tablist markup not yet children-complete for axe
      .disableRules(['nested-interactive', 'aria-required-children'])
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

  test('file header collapse is a keyboard button with aria-expanded', async ({ page }) => {
    await goSection(page);
    const chevron = () => fileHeader(page, 'server.go').locator('.file-header-chevron');
    const collapsed = () =>
      fileHeader(page, 'server.go').evaluate((el) => el.classList.contains('collapsed'));
    await expect(chevron()).toHaveJSProperty('tagName', 'BUTTON');
    await expect(chevron()).toHaveAttribute('aria-expanded', 'true');
    await expect(chevron()).toHaveAccessibleName(/server\.go/);

    // Focus + keypress must be atomic: a remount between focus() and the
    // keypress drops focus and the toggle never fires. State guards make
    // retries idempotent (no double-toggle).
    await expect(async () => {
      if (await collapsed()) return;
      await chevron().focus();
      await page.keyboard.press('Enter');
      await expect(fileHeader(page, 'server.go')).toHaveClass(/\bcollapsed\b/, { timeout: 2_000 });
    }).toPass({ timeout: 15_000 });
    await expect(chevron()).toHaveAttribute('aria-expanded', 'false');
    await expect(chevron()).toBeFocused();
    await expect(fileItem(page, 'server.go').locator('[data-line]')).toHaveCount(0);

    await expect(async () => {
      if (!(await collapsed())) return;
      await chevron().focus();
      await page.keyboard.press('Space');
      await expect(fileHeader(page, 'server.go')).not.toHaveClass(/\bcollapsed\b/, { timeout: 2_000 });
    }).toPass({ timeout: 15_000 });
    await expect(chevron()).toHaveAttribute('aria-expanded', 'true');
    await expect(chevron()).toBeFocused();
    await expect(fileItem(page, 'server.go').locator('[data-line]').first()).toBeVisible();
  });
});
