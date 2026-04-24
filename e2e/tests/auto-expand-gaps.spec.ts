import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

function serverSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

// ============================================================
// Auto-expand small gaps (≤ 8 lines) between diff hunks
// ============================================================
test.describe('Auto-expand small gaps — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('small gaps between hunks are auto-expanded (no spacer visible)', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // server.go has gaps of 8 and 5 lines between its 3 hunks — both ≤ 8,
    // so no inter-hunk spacers should be rendered (leading/trailing spacers may still appear)
    await expect(section.locator('.diff-spacer:not(.diff-spacer-leading):not(.diff-spacer-trailing)')).toHaveCount(0);
  });

  test('auto-expanded context lines render with correct line numbers', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // After auto-expansion, the context lines between hunks should be visible
    // as regular diff rows with line numbers.
    // Look for context rows (not addition, not deletion, not empty) with line numbers.
    const contextRows = section.locator('.diff-split-row');
    const count = await contextRows.count();

    // Find a context row: both sides present, neither addition nor deletion
    let foundContext = false;
    for (let i = 0; i < count; i++) {
      const row = contextRows.nth(i);
      const right = row.locator('.diff-split-side.right');
      const isAddition = await right.evaluate(el => el.classList.contains('addition'));
      const isDeletion = await right.evaluate(el => el.classList.contains('deletion'));
      const isEmpty = await right.evaluate(el => el.classList.contains('empty'));
      if (!isAddition && !isDeletion && !isEmpty) {
        const numText = await right.locator('.diff-gutter-num').textContent();
        if (numText && numText.trim()) {
          foundContext = true;
          // Verify line number is a positive integer
          expect(parseInt(numText.trim(), 10)).toBeGreaterThan(0);
          break;
        }
      }
    }
    expect(foundContext).toBe(true);
  });

  test('auto-expanded context lines are commentable (gutter + button works)', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // Find a context line in the expanded gap area and verify commenting works
    const rows = section.locator('.diff-split-row');
    const count = await rows.count();

    for (let i = 0; i < count; i++) {
      const row = rows.nth(i);
      const right = row.locator('.diff-split-side.right');
      const isAddition = await right.evaluate(el => el.classList.contains('addition'));
      const isDeletion = await right.evaluate(el => el.classList.contains('deletion'));
      const isEmpty = await right.evaluate(el => el.classList.contains('empty'));
      if (!isAddition && !isDeletion && !isEmpty) {
        const numText = await right.locator('.diff-gutter-num').textContent();
        if (numText && numText.trim()) {
          await right.hover();
          const commentBtn = right.locator('.diff-comment-btn');
          await expect(commentBtn).toBeVisible();
          await commentBtn.click();

          const textarea = page.locator('.comment-form textarea');
          await expect(textarea).toBeVisible();
          await textarea.fill('Comment on auto-expanded context line');
          await page.locator('.comment-form .btn-primary').click();

          const card = section.locator('.comment-card');
          await expect(card).toBeVisible();
          await expect(card.locator('.comment-body')).toContainText('Comment on auto-expanded context line');
          break;
        }
      }
    }
  });

  test('only one hunk header remains after merging all small gaps', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // With all gaps ≤ 8, all hunks merge into one contiguous block.
    // The leading spacer embeds the first hunk's header, and subsequent
    // hunks are contiguous — so no standalone hunk headers are rendered.
    const hunkHeaders = section.locator('.diff-hunk-header');
    await expect(hunkHeaders).toHaveCount(0);
  });
});

test.describe('Auto-expand small gaps — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
  });

  test('small gaps are auto-expanded in unified mode (no spacer)', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    await expect(section.locator('.diff-spacer:not(.diff-spacer-leading):not(.diff-spacer-trailing)')).toHaveCount(0);
  });

  test('auto-expanded context lines in unified mode have correct line numbers', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // Find a context line (not addition, not deletion)
    const contextLine = section.locator('.diff-container.unified .diff-line:not(.addition):not(.deletion)').first();
    await expect(contextLine).toBeVisible();

    // Both old and new line numbers should be present
    const gutterNums = contextLine.locator('.diff-gutter-num');
    const oldNum = await gutterNums.nth(0).textContent();
    const newNum = await gutterNums.nth(1).textContent();
    expect(oldNum && oldNum.trim()).toBeTruthy();
    expect(newNum && newNum.trim()).toBeTruthy();
  });

  test('auto-expanded context lines are commentable in unified mode', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    const contextLine = section.locator('.diff-container.unified .diff-line:not(.addition):not(.deletion)').first();
    await expect(contextLine).toBeVisible();
    await contextLine.hover();

    const commentBtn = contextLine.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('Unified auto-expanded comment');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Unified auto-expanded comment');
  });
});

test.describe('Large gaps still show spacer', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('large gaps (> 8 lines) still show spacer with expand controls', async ({ page }) => {
    // routes.go has a gap of >20 unchanged lines between its two hunks,
    // so the spacer should still be visible after auto-expansion, showing
    // directional expand controls and the hunk header text.
    const treeEntry = page.locator('.tree-file-name', { hasText: 'routes.go' });
    await treeEntry.click();

    const routesSection = page.locator('#file-section-routes\\.go');
    const spacer = routesSection.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();
    // Spacer now embeds the @@ hunk header instead of "unchanged line" text
    await expect(spacer.locator('.spacer-hunk-text')).toContainText('@@');
  });
});

test.describe('Auto-expand does not break other files', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('handler.js (new file, single hunk) renders correctly', async ({ page }) => {
    const handlerSection = page.locator('#file-section-handler\\.js');
    await expect(handlerSection).toBeVisible();

    // New file should have all addition lines, no spacers
    await expect(handlerSection.locator('.diff-spacer')).toHaveCount(0);
    const additionSide = handlerSection.locator('.diff-split-side.addition');
    await expect(additionSide.first()).toBeVisible();
  });

  test('auto-expanded file still renders hunk header in spacer', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // After merging, standalone hunk headers are suppressed — the leading
    // spacer embeds the first hunk's @@ header text instead.
    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();
    await expect(leadingSpacer.locator('.spacer-hunk-text')).toContainText('@@');
  });
});
