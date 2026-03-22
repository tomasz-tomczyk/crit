import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, addComment, getMdPath, switchToDocumentView } from './helpers';

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

  test('agent button visible on comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 5, 'Test comment for agent');
    await loadPage(page);

    const agentBtn = page.locator('.agent-btn').first();
    await expect(agentBtn).toBeVisible();
    await expect(agentBtn).toHaveAttribute('title', 'Send now');
  });

  test('agent button sends request and shows toast', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 5, 'Please explain this code');
    await loadPage(page);

    const agentBtn = page.locator('.agent-btn').first();
    await agentBtn.click();

    // Verify toast appears
    await expect(page.locator('.mini-toast')).toContainText('Sent to agent');
  });

  test('waiting indicator appears after sending', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 5, 'Review this section');
    await loadPage(page);
    await switchToDocumentView(page);

    const agentBtn = page.locator('.agent-btn').first();
    await agentBtn.click();

    await expect(page.locator('.agent-waiting')).toBeVisible();
    await expect(page.locator('.agent-waiting')).toContainText('Waiting for agent response');
  });

  test('POST /api/agent/request returns 202', async ({ request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 5, 'Is this safe?');

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
