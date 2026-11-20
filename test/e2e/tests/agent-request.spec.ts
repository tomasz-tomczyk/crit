import { test, expect, type Page } from '@playwright/test';
import {
  addComment, addReviewComment, clearAllComments, goSection, loadPage, getMdPath,
  switchToDocumentView, openDocLineComment,
} from './helpers';

// Hold every agent request until the returned function is called, then fail
// it as a server with no agent left does.
async function holdAgentRequests(page: Page) {
  let fail!: () => void;
  const held = new Promise<void>((resolve) => { fail = resolve; });
  await page.route('**/api/agent/request', async (route) => {
    await held;
    await route.fulfill({ status: 400, body: 'no agent' });
  });
  return fail;
}

function countAgentRequests(page: Page) {
  const seen = { count: 0 };
  page.on('request', (req) => {
    if (req.url().includes('/api/agent/request')) seen.count++;
  });
  return seen;
}

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
    const agentRequests = countAgentRequests(page);

    await textarea.press('Control+Enter');

    await expect(doc.locator('.comment-card')).toContainText('Saved, not sent');
    await page.waitForTimeout(300);
    expect(agentRequests.count).toBe(0);
  });

  test('Send now in a reply sends it and makes the thread live', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const textarea = await openDocLineComment(page, doc);
    await textarea.fill('Saved first, sent later');
    await textarea.press('Control+Enter');
    const card = doc.locator('.comment-card', { hasText: 'Saved first, sent later' });
    await expect(card.locator('.live-thread-badge')).toHaveCount(0);

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Please look at this now');
    const agentReq = page.waitForRequest((req) =>
      req.url().includes('/api/agent/request') && req.method() === 'POST'
    );
    await card.locator('.reply-form .btn-agent').click();

    expect((await agentReq).postDataJSON().file_path).toBeTruthy();
    await expect(page.locator('.mini-toast')).toContainText('Sent to agent');
    const liveCard = doc.locator('.comment-card', { hasText: 'Saved first, sent later' });
    await expect(liveCard).toContainText('Please look at this now');
    await expect(liveCard.locator('.live-thread-badge')).toBeVisible();

    // The badge also shows while the request is pending, and a reload clears
    // that: only a thread the server marked live keeps it.
    await loadPage(page);
    const reloaded = (await switchToDocumentView(page)).locator('.comment-card', { hasText: 'Saved first, sent later' });
    await expect(reloaded.locator('.live-thread-badge')).toBeVisible();
    await expect(reloaded.locator('.agent-pending-reply')).toHaveCount(0);
  });

  test('Ctrl+Shift+Enter in a reply sends the thread; Ctrl+Enter only saves it', async ({ page, request }) => {
    await addComment(request, await getMdPath(request), 1, 'Saved thread');
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const card = doc.locator('.comment-card', { hasText: 'Saved thread' });
    const agentRequests = countAgentRequests(page);

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Saved reply');
    await card.locator('.reply-textarea').press('Control+Enter');
    await expect(card.locator('.comment-reply')).toHaveCount(1);
    await page.waitForTimeout(300);
    expect(agentRequests.count).toBe(0);

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Sent reply');
    await card.locator('.reply-textarea').press('Control+Shift+Enter');
    await expect(page.locator('.mini-toast')).toContainText('Sent to agent');
    await expect(card.locator('.comment-reply')).toHaveCount(2);
    expect(agentRequests.count).toBe(1);
  });

  test('Ctrl+Shift+Enter in the edit form saves without sending', async ({ page, request }) => {
    await addComment(request, await getMdPath(request), 1, 'Before the edit');
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const agentRequests = countAgentRequests(page);

    await doc.locator('.comment-card .comment-actions button[title="Edit"]').click();
    const textarea = doc.locator('.comment-form textarea');
    await textarea.fill('After the edit');
    await textarea.press('Control+Shift+Enter');

    await expect(doc.locator('.comment-card .comment-body')).toContainText('After the edit');
    await page.waitForTimeout(300);
    expect(agentRequests.count).toBe(0);
  });

  test('no Send now in the reply form of a live or waiting thread', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const live = await addComment(request, mdPath, 1, 'Already live');
    await expect(await request.post('/api/agent/request', {
      data: { comment_id: live.id, file_path: mdPath },
    })).toBeOK();
    await addComment(request, mdPath, 3, 'Saved first');
    await loadPage(page);
    const doc = await switchToDocumentView(page);

    const liveCard = doc.locator('.comment-card', { hasText: 'Already live' });
    await liveCard.locator('.reply-input').click();
    await expect(liveCard.locator('.reply-form .btn-primary')).toBeVisible();
    await expect(liveCard.locator('.reply-form .btn-agent')).toHaveCount(0);

    const waitingCard = doc.locator('.comment-card', { hasText: 'Saved first' });
    await waitingCard.locator('.reply-input').click();
    await waitingCard.locator('.reply-textarea').fill('Over to you');
    await waitingCard.locator('.reply-form .btn-agent').click();
    await expect(waitingCard.locator('.reply-input')).toBeDisabled();
    await expect(waitingCard.locator('.reply-form .btn-agent')).toHaveCount(0);
  });

  test('Send now in the reply form of a review comment, and its failure', async ({ page, request }) => {
    await addReviewComment(request, 'Review-level thread');
    const failAgentRequest = await holdAgentRequests(page);
    await loadPage(page);
    const card = page.locator('#reviewConversation .comment-card', { hasText: 'Review-level thread' });

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Please look at this now');
    await card.locator('.reply-form .btn-agent').click();
    await expect(card).toContainText('Please look at this now');
    await expect(card.locator('.reply-input')).toHaveAttribute('placeholder', /^Waiting for @/);

    failAgentRequest();

    await expect(page.locator('.mini-toast')).toContainText('Failed to send to agent');
    await expect(card.locator('.reply-input')).toBeEnabled();
  });

  test('Send now in a diff thread, and its failure', async ({ page, request }) => {
    await addComment(request, 'server.go', 5, 'Diff thread');
    const failAgentRequest = await holdAgentRequests(page);
    await loadPage(page);
    const card = (await goSection(page)).locator('.comment-card', { hasText: 'Diff thread' });

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Please look at this now');
    await card.locator('.reply-form .btn-agent').click();
    await expect(card).toContainText('Please look at this now');
    await expect(card.locator('.reply-input')).toHaveAttribute('placeholder', /^Waiting for @/);

    failAgentRequest();

    await expect(page.locator('.mini-toast')).toContainText('Failed to send to agent');
    await expect(card.locator('.reply-input')).toBeEnabled();
  });

  test('the shortcuts list has Send now when an agent is set up', async ({ page }) => {
    await loadPage(page);
    await page.locator('#settingsToggle').click();
    await page.locator('#tab-shortcuts').click();
    await expect(page.locator('#shortcutsPane')).toContainText('Comment and send to the agent');
  });

  test('a failed Send now in a reply frees the reply box', async ({ page }) => {
    // Fail the request only once the saved reply has redrawn the thread as waiting.
    const failAgentRequest = await holdAgentRequests(page);
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await page.keyboard.press('Shift+C');
    const textarea = await openDocLineComment(page, doc);
    await textarea.fill('Saved first, sent later');
    await textarea.press('Control+Enter');
    const card = doc.locator('.comment-card', { hasText: 'Saved first, sent later' });
    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Please look at this now');
    await card.locator('.reply-form .btn-agent').click();
    await expect(card).toContainText('Please look at this now');
    await expect(card.locator('.reply-input')).toHaveAttribute('placeholder', /^Waiting for @/);
    const panelPending = page.locator('.panel-comment-block .agent-pending-reply');
    await expect(panelPending).toHaveCount(1);

    failAgentRequest();

    await expect(page.locator('.mini-toast')).toContainText('Failed to send to agent');
    await expect(card.locator('.reply-input')).toHaveAttribute('placeholder', 'Write a reply…');
    await expect(card.locator('.reply-input')).toBeEnabled();
    await expect(panelPending).toHaveCount(0);
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
