import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, getMdPath, switchToDocumentView, openDocLineComment } from './helpers';

// ============================================================
// Send to Agent — Git Mode
// The endpoint's config flag, review-level and unknown-comment cases are
// covered in Go (internal/server: TestHandleConfig_AgentCmdEnabled,
// TestHandleAgentRequest_ReviewLevelComment, TestHandleAgentRequest_CommentNotFound).
// ============================================================
test.describe('Send to Agent', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('Send now on a line comment submits it and sends it to the agent', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);

    const textarea = await openDocLineComment(page, doc);
    const sendBtn = page.locator('.btn-agent');
    await expect(sendBtn).toBeVisible();
    await expect(sendBtn).toHaveText('Send now');

    await textarea.fill('Please review this section');

    // Guard against scope inversion: line Send now must not post empty file_path.
    const agentReq = page.waitForRequest((req) =>
      req.url().includes('/api/agent/request') && req.method() === 'POST'
    );
    await sendBtn.click();

    const posted = await agentReq;
    expect(posted.postDataJSON().file_path).toBeTruthy();

    // Verify toast and pending reply indicator appear
    await expect(page.locator('.mini-toast')).toContainText('Sent to agent');
    await expect(page.locator('.agent-pending-reply')).toBeVisible();
    await expect(page.locator('.agent-pending-author')).toHaveText('@echo');
  });

  test('Ctrl+Shift+Enter in a new comment acts as Send now', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const textarea = await openDocLineComment(page, doc);
    await textarea.fill('Sent with the shortcut');

    const agentReq = page.waitForRequest((req) =>
      req.url().includes('/api/agent/request') && req.method() === 'POST'
    );
    await textarea.press('Control+Shift+Enter');

    expect((await agentReq).postDataJSON().file_path).toBeTruthy();
    await expect(page.locator('.mini-toast')).toContainText('Sent to agent');
    await expect(page.locator('.agent-pending-reply')).toBeVisible();
  });

  test('Ctrl+Enter only saves the comment', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const textarea = await openDocLineComment(page, doc);
    await textarea.fill('Saved, not sent');
    let agentRequests = 0;
    page.on('request', (req) => {
      if (req.url().includes('/api/agent/request')) agentRequests++;
    });

    await textarea.press('Control+Enter');

    await expect(doc.locator('.comment-card')).toContainText('Saved, not sent');
    await page.waitForTimeout(300);
    expect(agentRequests).toBe(0);
  });

  test('POST /api/agent/request returns 202 and echoes the comment', async ({ request }) => {
    const mdPath = await getMdPath(request);

    const commentRes = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 5, end_line: 5, body: 'Is this safe?' },
    });
    const comment = await commentRes.json();

    const res = await request.post('/api/agent/request', {
      data: { comment_id: comment.id, file_path: mdPath },
    });
    expect(res.status()).toBe(202);
    const body = await res.json();
    expect(body.status).toBe('accepted');
    expect(body.comment_id).toBe(comment.id);
  });

  // Used to run the file-scoped submit path with an empty path and bail silently.
  test('Send now submits a review-level comment and sends it to agent', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('Shift+G');

    const textarea = page.locator('#reviewConversation .comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('Describe the problem and how we fix it');

    await page.locator('#reviewConversation .btn-agent').click();

    const cards = page.locator('#reviewConversation .comment-card');
    await expect(cards).toHaveCount(1);
    await expect(cards.first()).toContainText('Describe the problem and how we fix it');

    await expect(page.locator('.mini-toast')).toContainText('Sent to agent');
    await expect(page.locator('#reviewConversation .agent-pending-reply')).toBeVisible();
    await expect(page.locator('#reviewConversation .agent-pending-author')).toHaveText('@echo');
  });
});
