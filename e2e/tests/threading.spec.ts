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
    const comment = await addComment(request, mdPath, 1, 'Fix this');

    // Add a reply via API
    const replyRes = await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Done, fixed it', author: 'agent' },
    });
    expect(replyRes.status()).toBe(201);
    const reply = await replyRes.json();
    expect(reply.id).toBeTruthy();

    // Load page, switch to document view, verify reply renders
    await loadPage(page);
    await switchToDocumentView(page);
    const section = mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('Done, fixed it');
  });

  test('reply input expands on focus and collapses on Escape', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Compact reply input should be visible at bottom of card
    const replyInput = card.locator('.reply-input');
    await expect(replyInput).toBeVisible();

    // Click to expand
    await replyInput.click();
    await expect(card.locator('.reply-textarea')).toBeFocused();
    await expect(card.locator('.reply-form-buttons')).toBeVisible();

    // Escape collapses back to compact input
    await card.locator('.reply-textarea').press('Escape');
    await expect(card.locator('.reply-input')).toBeVisible();
    await expect(card.locator('.reply-form-buttons')).toHaveCount(0);
  });

  test('submitting reply form adds reply to thread', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Click reply input to expand, fill and submit
    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Addressed this');
    await card.locator('.reply-form .btn-primary').click();

    // Verify reply appears
    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('Addressed this');
  });

  test('reply form supports Ctrl+Enter submit', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Check this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Fixed via Ctrl+Enter');
    await card.locator('.reply-textarea').press('Control+Enter');

    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('Fixed via Ctrl+Enter');
  });

  test('reply form Cancel collapses without submitting', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Check this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Expand the reply input
    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('draft text');

    // Click Cancel
    await card.locator('.reply-form-buttons .btn:not(.btn-primary)').click();

    // Should collapse back to compact input, no reply added
    await expect(card.locator('.reply-input')).toBeVisible();
    await expect(section.locator('.comment-reply')).toHaveCount(0);
  });

  test('panel shows replies inline', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Check this');
    await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Done', author: 'agent' },
    });
    await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Thanks', author: 'reviewer' },
    });
    await loadPage(page);

    // Open comments panel
    await page.keyboard.press('Shift+C');
    const card = page.locator('.panel-comment-block .comment-card').first();
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-reply')).toHaveCount(2);
  });

  test('can delete a reply', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this');
    await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
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

  test('resolve button marks comment as resolved', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Fix this bug');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Hover to reveal actions, click resolve
    await card.hover();
    await card.locator('.comment-actions button[title="Resolve"]').click();

    // Card should now show Unresolve button and be collapsed
    await expect(section.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();
    await expect(section.locator('.comment-card.collapsed')).toHaveCount(1);
  });

  test('unresolve button restores comment to active', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this bug');

    // Resolve via API
    await request.fetch(`/api/comment/${comment.id}/resolve?path=${encodeURIComponent(mdPath)}`, {
      method: 'PUT',
      data: { resolved: true },
    });

    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);

    // Expand the resolved card
    await section.locator('.comment-collapse-btn').click();
    await expect(section.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();

    // Hover to reveal unresolve, click it
    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Unresolve"]').click();

    // Should no longer have Unresolve button, card should be expanded
    await expect(section.locator('.comment-actions button[title="Unresolve"]')).toHaveCount(0);
    await expect(section.locator('.comment-card:not(.collapsed)')).toHaveCount(1);
  });

  test('collapse chevron toggles comment body visibility', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Collapsible comment');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toBeVisible();

    // Click collapse chevron
    await card.locator('.comment-collapse-btn').click();
    await expect(card).toHaveClass(/collapsed/);
    await expect(card.locator('.comment-body')).not.toBeVisible();

    // Click again to expand
    await card.locator('.comment-collapse-btn').click();
    await expect(card).not.toHaveClass(/collapsed/);
    await expect(card.locator('.comment-body')).toBeVisible();
  });

  test('resolved comment renders as full card with badge', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Needs work');
    await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Fixed it', author: 'agent' },
    });
    await request.fetch(`/api/comment/${comment.id}/resolve?path=${encodeURIComponent(mdPath)}`, {
      method: 'PUT',
      data: { resolved: true },
    });

    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');

    // Collapsed by default with Unresolve button
    await expect(card).toHaveClass(/collapsed/);
    await expect(section.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();

    // Expand — should show body, reply, and reply input
    await card.locator('.comment-collapse-btn').click();
    await expect(card.locator('.comment-body')).toContainText('Needs work');
    await expect(card.locator('.comment-reply')).toHaveCount(1);
    await expect(card.locator('.reply-body')).toContainText('Fixed it');
    await expect(card.locator('.reply-input')).toBeVisible();
  });
});
