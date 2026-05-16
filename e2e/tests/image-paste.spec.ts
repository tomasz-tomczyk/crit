import { test, expect, type Page, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath } from './helpers';

// Tiny 1x1 transparent PNG, base64-encoded. Decoded client-side and fed into
// the synthetic ClipboardEvent / DragEvent so the upload endpoint sees real
// image bytes that pass http.DetectContentType.
const PNG_1x1_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFAQH/k6sV0AAAAABJRU5ErkJggg==';

// Dispatch a synthetic ClipboardEvent('paste') on the focused element with a
// File built from base64 PNG bytes. Runs in the page context.
async function pasteImage(page: Page, target: Locator) {
  await target.focus();
  await target.evaluate((el, b64) => {
    const bytes = Uint8Array.from(atob(b64), c => c.charCodeAt(0));
    const file = new File([bytes], 'pasted-shot.png', { type: 'image/png' });
    const dt = new DataTransfer();
    dt.items.add(file);
    const event = new ClipboardEvent('paste', {
      bubbles: true,
      cancelable: true,
      clipboardData: dt,
    });
    el.dispatchEvent(event);
  }, PNG_1x1_BASE64);
}

// Dispatch synthetic dragover + drop events on the target with a File
// payload. Simulates the user dragging an image file in from the OS.
async function dropImage(page: Page, target: Locator) {
  await target.evaluate((el, b64) => {
    const bytes = Uint8Array.from(atob(b64), c => c.charCodeAt(0));
    const file = new File([bytes], 'dropped-shot.png', { type: 'image/png' });
    const dt = new DataTransfer();
    dt.items.add(file);
    const dragover = new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt });
    el.dispatchEvent(dragover);
    const drop = new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: dt });
    el.dispatchEvent(drop);
  }, PNG_1x1_BASE64);
}

// Match the markdown ref produced by uploadAndInsertImage on success.
const ATTACHMENT_REF_RE = /!\[[^\]]*\]\(attachments\/[0-9a-f-]+\.png\)/;

test.describe('Image paste & drag-drop', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('pasting image into new top-level comment form inserts markdown ref', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    await section.locator('.line-block').first().hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();

    await pasteImage(page, textarea);

    // The upload is async — wait for the placeholder to be swapped for the
    // real markdown ref. toHaveValue auto-retries up to the expect timeout.
    await expect(textarea).toHaveValue(ATTACHMENT_REF_RE);
  });

  test('pasting image into reply form inserts markdown ref (regression for top-level-only bug)', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Existing comment');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Click reply input to expand into the textarea.
    await card.locator('.reply-input').click();
    const replyTextarea = card.locator('.reply-textarea');
    await expect(replyTextarea).toBeFocused();

    await pasteImage(page, replyTextarea);

    await expect(replyTextarea).toHaveValue(ATTACHMENT_REF_RE);
  });

  test('dropping image file onto new comment textarea inserts markdown ref', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    await section.locator('.line-block').first().hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();

    await dropImage(page, textarea);

    await expect(textarea).toHaveValue(ATTACHMENT_REF_RE);
  });

  test('dropping image file onto reply textarea inserts markdown ref', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Existing comment');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    await card.locator('.reply-input').click();
    const replyTextarea = card.locator('.reply-textarea');
    await expect(replyTextarea).toBeFocused();

    await dropImage(page, replyTextarea);

    await expect(replyTextarea).toHaveValue(ATTACHMENT_REF_RE);
  });

  test('pasting image into reply EDIT textarea inserts markdown ref', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Top comment');
    await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Original reply', author: 'reviewer' },
    });
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const reply = section.locator('.comment-reply').first();
    await expect(reply).toBeVisible();

    // Open the reply edit form.
    await reply.hover();
    await reply.locator('.reply-actions button[title="Edit"]').click();
    const editTextarea = reply.locator('textarea');
    await expect(editTextarea).toBeVisible();

    await pasteImage(page, editTextarea);

    await expect(editTextarea).toHaveValue(/Original reply.*!\[[^\]]*\]\(attachments\/[0-9a-f-]+\.png\)/s);
  });
});
