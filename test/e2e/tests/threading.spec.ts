import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView, addComment, getMdPath, resolveComment } from './helpers';

async function addReply(request: APIRequestContext, mdPath: string, commentId: string, body: string, author: string) {
  const resp = await request.post(`/api/comment/${commentId}/replies?path=${encodeURIComponent(mdPath)}`, {
    data: { body, author },
  });
  expect(resp.status()).toBe(201);
  return resp.json();
}

// Load and switch plan.md to Document view (where these threads render).
async function openDocument(page: Page) {
  await loadPage(page);
  return switchToDocumentView(page);
}

// ============================================================
// Comment Threading — Git Mode
// ============================================================
test.describe('Comment Threading', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('API replies render inline and in the comments panel', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Check this');
    const reply = await addReply(request, mdPath, comment.id, 'Done, fixed it', 'agent');
    expect(reply.id).toBeTruthy();
    await addReply(request, mdPath, comment.id, 'Thanks', 'reviewer');

    const section = await openDocument(page);
    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-reply')).toHaveCount(2);
    await expect(section.locator('.reply-body').first()).toContainText('Done, fixed it');

    await page.keyboard.press('Shift+C');
    const panelCard = page.locator('.panel-comment-block .comment-card').first();
    await expect(panelCard).toBeVisible();
    await expect(panelCard.locator('.comment-reply')).toHaveCount(2);
  });

  test('reply box expands on focus; Escape, Cancel and collapsing the card hide its buttons', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    const section = await openDocument(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Compact reply input; Cancel/Reply stay hidden until focus expands it.
    const replyInput = card.locator('.reply-input');
    await expect(replyInput).toBeVisible();
    await expect(card.locator('.reply-form-buttons')).toBeHidden();
    await replyInput.click();
    await expect(card.locator('.reply-textarea')).toBeFocused();
    await expect(card.locator('.reply-form-buttons .btn-primary')).toBeVisible();

    // Escape collapses back to the compact input and hides the buttons again.
    await card.locator('.reply-textarea').press('Escape');
    await expect(card.locator('.reply-input')).toBeVisible();
    await expect(card.locator('.reply-textarea')).toHaveCount(0);
    await expect(card.locator('.reply-form-buttons')).toBeHidden();

    // Collapsing the card hides the buttons, and re-expanding the card does
    // not bring them back until the reply box is focused again.
    await card.locator('.reply-input').click();
    await expect(card.locator('.reply-form-buttons .btn-primary')).toBeVisible();
    await card.locator('.comment-collapse-btn').click();
    await expect(card).toHaveClass(/collapsed/);
    await expect(card.locator('.reply-form-buttons')).toBeHidden();
    await card.locator('.comment-collapse-btn').click();
    await expect(card.locator('.reply-form-buttons')).toBeHidden();

    // Cancel collapses without submitting.
    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('draft text');
    await card.locator('.reply-form-buttons .btn:not(.btn-primary)').click();
    await expect(card.locator('.reply-input')).toBeVisible();
    await expect(section.locator('.comment-reply')).toHaveCount(0);
  });

  test('Reply button and Ctrl+Enter add replies, then the box collapses and clears', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Review this');
    const section = await openDocument(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Addressed this');
    await card.locator('.reply-form-buttons .btn-primary').click();

    await expect(section.locator('.comment-reply')).toHaveCount(1);
    await expect(section.locator('.reply-body')).toContainText('Addressed this');
    // The form collapses back to the compact input, NOT re-populated.
    await expect(card.locator('.reply-input')).toBeVisible();
    await expect(card.locator('.reply-form.expanded')).toHaveCount(0);
    await expect(card.locator('.reply-textarea')).toHaveCount(0);
    await expect(card.locator('.reply-input')).toHaveValue('');

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Fixed via Ctrl+Enter');
    await card.locator('.reply-textarea').press('Control+Enter');
    await expect(section.locator('.comment-reply')).toHaveCount(2);
    await expect(section.locator('.reply-body').nth(1)).toContainText('Fixed via Ctrl+Enter');
  });

  test('opening a reply box closes other empty ones but keeps ones with text', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 2, 'Second comment');
    const section = await openDocument(page);
    const firstCard = section.locator('.comment-card').filter({ hasText: 'First comment' });
    const secondCard = section.locator('.comment-card').filter({ hasText: 'Second comment' });
    await expect(firstCard).toBeVisible();
    await expect(secondCard).toBeVisible();

    // Expand first reply form (leave empty), then the second: the first collapses.
    await firstCard.locator('.reply-input').click();
    await expect(firstCard.locator('.reply-form.expanded')).toHaveCount(1);
    await secondCard.locator('.reply-input').click();
    await expect(secondCard.locator('.reply-form.expanded')).toHaveCount(1);
    await expect(firstCard.locator('.reply-form.expanded')).toHaveCount(0);
    await expect(firstCard.locator('.reply-input')).toBeVisible();

    // With text in the second, opening the first keeps both expanded.
    await secondCard.locator('.reply-textarea').fill('draft reply');
    await firstCard.locator('.reply-input').click();
    await expect(firstCard.locator('.reply-form.expanded')).toHaveCount(1);
    await expect(secondCard.locator('.reply-form.expanded')).toHaveCount(1);
    await expect(secondCard.locator('.reply-textarea')).toHaveValue('draft reply');
  });

  test('a reply can be edited with Ctrl+Enter and deleted', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this');
    await addReply(request, mdPath, comment.id, 'Original reply', 'reviewer');
    const section = await openDocument(page);
    const reply = section.locator('.comment-reply').first();
    await expect(reply).toBeVisible();

    // Hover to reveal actions, click Edit
    await reply.hover();
    await reply.locator('.reply-actions button[title="Edit"]').click();
    const textarea = reply.locator('textarea');
    await expect(textarea).toHaveValue('Original reply');
    await textarea.fill('Edited reply via Ctrl+Enter');
    await textarea.press('Control+Enter');
    await expect(section.locator('.reply-body')).toContainText('Edited reply via Ctrl+Enter');
    await expect(reply.locator('textarea')).toHaveCount(0);

    await section.locator('.comment-reply').hover();
    await section.locator('.comment-reply .delete-btn').click();
    await expect(section.locator('.comment-reply')).toHaveCount(0);
  });

  // Expanding used to push Reply off screen with nothing scrolling it back.
  test('expanding a reply near the bottom edge keeps Reply on screen', async ({ page, request }) => {
    // Pinned: the bug is geometric, so don't depend on the default window size.
    await page.setViewportSize({ width: 1200, height: 500 });
    const mdPath = await getMdPath(request);
    // Mid-document, so there is page above it to scroll the card down to the edge.
    await addComment(request, mdPath, 24, 'Reply to me');
    const section = await openDocument(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Park the collapsed reply input just above the bottom edge. Instant
    // scroll (html is scroll-behavior: smooth), re-parked until it holds —
    // CodeView may still be settling item heights right after load.
    // locator.click() and boundingBox() scroll the target into view first,
    // which would move the page out of the state under test, so measure
    // in-page.
    let point = { x: 0, y: 0, edge: 0 };
    await expect(async () => {
      point = await page.evaluate(async () => {
        const el = document.querySelector('.comment-card .reply-input');
        if (!el) throw new Error('no reply input');
        // Git mode scrolls #filesContainer (CodeView's scroll root), not the window.
        const scroller = document.getElementById('filesContainer')!;
        const edge = Math.min(window.innerHeight, scroller.getBoundingClientRect().bottom);
        const top = el.getBoundingClientRect().top;
        scroller.scrollBy({ top: top - (edge - 60), behavior: 'instant' });
        await new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)));
        const r = el.getBoundingClientRect();
        return { x: r.x + r.width / 2, y: r.y + r.height / 2, edge };
      });
      expect(point.y).toBeGreaterThan(point.edge - 100);
      expect(point.y).toBeLessThan(point.edge);
    }).toPass({ timeout: 5000 });
    // Pierre turns pointer events off briefly after a scroll: wait until a
    // hit test at the input's center lands on the input itself.
    await expect.poll(() => page.evaluate(({ x, y }) => {
      const hit = document.elementFromPoint(x, y);
      return !!hit && !!hit.closest('.reply-input');
    }, point)).toBe(true);
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
        const scroller = document.getElementById('filesContainer')!;
        const edge = Math.min(window.innerHeight, scroller.getBoundingClientRect().bottom);
        return { bottom: Math.round(r.bottom), viewportHeight: Math.round(edge) };
      });
      expect(geom.bottom).toBeLessThanOrEqual(geom.viewportHeight);
    }).toPass({ timeout: 5000 });
  });

  // Closing an empty form re-renders the file, which used to detach the very
  // reply form being expanded — the box stayed one line tall.
  test('reply box expands with an empty comment form open, and keeps its text when another form opens', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Reply to me');
    const section = await openDocument(page);
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();

    // Open a second, empty comment form on the same file via the gutter.
    await section.locator('.line-block').nth(2).hover();
    await section.locator('.line-comment-gutter').nth(2).click();
    await expect(section.locator('.comment-form')).toHaveCount(1);

    // Now expand the thread's reply box.
    await card.locator('.reply-input').click();
    await expect(card.locator('.reply-form.expanded')).toHaveCount(1);
    await expect(card.locator('.reply-textarea')).toBeFocused();
    await expect(card.locator('.reply-form-buttons .btn-primary')).toBeVisible();

    // Type a reply, then open a comment form on another line of the SAME
    // file (re-renders it): the reply box survives with its text.
    await card.locator('.reply-textarea').fill('draft reply text');
    await section.locator('.line-block').nth(4).hover();
    await section.locator('.line-comment-gutter').nth(4).click();
    await expect(section.locator('.comment-form').last()).toBeVisible();
    await expect(card.locator('.reply-form.expanded')).toBeVisible();
    await expect(card.locator('.reply-textarea')).toHaveValue('draft reply text');
    await expect(card.locator('.reply-form-buttons')).toBeVisible();
  });

  test('Resolve collapses a comment, Unresolve restores it, and the chevron toggles its body', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Fix this bug');
    const section = await openDocument(page);
    const card = section.locator('.comment-card');
    await expect(card.locator('.comment-body')).toBeVisible();

    // Resolve: shows Unresolve and collapses.
    await card.hover();
    await card.locator('.comment-actions button[title="Resolve"]').click();
    await expect(section.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();
    await expect(section.locator('.comment-card.collapsed')).toHaveCount(1);

    // Expand, then Unresolve: back to an active, expanded comment.
    await section.locator('.comment-collapse-btn').click();
    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Unresolve"]').click();
    await expect(section.locator('.comment-actions button[title="Unresolve"]')).toHaveCount(0);
    await expect(section.locator('.comment-card:not(.collapsed)')).toHaveCount(1);

    // Collapse chevron toggles the body.
    await card.locator('.comment-collapse-btn').click();
    await expect(card).toHaveClass(/collapsed/);
    await expect(card.locator('.comment-body')).not.toBeVisible();
    await card.locator('.comment-collapse-btn').click();
    await expect(card).not.toHaveClass(/collapsed/);
    await expect(card.locator('.comment-body')).toBeVisible();
  });

  test('resolved comment renders as full card with badge', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Needs work');
    await addReply(request, mdPath, comment.id, 'Fixed it', 'agent');
    await resolveComment(request, mdPath, comment.id);

    const section = await openDocument(page);
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

    const section = await openDocument(page);
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
});
