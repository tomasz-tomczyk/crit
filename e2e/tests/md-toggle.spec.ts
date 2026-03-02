import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  for (const f of (session.files || [])) {
    const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await commentsRes.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
}

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

// ============================================================
// Markdown Document/Diff Toggle (git mode only)
// ============================================================
test.describe('Markdown Document/Diff Toggle — Git Mode', () => {
  test('markdown file defaults to diff view in git mode', async ({ page }) => {
    await loadPage(page);
    const section = mdSection(page);
    await expect(section).toBeVisible();

    // In git mode, markdown defaults to diff view
    const toggle = section.locator('.file-header-toggle');
    await expect(toggle).toBeVisible();

    const diffBtn = toggle.locator('.toggle-btn[data-mode="diff"]');
    await expect(diffBtn).toHaveClass(/active/);
  });

  test('clicking Document button switches to document view', async ({ page }) => {
    await loadPage(page);
    const section = mdSection(page);

    const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
    await docBtn.click();

    // Document wrapper should appear
    await expect(section.locator('.document-wrapper')).toBeVisible();
    // Diff container should not be visible
    await expect(section.locator('.diff-container')).toHaveCount(0);
    // Document button should be active
    await expect(docBtn).toHaveClass(/active/);
  });

  test('clicking Diff button switches back to diff view', async ({ page }) => {
    await loadPage(page);
    const section = mdSection(page);

    // Switch to document first
    await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Switch back to diff
    const diffBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="diff"]');
    await diffBtn.click();

    // Diff container should appear
    await expect(section.locator('.diff-container')).toBeVisible();
    await expect(section.locator('.document-wrapper')).toHaveCount(0);
    await expect(diffBtn).toHaveClass(/active/);
  });

  test('document view shows rendered markdown with line blocks', async ({ page }) => {
    await loadPage(page);
    const section = mdSection(page);

    await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Should have line blocks with gutters
    const lineBlocks = section.locator('.line-block');
    await expect(lineBlocks.first()).toBeVisible();
    const count = await lineBlocks.count();
    expect(count).toBeGreaterThan(0);

    // Should have rendered markdown content (headings)
    await expect(section.locator('h1')).toBeVisible();
  });

  test('diff view shows hunk headers for markdown', async ({ page }) => {
    await loadPage(page);
    const section = mdSection(page);

    // Should be in diff view by default in git mode
    const hunkHeader = section.locator('.diff-hunk-header');
    // plan.md is a new file, so there should be at least one hunk
    await expect(hunkHeader.first()).toBeVisible();
  });

  test('toggle only appears on markdown files, not code files', async ({ page }) => {
    await loadPage(page);

    // server.go should NOT have a document/diff toggle
    const goSection = page.locator('#file-section-server\\.go');
    await expect(goSection).toBeVisible();
    const goToggle = goSection.locator('.file-header-toggle');
    await expect(goToggle).toHaveCount(0);

    // plan.md SHOULD have the toggle
    const mdToggle = mdSection(page).locator('.file-header-toggle');
    await expect(mdToggle).toBeVisible();
  });

  test('comments created in document view are visible after switching to diff and back', async ({ page, request }) => {
    // Clear comments first
    const sessionRes = await request.get('/api/session');
    const session = await sessionRes.json();
    for (const f of session.files || []) {
      const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
      const comments = await commentsRes.json();
      if (Array.isArray(comments)) {
        for (const c of comments) {
          await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
        }
      }
    }

    await loadPage(page);
    const section = mdSection(page);

    // Switch to document view
    await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Add a comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Cross-view comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Switch to diff view
    await section.locator('.file-header-toggle .toggle-btn[data-mode="diff"]').click();
    await expect(section.locator('.diff-container')).toBeVisible();

    // Switch back to document view
    await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Comment should still be visible
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Cross-view comment');
  });

  test('switching view closes open comment form', async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = mdSection(page);

    // Switch to document view and open a comment form
    await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await expect(page.locator('.comment-form')).toBeVisible();

    // Switch to diff view
    await section.locator('.file-header-toggle .toggle-btn[data-mode="diff"]').click();
    await expect(section.locator('.diff-container')).toBeVisible();

    // Comment form should be gone
    await expect(page.locator('.comment-form')).toHaveCount(0);
  });

  test('switching view clears line selection highlight', async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = mdSection(page);

    // Switch to document view and select a line
    await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await expect(section.locator('.line-block.selected')).toHaveCount(1);

    // Switch to diff view
    await section.locator('.file-header-toggle .toggle-btn[data-mode="diff"]').click();
    await expect(section.locator('.diff-container')).toBeVisible();

    // Switch back to document view — selection should be gone
    await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
  });
});
