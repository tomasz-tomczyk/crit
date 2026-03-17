import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath } from './helpers';

// ============================================================
// Comment Threading — Git Mode
// ============================================================
test.describe('Comment Threading', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('can add a reply via API and see it rendered', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Fix this');

    // Add a reply via API
    const replyRes = await request.post(`/api/comment/c1/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Done, fixed it', author: 'agent' },
    });
    expect(replyRes.status()).toBe(201);
    const reply = await replyRes.json();
    expect(reply.id).toBe('c1-r1');

    // Load page, switch to document view, verify reply renders
    await loadPage(page);
    await switchToDocumentView(page);
    const section = mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('Done, fixed it');
  });

  test('reply button opens and closes reply form', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Hover comment card to reveal actions, then click Reply
    await card.hover();
    const replyBtn = section.locator('.comment-actions button[title="Reply"]');
    await replyBtn.click();
    await expect(page.locator('.reply-form')).toBeVisible();
    await expect(page.locator('.reply-textarea')).toBeFocused();

    // Toggle close — hover card again to keep actions visible
    await card.hover();
    await replyBtn.click();
    await expect(page.locator('.reply-form')).toHaveCount(0);
  });

  test('submitting reply form adds reply to thread', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();

    // Open form and submit
    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Reply"]').click();
    await page.locator('.reply-textarea').fill('Addressed this');
    await page.locator('.reply-form .btn-primary').click();

    // Verify reply appears
    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('Addressed this');
    // Form should be gone
    await expect(page.locator('.reply-form')).toHaveCount(0);
  });

  test('reply form supports Ctrl+Enter submit', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Check this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();

    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Reply"]').click();
    await page.locator('.reply-textarea').fill('Fixed via Ctrl+Enter');
    await page.locator('.reply-textarea').press('Control+Enter');

    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('Fixed via Ctrl+Enter');
  });

  test('reply form Escape cancels', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Check this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();

    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Reply"]').click();
    await expect(page.locator('.reply-form')).toBeVisible();
    await page.locator('.reply-textarea').press('Escape');
    await expect(page.locator('.reply-form')).toHaveCount(0);
  });

  test('panel shows reply count badge', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Check this');
    await request.post(`/api/comment/c1/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Done', author: 'agent' },
    });
    await request.post(`/api/comment/c1/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Thanks', author: 'reviewer' },
    });
    await loadPage(page);

    // Open comments panel
    await page.keyboard.press('Shift+C');
    await expect(page.locator('.comments-panel-badge-replies')).toContainText('2 replies');
  });

  test('can delete a reply', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Fix this');
    await request.post(`/api/comment/c1/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Done', author: 'agent' },
    });
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);

    // Verify reply exists
    await expect(section.locator('.comment-reply')).toHaveCount(1);

    // Hover to reveal actions, click delete
    await section.locator('.comment-reply').hover();
    await section.locator('.comment-reply .delete-btn').click();

    // Reply should be gone
    await expect(section.locator('.comment-reply')).toHaveCount(0);
  });
});
