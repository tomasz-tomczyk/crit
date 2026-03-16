import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, jsSection, switchToDocumentView } from './helpers';

// ============================================================
// Markdown Comments (git mode — plan.md in document view)
// ============================================================
test.describe('Markdown Comments — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('clicking + gutter button opens comment form', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
  });

  test('comment form textarea is focused when opened', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();
  });

  test('submitting comment creates a comment card', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('This is a test comment on markdown');

    const submitBtn = page.locator('.comment-form .btn-primary');
    await submitBtn.click();

    // Comment card should appear
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('This is a test comment on markdown');
  });

  test('comment with fenced code block gets syntax highlighting', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Check this:\n```go\nfunc main() {\n\tfmt.Println("hello")\n}\n```');
    await page.locator('.comment-form .btn-primary').click();

    const body = section.locator('.comment-card .comment-body');
    await expect(body).toBeVisible();
    // hljs should produce spans with hljs-* classes inside the code block
    const codeBlock = body.locator('pre code');
    await expect(codeBlock).toBeVisible();
    await expect(codeBlock.locator('span[class^="hljs-"]').first()).toBeVisible();
  });

  test('comment with URL renders styled link', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('See https://example.com for details');
    await page.locator('.comment-form .btn-primary').click();

    const body = section.locator('.comment-card .comment-body');
    const link = body.locator('a');
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', 'https://example.com');
    // Link should have accent color styling (not default browser blue)
    const color = await link.evaluate(el => getComputedStyle(el).color);
    expect(color).not.toBe('rgb(0, 0, 238)'); // not default blue
  });

  test('Ctrl+Enter submits comment', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Ctrl+Enter test');
    await textarea.press('Control+Enter');

    // Comment card should appear
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Ctrl+Enter test');
  });

  test('editing a comment opens editor with existing text and saves changes', async ({ page }) => {
    const section = mdSection(page);

    // Create a comment first
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Original text');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Click Edit
    const editBtn = section.locator('.comment-actions button[title="Edit"]');
    await editBtn.click();

    // Editor should open with existing text
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Original text');

    // Change text and submit
    await textarea.clear();
    await textarea.fill('Updated text');
    await page.locator('.comment-form .btn-primary').click();

    // Comment should show updated text
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Updated text');
  });

  test('deleting a comment removes it and updates count', async ({ page }) => {
    const section = mdSection(page);
    const countEl = page.locator('#commentCount');

    // Create a comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Delete me');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(countEl).toBeVisible();

    // Delete it
    const deleteBtn = section.locator('.comment-actions .delete-btn');
    await deleteBtn.click();

    // Comment card should be gone
    await expect(section.locator('.comment-card')).toHaveCount(0);
    await expect(countEl).toBeHidden();
  });

  test('pressing Escape closes the comment form', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Press Escape
    const textarea = page.locator('.comment-form textarea');
    await textarea.press('Escape');

    await expect(form).toHaveCount(0);
  });

  test('comment form header shows correct line reference', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const header = page.locator('.comment-form-header');
    await expect(header).toBeVisible();
    // Should show "Comment on Line N" or "Comment on Lines N-M"
    await expect(header).toContainText(/Line/);
  });

  test('comment body renders markdown (bold, links, code)', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('**bold text** and `inline code` and [a link](https://example.com)');
    await page.locator('.comment-form .btn-primary').click();

    const body = section.locator('.comment-card .comment-body');
    await expect(body).toBeVisible();

    // Bold should render as <strong>
    await expect(body.locator('strong')).toHaveText('bold text');
    // Inline code should render as <code>
    await expect(body.locator('code')).toHaveText('inline code');
    // Link should render as <a>
    const link = body.locator('a');
    await expect(link).toHaveText('a link');
    await expect(link).toHaveAttribute('href', 'https://example.com');
  });
});

// ============================================================
// Diff Comments (git mode — code files in split/unified modes)
// ============================================================
test.describe('Diff Comments — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('hovering a diff line shows the + button', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Find an addition line in split mode
    const additionSide = section.locator('.diff-split-side.addition').first();
    await expect(additionSide).toBeVisible();
    await additionSide.hover();

    const commentBtn = additionSide.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
  });

  test('clicking + button on a diff line opens comment form', async ({ page }) => {
    const section = goSection(page);

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();

    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
  });

  test('submitting a diff comment creates a comment card', async ({ page }) => {
    const section = goSection(page);

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Diff comment in split mode');
    await page.locator('.comment-form .btn-primary').click();

    // Comment card should appear in the diff section
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Diff comment in split mode');
  });

  test('comments work on addition lines in split mode', async ({ page }) => {
    // Use handler.js which is an all-addition file
    const section = jsSection(page);
    await expect(section).toBeVisible();

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on new code');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on new code');
  });

  test('diff comment body renders markdown', async ({ page }) => {
    const section = goSection(page);

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('This has **bold**, `code`, and a [link](https://test.com)');
    await page.locator('.comment-form .btn-primary').click();

    const body = section.locator('.comment-card .comment-body');
    await expect(body).toBeVisible();
    await expect(body.locator('strong')).toHaveText('bold');
    await expect(body.locator('code')).toHaveText('code');
    const link = body.locator('a');
    await expect(link).toHaveText('link');
    await expect(link).toHaveAttribute('href', 'https://test.com');
  });
});

test.describe('Diff Comments — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Switch to unified mode
    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
  });

  test('comments work on addition lines in unified mode', async ({ page }) => {
    const section = goSection(page);

    const additionLine = section.locator('.diff-container.unified .diff-line.addition').first();
    await expect(additionLine).toBeVisible();
    await additionLine.hover();

    const commentBtn = additionLine.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Unified mode comment');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Unified mode comment');
  });

  test('comment form in unified mode has capped max-width', async ({ page }) => {
    const section = goSection(page);

    const additionLine = section.locator('.diff-container.unified .diff-line.addition').first();
    await additionLine.hover();
    const commentBtn = additionLine.locator('.diff-comment-btn');
    await commentBtn.click();

    const formWrapper = section.locator('.comment-form-wrapper');
    await expect(formWrapper).toBeVisible();

    const box = await formWrapper.boundingBox();
    expect(box).toBeTruthy();
    // In unified mode, max-width is 900px. With tolerance allow up to 910px.
    expect(box!.width).toBeLessThanOrEqual(910);
  });
});

// ============================================================
// Cross-file comment behavior
// ============================================================
test.describe('Cross-File Comments', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('opening a comment form on one file keeps form on another file open', async ({ page }) => {
    await loadPage(page);

    // Open comment form on server.go (diff file)
    const serverSection = goSection(page);
    const additionSide = serverSection.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();

    // Form should be open
    await expect(serverSection.locator('.comment-form')).toBeVisible();
    await expect(page.locator('.comment-form')).toHaveCount(1);

    // Now open comment form on handler.js
    const handlerSection = jsSection(page);
    const jsAdditionSide = handlerSection.locator('.diff-split-side.addition').first();
    await jsAdditionSide.hover();
    await jsAdditionSide.locator('.diff-comment-btn').click();

    // Both forms should be visible
    await expect(page.locator('.comment-form')).toHaveCount(2);

    // Both file sections should have their form
    await expect(serverSection.locator('.comment-form')).toBeVisible();
    await expect(handlerSection.locator('.comment-form')).toBeVisible();
  });
});

// ============================================================
// Author Badge Rendering
// ============================================================
test.describe('Author Badges', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('displays author badge when comment has author field', async ({ page, request }) => {
    const resp = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Test comment', author: 'reviewer1' }
    });
    expect(resp.ok()).toBeTruthy();

    await loadPage(page);
    await switchToDocumentView(page);

    const badge = page.locator('.comment-author-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('@reviewer1');
  });

  test('does not display author badge when comment has no author', async ({ page, request }) => {
    const resp = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Local comment' }
    });
    expect(resp.ok()).toBeTruthy();

    await loadPage(page);
    await switchToDocumentView(page);

    await expect(page.locator('.comment-card')).toHaveCount(1);
    await expect(page.locator('.comment-author-badge')).toHaveCount(0);
  });

  test('does not display author badge when author is empty string', async ({ page, request }) => {
    const resp = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Empty author comment', author: '' }
    });
    expect(resp.ok()).toBeTruthy();

    await loadPage(page);
    await switchToDocumentView(page);

    await expect(page.locator('.comment-card')).toHaveCount(1);
    await expect(page.locator('.comment-author-badge')).toHaveCount(0);
  });

  test('color-codes different authors distinctly', async ({ page, request }) => {
    const resp1 = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Comment A', author: 'alice' }
    });
    expect(resp1.ok()).toBeTruthy();
    const resp2 = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 2, end_line: 2, body: 'Comment B', author: 'bob' }
    });
    expect(resp2.ok()).toBeTruthy();

    await loadPage(page);
    await switchToDocumentView(page);

    const badges = page.locator('.comment-author-badge');
    await expect(badges).toHaveCount(2);

    // Verify both have color styles and they are different from each other
    const style0 = await badges.nth(0).getAttribute('style');
    const style1 = await badges.nth(1).getAttribute('style');
    expect(style0).toContain('background:');
    expect(style1).toContain('background:');
    expect(style0).not.toEqual(style1);
  });

  test('displays author badge on diff comments', async ({ page, request }) => {
    // Add a comment with author on a code file (shown in diff view by default).
    // Line 5 is the added "log" import — guaranteed to be in the diff hunk.
    const resp = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: 5, end_line: 5, body: 'Diff comment', author: 'reviewer1' }
    });
    expect(resp.ok()).toBeTruthy();

    await loadPage(page);

    const section = goSection(page);
    const badge = section.locator('.comment-author-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('@reviewer1');
  });
});
