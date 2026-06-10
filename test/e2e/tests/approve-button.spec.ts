import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath } from './helpers';

// ============================================================
// Approve Button — Finish/Approve text reacts to resolve state
// Regression test for issue #156: unresolving a comment did not
// switch the button from "Approve" back to "Finish Review".
// ============================================================
test.describe('Approve Button Text', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('shows Finish Review when there is an unresolved comment', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Needs work');

    await loadPage(page);
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
  });

  test('shows Approve when all comments are resolved', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Needs work');

    // Resolve via API
    await request.fetch(`/api/comment/${comment.id}/resolve?path=${encodeURIComponent(mdPath)}`, {
      method: 'PUT',
      data: { resolved: true },
    });

    await loadPage(page);
    await expect(page.locator('#finishBtn')).toHaveText('Approve');
  });

  test('switches from Approve back to Finish Review when comment is unresolved', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Fix this bug');

    // Resolve via API so we start with "Approve"
    await request.fetch(`/api/comment/${comment.id}/resolve?path=${encodeURIComponent(mdPath)}`, {
      method: 'PUT',
      data: { resolved: true },
    });

    await loadPage(page);
    await expect(page.locator('#finishBtn')).toHaveText('Approve');

    // Switch to document view to access comment card
    await switchToDocumentView(page);
    const section = mdSection(page);

    // Expand the resolved (collapsed) card
    await section.locator('.comment-collapse-btn').click();

    // Hover to reveal unresolve button, click it
    await section.locator('.comment-card').hover();
    await section.locator('.comment-actions button[title="Unresolve"]').click();

    // Button should switch back to "Finish Review"
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
  });

  test('handles multiple comments: Approve only when all resolved', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 3, 'Second comment');

    await loadPage(page);

    // Two unresolved comments — should say "Finish Review"
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');

    // Switch to document view to interact with comment cards
    await switchToDocumentView(page);
    const section = mdSection(page);
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
