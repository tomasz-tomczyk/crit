import { test, expect, type Page, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, fileHeader, diffLine } from './helpers';

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

  test('markdown file defaults to diff view in git mode', async ({ page }) => {
    const section = await mdSection(page);

    // In git mode, markdown defaults to diff view
    await expect(fileHeader(page, 'plan.md').locator('.file-header-toggle')).toBeVisible();
    await expect(toggleBtn(page, 'diff')).toHaveClass(/active/);
    await expect(toggleBtn(page, 'diff')).toHaveAttribute('aria-pressed', 'true');
    await expect(diffLine(section, 1)).toBeVisible();
    await expect(section.locator('.document-wrapper')).toHaveCount(0);
  });

  test('clicking Document button switches to document view', async ({ page }) => {
    const section = await mdSection(page);

    const docBtn = toggleBtn(page, 'document');
    await docBtn.click();

    // Rendered document appears; the Pierre diff rows are gone
    await expect(section.locator('[id="file-section-plan.md"].pierre-document .document-wrapper')).toBeVisible();
    await expect(diffRows(section)).toHaveCount(0);
    await expect(docBtn).toHaveClass(/active/);
    await expect(docBtn).toHaveAttribute('aria-pressed', 'true');
  });

  test('clicking Diff button switches back to diff view', async ({ page }) => {
    const section = await mdSection(page);

    // Switch to document first
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Switch back to diff
    const diffBtn = toggleBtn(page, 'diff');
    await diffBtn.click();

    await expect(diffLine(section, 1)).toBeVisible();
    await expect(section.locator('.document-wrapper')).toHaveCount(0);
    await expect(diffBtn).toHaveClass(/active/);
  });

  test('document view shows rendered markdown with line blocks', async ({ page }) => {
    const section = await mdSection(page);

    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Should have line blocks with gutters
    await expect(section.locator('.line-block').first()).toBeVisible();

    // Should have rendered markdown content (headings)
    await expect(section.locator('.document-wrapper h1')).toHaveText(/Authentication Plan/);
  });

  test('diff view shows markdown source as added diff lines', async ({ page }) => {
    const section = await mdSection(page);

    // plan.md is a new file: diff view shows the raw markdown source, every
    // line an addition, numbered from 1.
    const first = diffLine(section, 1);
    await expect(first).toBeVisible();
    await expect(first).toHaveAttribute('data-line-type', 'change-addition');
    await expect(first).toContainText('# Authentication Plan');
    await expect(section.locator('[data-gutter] > [data-column-number="1"]').first()).toBeVisible();
  });

  test('toggle only appears on markdown files, not code files', async ({ page }) => {
    // plan.md SHOULD have the toggle
    await mdSection(page);
    await expect(fileHeader(page, 'plan.md').locator('.file-header-toggle')).toBeVisible();

    // server.go should NOT have a document/diff toggle
    const tree = page.locator('.tree-file[data-tree-path="server.go"]');
    await tree.click();
    const goHeader = fileHeader(page, 'server.go');
    await expect(goHeader).toBeVisible();
    await expect(goHeader.locator('.file-header-toggle')).toHaveCount(0);
  });

  test('comments created in document view are visible after switching to diff and back', async ({ page }) => {
    const section = await mdSection(page);

    // Switch to document view
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Add a comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Cross-view comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Switch to diff view — the comment is anchored to line 1 there too
    await toggleBtn(page, 'diff').click();
    await expect(diffLine(section, 1)).toBeVisible();
    await expect(section.locator('.comment-card .comment-body')).toContainText('Cross-view comment');

    // Switch back to document view
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Comment should still be visible
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Cross-view comment');
  });

  test('switching view closes open comment form', async ({ page }) => {
    const section = await mdSection(page);

    // Switch to document view and open a comment form
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await expect(page.locator('.comment-form')).toBeVisible();

    // Switch to diff view
    await toggleBtn(page, 'diff').click();
    await expect(diffLine(section, 1)).toBeVisible();

    // Comment form should be gone
    await expect(page.locator('.comment-form')).toHaveCount(0);
  });

  test('document view shows change indicators in git mode', async ({ page }) => {
    const section = await mdSection(page);

    // Switch to document view
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // plan.md is added on the feature branch — every line block should be marked as added
    await expect(section.locator('.line-block').first()).toBeVisible();
    await expect(section.locator('.line-block-added').first()).toBeVisible();
    // (the trailing empty line after the final newline is the only exception)
    await expect(section.locator('.line-block:not(.line-block-added) .line-content:not(.empty-line)')).toHaveCount(0);

    // Change navigation widget remains file-mode only
    await expect(page.locator('.change-nav')).not.toBeVisible();
  });

  test('document view shows line numbers in git mode', async ({ page }) => {
    const section = await mdSection(page);

    // Switch to document view
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Line gutters with line numbers should be present
    await expect(section.locator('.line-gutter').first()).toBeVisible();
    await expect(section.locator('.line-gutter .line-num').first()).toHaveText('1');
  });

  test('switching view clears line selection highlight', async ({ page }) => {
    const section = await mdSection(page);

    // Switch to document view and select a line
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await expect(section.locator('.line-block.selected')).toHaveCount(1);

    // Switch to diff view
    await toggleBtn(page, 'diff').click();
    await expect(diffLine(section, 1)).toBeVisible();

    // Switch back to document view — selection should be gone
    await toggleBtn(page, 'document').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
  });
});
