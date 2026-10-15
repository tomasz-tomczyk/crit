import { test, expect } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, jsSection,
  switchToDocumentView, openLineComment, hoverLine, diffLine, setDiffStyle, openDocLineComment,
} from './helpers';

// How comment bodies render (markdown, code, links, tables) is covered by
// comment-markdown.spec.ts; these tests cover the form and card lifecycle.

// ============================================================
// Markdown Comments (git mode — plan.md in document view)
// ============================================================
test.describe('Markdown Comments — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('gutter form: open focused, Escape, submit, Ctrl+Enter, edit, delete', async ({ page }) => {
    await loadPage(page);
    const section = await switchToDocumentView(page);
    const form = section.locator('.comment-form');

    // The gutter opens a focused form for the block's exact source lines.
    const lineBlock = section.locator('.line-block').first();
    const startLine = await lineBlock.getAttribute('data-start-line');
    const endLine = await lineBlock.getAttribute('data-end-line');
    expect(startLine).not.toBeNull();
    expect(endLine).not.toBeNull();
    let textarea = await openDocLineComment(page, section, 0);
    await expect(textarea).toBeFocused();
    await expect(form.locator('.comment-form-header')).toHaveText(startLine === endLine
      ? `Comment on Line ${startLine}`
      : `Comment on Lines ${startLine}-${endLine}`);

    // Escape closes it.
    await textarea.press('Escape');
    await expect(form).toHaveCount(0);

    // Submit button creates a card.
    textarea = await openDocLineComment(page, section, 0);
    await textarea.fill('This is a test comment on markdown');
    await form.locator('.btn-primary').click();
    const card = section.locator('.comment-card', { hasText: 'This is a test comment on markdown' });
    await expect(card).toBeVisible();
    await expect(form).toHaveCount(0);

    // Ctrl+Enter submits a new comment too.
    textarea = await openDocLineComment(page, section, 1);
    await textarea.fill('Ctrl+Enter test');
    await textarea.press('Control+Enter');
    await expect(section.locator('.comment-card', { hasText: 'Ctrl+Enter test' })).toBeVisible();
    await expect(form).toHaveCount(0);

    // Edit opens the editor with the existing text and saves changes.
    await card.locator('.comment-actions button[title="Edit"]').click();
    await expect(form.locator('textarea')).toHaveValue('This is a test comment on markdown');
    await form.locator('textarea').clear();
    await form.locator('textarea').fill('Updated text');
    await form.locator('.btn-primary').click();
    const updated = section.locator('.comment-card', { hasText: 'Updated text' });
    await expect(updated.locator('.comment-body')).toContainText('Updated text');

    // Ctrl+Enter saves edits.
    await updated.locator('.comment-actions button[title="Edit"]').click();
    await expect(form.locator('textarea')).toHaveValue('Updated text');
    await form.locator('textarea').fill('Edited via Ctrl+Enter');
    await form.locator('textarea').press('Control+Enter');
    await expect(section.locator('.comment-card', { hasText: 'Edited via Ctrl+Enter' }).locator('.comment-body')).toContainText('Edited via Ctrl+Enter');
    await expect(form).toHaveCount(0);

    // Deleting removes the cards and updates the count.
    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(page.locator('#commentCountNumber')).toHaveText('2');
    for (let remaining = 1; remaining >= 0; remaining--) {
      await section.locator('.comment-actions .delete-btn').first().click();
      await expect(section.locator('.comment-card')).toHaveCount(remaining);
    }
    await expect(page.locator('#commentCountNumber')).toHaveText('');
  });
});

// ============================================================
// Diff Comments (git mode — code files in split/unified modes)
// ============================================================
// server.go line 5 is the added "log" import; handler.js is all additions.
test.describe('Diff Comments', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('split mode: + button opens a form; submitting creates a persisted card', async ({ page, request }) => {
    await loadPage(page);
    const section = await goSection(page);
    await expect(diffLine(section, 5)).toHaveAttribute('data-line-type', 'change-addition');

    // Hovering a diff line shows the + button on that row, not elsewhere.
    const commentBtn = await hoverLine(page, section, 5);
    await expect(commentBtn).toBeVisible();
    await expect.poll(async () => {
      const btnBox = await commentBtn.boundingBox();
      const lineBox = await diffLine(section, 5).boundingBox();
      if (!btnBox || !lineBox) return false;
      const mid = btnBox.y + btnBox.height / 2;
      return mid >= lineBox.y && mid <= lineBox.y + lineBox.height;
    }).toBe(true);

    const form = await openLineComment(page, section, 5);
    await expect(form).toBeVisible();
    await expect(section.locator('.comment-form')).toHaveCount(1);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 5');
    await expect(form.locator('textarea')).toBeFocused();

    // Fill + submit + card must be atomic: Pierre can remount the annotation
    // between fill and click, dropping the submit with no card. Guarded
    // against double-submit — a retry after a successful submit sees the
    // card and stops.
    const done = section.locator('.comment-card', { hasText: 'Diff comment in split mode' });
    await expect(async () => {
      if (await done.isVisible()) return;
      const current = section.locator('.comment-form').last();
      await current.locator('textarea').fill('Diff comment in split mode');
      await current.locator('.btn-primary').click();
      await expect(done).toBeVisible({ timeout: 5_000 });
    }).toPass({ timeout: 20_000 });
    await expect(done.locator('.comment-body')).toContainText('Diff comment in split mode');
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);

    // An all-addition file (handler.js) persists against the right file and line.
    const handler = await jsSection(page);
    await expect(diffLine(handler, 1)).toHaveAttribute('data-line-type', 'change-addition');
    const handlerForm = await openLineComment(page, handler, 1);
    await handlerForm.locator('textarea').fill('Comment on new code');
    await handlerForm.locator('.btn-primary').click();
    await expect(handler.locator('.comment-card .comment-body')).toContainText('Comment on new code');
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=handler.js')).json();
      return comments.map((c: { start_line: number; body: string }) => `${c.start_line}:${c.body}`);
    }).toEqual(['1:Comment on new code']);
  });

  test('unified mode: comments work on addition lines, and the form width is capped', async ({ page }) => {
    // Wide enough that an uncapped form would exceed --content-width.
    await page.setViewportSize({ width: 1800, height: 900 });
    await loadPage(page);
    await setDiffStyle(page, 'unified');
    const section = await goSection(page);
    await expect(section.locator('code[data-unified]')).toBeVisible();
    await expect(section.locator('code[data-additions]')).toHaveCount(0);
    await expect(diffLine(section, 5)).toHaveAttribute('data-line-type', 'change-addition');

    const form = await openLineComment(page, section, 5);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 5');

    // In unified mode, max-width is var(--content-width), defaulting to 1040px.
    const formWrapper = section.locator('.comment-form-wrapper');
    await expect(formWrapper).toBeVisible();
    const box = await formWrapper.boundingBox();
    expect(box).toBeTruthy();
    expect(box!.width).toBeLessThanOrEqual(1050);

    await form.locator('textarea').fill('Unified mode comment');
    await form.locator('.btn-primary').click();
    await expect(section.locator('.comment-card .comment-body')).toContainText('Unified mode comment');
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

  test('author badges show for authored comments only, colour-coded per author, in documents and diffs', async ({ page, request }) => {
    const post = async (path: string, line: number, body: string, author?: string) => {
      const resp = await request.post(`/api/file/comments?path=${path}`, {
        data: { start_line: line, end_line: line, body, ...(author === undefined ? {} : { author }) },
      });
      expect(resp.ok()).toBeTruthy();
      return (await resp.json()).id as string;
    };
    const reviewer = await post('plan.md', 1, 'Test comment', 'reviewer1');
    const local = await post('plan.md', 3, 'Local comment');
    const empty = await post('plan.md', 5, 'Empty author comment', '');
    const alice = await post('plan.md', 7, 'Comment A', 'alice');
    const bob = await post('plan.md', 9, 'Comment B', 'bob');
    // Line 5 is the added "log" import — guaranteed to be in the diff hunk.
    const diff = await post('server.go', 5, 'Diff comment', 'reviewer1');

    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const card = (id: string) => doc.locator(`.comment-card[data-comment-id="${id}"]`);
    await expect(doc.locator('.comment-card')).toHaveCount(5);

    await expect(card(reviewer).locator('.comment-author-badge')).toHaveText('@reviewer1');
    // No author, or an empty one: no badge.
    await expect(card(local).locator('.comment-body')).toContainText('Local comment');
    await expect(card(local).locator('.comment-author-badge')).toHaveCount(0);
    await expect(card(empty).locator('.comment-body')).toContainText('Empty author comment');
    await expect(card(empty).locator('.comment-author-badge')).toHaveCount(0);

    // Different authors get different colour classes.
    const colorOf = async (id: string) =>
      (await card(id).locator('.comment-author-badge').getAttribute('class'))?.match(/author-color-\d/)?.[0];
    const color0 = await colorOf(alice);
    const color1 = await colorOf(bob);
    expect(color0).toBeTruthy();
    expect(color1).toBeTruthy();
    expect(color0).not.toEqual(color1);

    // Diff comments show the badge too.
    const section = await goSection(page);
    const badge = section.locator(`.comment-card[data-comment-id="${diff}"] .comment-author-badge`);
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('@reviewer1');
  });
});
