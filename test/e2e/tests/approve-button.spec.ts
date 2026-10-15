import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath, resolveComment } from './helpers';

// ============================================================
// Approve Button — Finish/Approve text reacts to resolve state
// Regression test for issue #156: unresolving a comment did not
// switch the button from "Approve" back to "Finish Review".
// ============================================================
test.describe('Approve Button Text', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('shows Approve on load when all comments are resolved, and Finish Review after unresolving', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this bug');
    await resolveComment(request, mdPath, comment.id);

    await loadPage(page);
    await expect(page.locator('#finishBtn')).toHaveText('Approve');

    // Switch to document view to access comment card
    await switchToDocumentView(page);
    const section = await mdSection(page);

    // Expand the resolved (collapsed) card, then unresolve it
    await section.locator('.comment-collapse-btn').click();
    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Unresolve"]').click();

    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
  });

  test('Approve only when every comment is resolved', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 3, 'Second comment');

    await loadPage(page);

    // Unresolved comments — should say "Finish Review"
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');

    await switchToDocumentView(page);
    const section = await mdSection(page);
    const cards = section.locator('.comment-card');
    await expect(cards).toHaveCount(2);

    // Resolve the first comment via hover + click
    await cards.nth(0).hover();
    await cards.nth(0).locator('.comment-actions button[title="Resolve"]').click();

    // Wait for the resolve to take effect: c1 should become a resolved collapsed card
    await expect(section.locator('.comment-card.collapsed')).toHaveCount(1);

    // One unresolved remains — still "Finish Review"
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');

    // Resolve the second comment — now only one non-collapsed card remains
    const secondCard = section.locator('.comment-card:not(.collapsed)');
    await expect(secondCard).toHaveCount(1);
    await secondCard.hover();
    await secondCard.locator('.comment-actions button[title="Resolve"]').click();

    // All resolved — should say "Approve"
    await expect(page.locator('#finishBtn')).toHaveText('Approve');

    // Unresolve the first comment: expand it first, then unresolve
    const collapsedCards = section.locator('.comment-card.collapsed');
    await expect(collapsedCards).toHaveCount(2);
    await collapsedCards.nth(0).locator('.comment-collapse-btn').click();
    const expandedCard = section.locator('.comment-card:not(.collapsed)').first();
    await expandedCard.hover();
    await expandedCard.locator('.comment-actions button[title="Unresolve"]').click();

    // Back to "Finish Review"
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
  });
});

// ============================================================
// Approve Button - read only after the server stopped
// Once the daemon sends server-shutdown, nothing may re-enable finishing:
// clicking it would POST /api/finish to a stopped server.
// ============================================================
test.describe('Approve Button after server shutdown', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    // Stand in for a daemon that just stopped: the event stream delivers
    // server-shutdown, while the other endpoints keep answering.
    await page.route('**/api/events', (route) =>
      route.fulfill({
        status: 200,
        headers: { 'content-type': 'text/event-stream' },
        body: 'event: server-shutdown\ndata: {"type":"server-shutdown"}\n\n',
      }),
    );
  });

  test('shows a disabled Session complete button that stays locked when a comment changes', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this bug');
    await resolveComment(request, mdPath, comment.id);

    await loadPage(page);
    await expect(page.locator('.disconnected-banner')).toBeVisible();
    const finishBtn = page.locator('#finishBtn');
    await expect(finishBtn).toHaveText('Session complete');
    await expect(finishBtn).toBeDisabled();
    await expect(finishBtn).not.toHaveClass(/btn-primary/);

    // Unresolving recounts the comments, which relabels the finish button.
    await switchToDocumentView(page);
    const section = await mdSection(page);
    await section.locator('.comment-collapse-btn').click();
    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Unresolve"]').click();
    // The recount runs in the same refresh that re-renders the card.
    await expect(section.locator('.comment-actions button[title="Resolve"]')).toHaveCount(1);

    await expect(finishBtn).toHaveText('Session complete');
    await expect(finishBtn).toBeDisabled();
  });
});
