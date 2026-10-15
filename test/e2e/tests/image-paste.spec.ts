import { test, expect, type Page, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView, addComment, getMdPath, openDocLineComment } from './helpers';

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

const ATTACHMENT_REF_ALL = new RegExp(ATTACHMENT_REF_RE.source, 'g');

// The textarea's value once it holds `n` attachment refs (uploads are async:
// each placeholder is swapped for the real markdown ref when it finishes).
async function expectRefs(textarea: Locator, n: number) {
  await expect.poll(async () => ((await textarea.inputValue()).match(ATTACHMENT_REF_ALL) || []).length).toBe(n);
}

test.describe('Image paste & drag-drop', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  // Every composer wires paste and drop: new comments, replies, reply edits.
  test('pasting or dropping an image inserts a markdown ref in comment, reply and reply-edit textareas', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const top = await addComment(request, mdPath, 1, 'Top comment');
    const reply = await request.post(`/api/comment/${top.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Original reply', author: 'reviewer' },
    });
    expect(reply.ok()).toBeTruthy();
    const existing = await addComment(request, mdPath, 3, 'Existing comment');

    await loadPage(page);
    const doc = await switchToDocumentView(page);

    // New top-level comment form: paste, then drop.
    const textarea = await openDocLineComment(page, doc, 4);
    await pasteImage(page, textarea);
    await expectRefs(textarea, 1);
    await dropImage(page, textarea);
    await expectRefs(textarea, 2);

    // Reply form (regression for the top-level-only bug): paste, then drop.
    const card = doc.locator(`.comment-card[data-comment-id="${existing.id}"]`);
    await expect(card).toBeVisible();
    await card.locator('.reply-input').click();
    const replyTextarea = card.locator('.reply-textarea');
    await expect(replyTextarea).toBeFocused();
    await pasteImage(page, replyTextarea);
    await expectRefs(replyTextarea, 1);
    await dropImage(page, replyTextarea);
    await expectRefs(replyTextarea, 2);

    // Editing an existing reply.
    const replyEl = doc.locator(`.comment-card[data-comment-id="${top.id}"] .comment-reply`).first();
    await expect(replyEl).toBeVisible();
    await replyEl.hover();
    await replyEl.locator('.reply-actions button[title="Edit"]').click();
    const editTextarea = replyEl.locator('textarea');
    await expect(editTextarea).toBeVisible();
    await pasteImage(page, editTextarea);
    await expect(editTextarea).toHaveValue(/Original reply.*!\[[^\]]*\]\(attachments\/[0-9a-f-]+\.png\)/s);
  });
});
