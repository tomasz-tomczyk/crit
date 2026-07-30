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

    // Escape collapses back to the compact input, but the buttons stay put.
    await card.locator('.reply-textarea').press('Escape');
    await expect(card.locator('.reply-input')).toBeVisible();
    await expect(card.locator('.reply-textarea')).toHaveCount(0);
    await expect(card.locator('.reply-form-buttons')).toBeVisible();
  });

  test('send buttons are visible on an open card and hidden with a collapsed one', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    await expect(card.locator('.reply-form-buttons .btn-primary')).toBeVisible();

    await card.locator('.comment-collapse-btn').click();
    await expect(card).toHaveClass(/collapsed/);
    await expect(card.locator('.reply-form-buttons')).toBeHidden();

    await card.locator('.comment-collapse-btn').click();
    await expect(card.locator('.reply-form-buttons .btn-primary')).toBeVisible();
  });

  test('can send a reply without expanding the box', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    await card.locator('.reply-input').fill('sent without expanding');
    await card.locator('.reply-form-buttons .btn-primary').click();

    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('sent without expanding');
    await expect(card.locator('.reply-input')).toHaveValue('');
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

  test('reply form collapses and clears after successful submit', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Addressed this');
    await card.locator('.reply-form .btn-primary').click();

    // Reply rendered
    await expect(section.locator('.comment-reply')).toHaveCount(1);

    // Form should collapse back to compact input, NOT remain expanded with re-populated text
    await expect(card.locator('.reply-input')).toBeVisible();
    await expect(card.locator('.reply-form.expanded')).toHaveCount(0);
    await expect(card.locator('.reply-textarea')).toHaveCount(0);
    await expect(card.locator('.reply-input')).toHaveValue('');
  });

  test('opening a new reply form closes other empty expanded reply forms', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 2, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const firstCard = section.locator('.comment-card').filter({ hasText: 'First comment' });
    const secondCard = section.locator('.comment-card').filter({ hasText: 'Second comment' });
    await expect(firstCard).toBeVisible();
    await expect(secondCard).toBeVisible();

    // Expand first reply form (leave empty)
    await firstCard.locator('.reply-input').click();
    await expect(firstCard.locator('.reply-form.expanded')).toHaveCount(1);

    // Expand second reply form
    await secondCard.locator('.reply-input').click();
    await expect(secondCard.locator('.reply-form.expanded')).toHaveCount(1);

    // First (empty) reply form should collapse
    await expect(firstCard.locator('.reply-form.expanded')).toHaveCount(0);
    await expect(firstCard.locator('.reply-input')).toBeVisible();
  });

  test('opening a new reply form keeps other reply forms with text', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 2, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const firstCard = section.locator('.comment-card').filter({ hasText: 'First comment' });
    const secondCard = section.locator('.comment-card').filter({ hasText: 'Second comment' });

    await firstCard.locator('.reply-input').click();
    await firstCard.locator('.reply-textarea').fill('draft reply');

    await secondCard.locator('.reply-input').click();

    // Both reply forms expanded; first retains its text
    await expect(firstCard.locator('.reply-form.expanded')).toHaveCount(1);
    await expect(secondCard.locator('.reply-form.expanded')).toHaveCount(1);
    await expect(firstCard.locator('.reply-textarea')).toHaveValue('draft reply');
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

  test('Ctrl+Enter saves edits to a reply', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this');
    await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: 'Original reply', author: 'reviewer' },
    });
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const reply = section.locator('.comment-reply').first();
    await expect(reply).toBeVisible();

    // Hover to reveal actions, click Edit
    await reply.hover();
    await reply.locator('.reply-actions button[title="Edit"]').click();

    const textarea = reply.locator('textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Original reply');

    await textarea.fill('Edited reply via Ctrl+Enter');
    await textarea.press('Control+Enter');

    await expect(section.locator('.reply-body')).toContainText('Edited reply via Ctrl+Enter');
    await expect(reply.locator('textarea')).toHaveCount(0);
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

  // Expanding used to push Reply off screen with nothing scrolling it back.
  test('expanding a reply near the bottom edge keeps Reply on screen', async ({ page, request }) => {
    // Pinned: the bug is geometric, so don't depend on the default window size.
    await page.setViewportSize({ width: 1200, height: 500 });
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Reply to me');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Park the collapsed reply input just above the bottom edge.
    await page.evaluate(() => {
      const el = document.querySelector('.comment-card .reply-input');
      if (!el) throw new Error('no reply input');
      const r = el.getBoundingClientRect();
      window.scrollBy(0, r.top - (window.innerHeight - 60));
    });

    // locator.click() and boundingBox() scroll the target into view first,
    // which would move the page out of the state under test.
    const point = await page.evaluate(() => {
      const el = document.querySelector('.comment-card .reply-input');
      if (!el) throw new Error('no reply input');
      const r = el.getBoundingClientRect();
      return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
    });
    await page.mouse.click(point.x, point.y);

    await expect(card.locator('.reply-textarea')).toBeVisible();
    const replyBtn = card.locator('.reply-form-buttons .btn-primary');
    await expect(replyBtn).toBeVisible();

    // Measured in-page for the same reason. Retried because html has
    // scroll-behavior: smooth, so the scroll is animated.
    await expect(async () => {
      const geom = await page.evaluate(() => {
        const btn = document.querySelector('.comment-card .reply-form-buttons .btn-primary');
        if (!btn) throw new Error('no Reply button');
        const r = btn.getBoundingClientRect();
        return { bottom: Math.round(r.bottom), viewportHeight: window.innerHeight };
      });
      expect(geom.bottom).toBeLessThanOrEqual(geom.viewportHeight);
    }).toPass({ timeout: 5000 });
  });

  // Closing an empty form re-renders the file, which used to detach the very
  // reply form being expanded — the box stayed one line tall.
  test('reply form still expands when an empty comment form is open on the same file', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Reply to me');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Open a second, empty comment form on the same file via the gutter.
    await section.locator('.line-block').nth(2).hover();
    await section.locator('.line-comment-gutter').nth(2).click();
    await expect(section.locator('.comment-form')).toHaveCount(1);

    // Now expand the thread's reply box.
    await card.locator('.reply-input').click();

    await expect(card.locator('.reply-form.expanded')).toHaveCount(1);
    await expect(card.locator('.reply-textarea')).toBeVisible();
    await expect(card.locator('.reply-form-buttons .btn-primary')).toBeVisible();
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

  test('resolving a live thread collapses it', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this bug');

    // Send comment to agent — this sets comment.live = true server-side
    // (agent_cmd is "echo", so the request succeeds and adds an agent reply)
    const agentRes = await request.post('/api/agent/request', {
      data: { comment_id: comment.id, file_path: mdPath },
    });
    expect(agentRes.ok()).toBeTruthy();

    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Live thread should be expanded (not collapsed) and have live-thread styling
    await expect(section.locator('.comment-block.live-thread')).toHaveCount(1);
    await expect(card).not.toHaveClass(/collapsed/);

    // Hover and resolve
    await card.hover();
    await card.locator('.comment-actions button[title="Resolve"]').click();

    // After resolving, card should collapse even though it was a live thread
    await expect(section.locator('.comment-card.collapsed')).toHaveCount(1);
  });

  test('reply form persists expanded state and text when opening a new comment form on same file', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Expand reply input and type text
    await card.locator('.reply-input').click();
    await expect(card.locator('.reply-textarea')).toBeFocused();
    await card.locator('.reply-textarea').fill('draft reply text');
    await expect(card.locator('.reply-form-buttons')).toBeVisible();

    // Open a new comment form on a different line of the SAME file to trigger re-render
    const thirdLineBlock = section.locator('.line-block').nth(2);
    await thirdLineBlock.hover();
    await section.locator('.line-comment-gutter').nth(2).click();

    // Verify the new comment form opened
    await expect(section.locator('.comment-form')).toBeVisible();

    // Verify reply form survived the re-render
    await expect(card.locator('.reply-form.expanded')).toBeVisible();
    await expect(card.locator('.reply-textarea')).toHaveValue('draft reply text');
    await expect(card.locator('.reply-form-buttons')).toBeVisible();
  });
});
