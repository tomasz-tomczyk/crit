import { test, expect } from '@playwright/test';
import {
  clearAllComments, loadPage, mdSection, goSection, jsSection,
  switchToDocumentView, openLineComment, hoverLine, diffLine, setDiffStyle,
} from './helpers';

// ============================================================
// Markdown Comments (git mode — plan.md in document view)
// ============================================================
test.describe('Markdown Comments — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('gutter opens a focused comment form for the exact source line', async ({ page }) => {
    const section = await mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    const startLine = await lineBlock.getAttribute('data-start-line');
    const endLine = await lineBlock.getAttribute('data-end-line');
    expect(startLine).not.toBeNull();
    expect(endLine).not.toBeNull();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const form = page.locator('.comment-form');
    const textarea = page.locator('.comment-form textarea');
    await expect(form).toBeVisible();
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();
    const lineRef = startLine === endLine
      ? `Comment on Line ${startLine}`
      : `Comment on Lines ${startLine}-${endLine}`;
    await expect(form.locator('.comment-form-header')).toHaveText(lineRef);
  });

  test('submitting comment creates a comment card', async ({ page }) => {
    const section = await mdSection(page);
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
    const section = await mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Check this:\n```go\nfunc main() {\n\tfmt.Println("hello")\n}\n```');
    await page.locator('.comment-form .btn-primary').click();

    const body = section.locator('.comment-card .comment-body');
    await expect(body).toBeVisible();
    // Shiki upgrades the block asynchronously; tokens carry theme colors.
    const codeBlock = body.locator('pre code');
    await expect(codeBlock).toBeVisible();
    await expect(codeBlock).toHaveAttribute('data-crit-code', 'highlighted');
    await expect(codeBlock.locator('span[style*="--diffs-token"]').first()).toBeVisible();
  });

  test('comment with URL renders styled link', async ({ page }) => {
    const section = await mdSection(page);
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
    const section = await mdSection(page);
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
    const section = await mdSection(page);

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

  test('Ctrl+Enter saves edits to a comment', async ({ page }) => {
    const section = await mdSection(page);

    // Create a comment first
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Original');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Click Edit
    await section.locator('.comment-actions button[title="Edit"]').click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toHaveValue('Original');
    await textarea.fill('Edited via Ctrl+Enter');
    await textarea.press('Control+Enter');

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Edited via Ctrl+Enter');
    await expect(page.locator('.comment-form')).toHaveCount(0);
  });

  test('deleting a comment removes it and updates count', async ({ page }) => {
    const section = await mdSection(page);
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
    await expect(page.locator('#commentCountNumber')).toHaveText('');
  });

  test('pressing Escape closes the comment form', async ({ page }) => {
    const section = await mdSection(page);
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

  test('comment body renders markdown (bold, links, code)', async ({ page }) => {
    const section = await mdSection(page);
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
// server.go line 5 is the added "log" import; handler.js is all additions.
test.describe('Diff Comments — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('hovering a diff line shows the + button', async ({ page }) => {
    const section = await goSection(page);
    await expect(diffLine(section, 5)).toHaveAttribute('data-line-type', 'change-addition');

    const commentBtn = await hoverLine(page, section, 5);
    await expect(commentBtn).toBeVisible();
    // The button sits on the hovered row, not elsewhere in the file.
    const btnBox = await commentBtn.boundingBox();
    const lineBox = await diffLine(section, 5).boundingBox();
    expect(btnBox && lineBox).toBeTruthy();
    const mid = btnBox!.y + btnBox!.height / 2;
    expect(mid).toBeGreaterThanOrEqual(lineBox!.y);
    expect(mid).toBeLessThanOrEqual(lineBox!.y + lineBox!.height);
  });

  test('clicking + button on a diff line opens comment form', async ({ page }) => {
    const section = await goSection(page);
    const form = await openLineComment(page, section, 5);
    await expect(form).toBeVisible();
    await expect(section.locator('.comment-form')).toHaveCount(1);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 5');
    await expect(form.locator('textarea')).toBeFocused();
  });

  test('submitting a diff comment creates a comment card', async ({ page }) => {
    const section = await goSection(page);
    const form = await openLineComment(page, section, 5);
    await form.locator('textarea').fill('Diff comment in split mode');
    await form.locator('.btn-primary').click();

    // Comment card should appear in the diff section
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Diff comment in split mode');
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);
  });

  test('comments work on addition lines in split mode', async ({ page, request }) => {
    // Use handler.js which is an all-addition file
    const section = await jsSection(page);
    await expect(diffLine(section, 1)).toHaveAttribute('data-line-type', 'change-addition');

    const form = await openLineComment(page, section, 1);
    await form.locator('textarea').fill('Comment on new code');
    await form.locator('.btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on new code');

    // Persisted against the right file and line.
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=handler.js')).json();
      return comments.map((c: { start_line: number; body: string }) => `${c.start_line}:${c.body}`);
    }).toEqual(['1:Comment on new code']);
  });

  test('diff comment body renders markdown', async ({ page }) => {
    const section = await goSection(page);
    const form = await openLineComment(page, section, 5);
    await form.locator('textarea').fill('This has **bold**, `code`, and a [link](https://test.com)');
    await form.locator('.btn-primary').click();

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
    await setDiffStyle(page, 'unified');
    const section = await goSection(page);
    await expect(section.locator('code[data-unified]')).toBeVisible();
    await expect(section.locator('code[data-additions]')).toHaveCount(0);
  });

  test('comments work on addition lines in unified mode', async ({ page }) => {
    const section = await goSection(page);
    await expect(diffLine(section, 5)).toHaveAttribute('data-line-type', 'change-addition');

    const form = await openLineComment(page, section, 5);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 5');
    await form.locator('textarea').fill('Unified mode comment');
    await form.locator('.btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Unified mode comment');
  });

  test('comment form in unified mode has capped max-width', async ({ page }) => {
    // Wide enough that an uncapped form would exceed --content-width.
    await page.setViewportSize({ width: 1800, height: 900 });
    const section = await goSection(page);
    await openLineComment(page, section, 5);

    const formWrapper = section.locator('.comment-form-wrapper');
    await expect(formWrapper).toBeVisible();

    const box = await formWrapper.boundingBox();
    expect(box).toBeTruthy();
    // In unified mode, max-width is var(--content-width), defaulting to 1040px. With tolerance allow up to 1050px.
    expect(box!.width).toBeLessThanOrEqual(1050);
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
    const serverSection = await goSection(page);
    const serverForm = await openLineComment(page, serverSection, 5);
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(1);
    // Fill so it isn't auto-closed when the second form opens
    await serverForm.locator('textarea').fill('first');

    // Now open comment form on handler.js
    const handlerSection = await jsSection(page);
    await openLineComment(page, handlerSection, 1);
    await expect(handlerSection.locator('.comment-form')).toBeVisible();

    // Back on server.go, its draft form is still open with its text.
    const serverAgain = await goSection(page);
    await expect(serverAgain.locator('.comment-form textarea')).toHaveValue('first');
    await expect(serverAgain.locator('.comment-form')).toHaveCount(1);
    // And handler.js still has its own form.
    const handlerAgain = await jsSection(page);
    await expect(handlerAgain.locator('.comment-form')).toHaveCount(1);
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

    // Wait for the comment card to be visible before asserting on badge absence.
    // switchToDocumentView only waits for the document wrapper — comment rendering
    // may still be in progress, causing the count check to see 0 cards.
    await expect(page.locator('.comment-card')).toBeVisible();
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

    // Wait for the comment card to render before asserting badge absence.
    await expect(page.locator('.comment-card')).toBeVisible();
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

    // Verify both have author-color classes and they are different from each other
    const class0 = await badges.nth(0).getAttribute('class');
    const class1 = await badges.nth(1).getAttribute('class');
    const color0 = class0?.match(/author-color-\d/)?.[0];
    const color1 = class1?.match(/author-color-\d/)?.[0];
    expect(color0).toBeTruthy();
    expect(color1).toBeTruthy();
    expect(color0).not.toEqual(color1);
  });

  test('displays author badge on diff comments', async ({ page, request }) => {
    // Add a comment with author on a code file (shown in diff view by default).
    // Line 5 is the added "log" import — guaranteed to be in the diff hunk.
    const resp = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: 5, end_line: 5, body: 'Diff comment', author: 'reviewer1' }
    });
    expect(resp.ok()).toBeTruthy();

    await loadPage(page);

    const section = await goSection(page);
    const badge = section.locator('.comment-author-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('@reviewer1');
  });
});
