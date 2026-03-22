import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, getMdPath, mdSection, switchToDocumentView } from './helpers';

// ============================================================
// Send to Agent — Git Mode
// ============================================================
test.describe('Send to Agent', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('config API reports agent_cmd_enabled', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.agent_cmd_enabled).toBe(true);
  });

  test('Send now button visible on comment form', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    const gutterBtn = section.locator('.line-comment-gutter').first();
    await gutterBtn.click();

    const sendBtn = page.locator('.btn-agent');
    await expect(sendBtn).toBeVisible();
    await expect(sendBtn).toHaveText('Send now');
  });

  test('Send now submits comment and sends to agent', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    const gutterBtn = section.locator('.line-comment-gutter').first();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Please review this section');

    const sendBtn = page.locator('.btn-agent');
    await sendBtn.click();

    // Verify toast and pending reply indicator appear
    await expect(page.locator('.mini-toast')).toContainText('Sent to agent');
    await expect(page.locator('.agent-pending-reply')).toBeVisible();
    await expect(page.locator('.agent-pending-author')).toHaveText('@agent');
  });

  test('POST /api/agent/request returns 202', async ({ request }) => {
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

  test('POST /api/agent/request returns 404 for unknown comment', async ({ request }) => {
    const res = await request.post('/api/agent/request', {
      data: { comment_id: 'nonexistent', file_path: 'plan.md' },
    });
    expect(res.status()).toBe(404);
  });
});
