import { test, expect, type Page, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, fileHeader, diffLine, openDocLineComment } from './helpers';

// plan.md's Document/Diff toggle buttons (live in Crit's Pierre file header).
function toggleBtn(page: Page, mode: 'document' | 'diff'): Locator {
  return fileHeader(page, 'plan.md').locator(`.file-header-toggle .toggle-btn[data-mode="${mode}"]`);
}

// Diff view: plan.md's changed source lines render as Pierre diff rows.
// (Document view keeps an empty placeholder context row to host the
// rendered document annotation, so count change rows only.)
function diffRows(item: Locator): Locator {
  return item.locator('code [data-content] > [data-line][data-line-type^="change-"]');
}

// ============================================================
// Markdown Document/Diff Toggle (git mode only)
// ============================================================
test.describe('Markdown Document/Diff Toggle — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('markdown defaults to diff view and toggles to Document and back; code files have no toggle', async ({ page }) => {
    const section = await mdSection(page);

    // Diff view by default: plan.md is a new file, so every source line is an
    // addition, numbered from 1.
    await expect(fileHeader(page, 'plan.md').locator('.file-header-toggle')).toBeVisible();
    await expect(toggleBtn(page, 'diff')).toHaveClass(/active/);
    await expect(toggleBtn(page, 'diff')).toHaveAttribute('aria-pressed', 'true');
    const first = diffLine(section, 1);
    await expect(first).toBeVisible();
    await expect(first).toHaveAttribute('data-line-type', 'change-addition');
    await expect(first).toContainText('# Authentication Plan');
    await expect(section.locator('[data-gutter] > [data-column-number="1"]').first()).toBeVisible();
    await expect(section.locator('.document-wrapper')).toHaveCount(0);

    // Document view: rendered markdown with line blocks; the diff rows go.
    const docBtn = toggleBtn(page, 'document');
    await docBtn.click();
    await expect(section.locator('[id="file-section-plan.md"].pierre-document .document-wrapper')).toBeVisible();
    await expect(diffRows(section)).toHaveCount(0);
    await expect(docBtn).toHaveClass(/active/);
    await expect(docBtn).toHaveAttribute('aria-pressed', 'true');
    await expect(section.locator('.document-wrapper h1')).toHaveText(/Authentication Plan/);
    // Line numbers, starting at 1.
    await expect(section.locator('.line-gutter .line-num').first()).toHaveText('1');
    // plan.md is added on the feature branch — every line block is marked as
    // added (the trailing empty line after the final newline is the only exception).
    await expect(section.locator('.line-block-added').first()).toBeVisible();
    await expect(section.locator('.line-block:not(.line-block-added) .line-content:not(.empty-line)')).toHaveCount(0);
    // The change navigation widget remains file-mode only.
    await expect(page.locator('.change-nav')).not.toBeVisible();

    // Back to Diff.
    const diffBtn = toggleBtn(page, 'diff');
    await diffBtn.click();
    await expect(diffLine(section, 1)).toBeVisible();
    await expect(section.locator('.document-wrapper')).toHaveCount(0);
    await expect(diffBtn).toHaveClass(/active/);

    // server.go has no Document/Diff toggle.
    await page.locator('.tree-file[data-tree-path="server.go"]').click();
    const goHeader = fileHeader(page, 'server.go');
    await expect(goHeader).toBeVisible();
    await expect(goHeader.locator('.file-header-toggle')).toHaveCount(0);
  });

  test('comments created in document view are visible after switching to diff and back', async ({ page }) => {
    const section = await mdSection(page);

    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    await openDocLineComment(page, section);
    await page.locator('.comment-form textarea').fill('Cross-view comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Switch to diff view — the comment is anchored to line 1 there too
    await toggleBtn(page, 'diff').click();
    await expect(diffLine(section, 1)).toBeVisible();
    await expect(section.locator('.comment-card .comment-body')).toContainText('Cross-view comment');

    // Switch back to document view — comment still visible
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Cross-view comment');
  });

  test('switching view closes an open comment form and clears the line selection', async ({ page }) => {
    const section = await mdSection(page);

    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await openDocLineComment(page, section);
    await expect(page.locator('.comment-form')).toBeVisible();
    await expect(section.locator('.line-block.selected')).toHaveCount(1);

    await toggleBtn(page, 'diff').click();
    await expect(diffLine(section, 1)).toBeVisible();
    await expect(page.locator('.comment-form')).toHaveCount(0);

    // Back in document view the selection is gone too.
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
  });
});
