import { test, expect, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Review-Level (General) Comments — Git Mode
// ============================================================
//
// As of the Review Conversation refactor: the compose/edit form lives in the
// inline `#reviewConversation` section at the top of the document, not in the
// side panel. Cards still mirror in the panel for navigation; clicking a panel
// card scrolls to the inline section.

async function seedReviewComment(request: APIRequestContext, body: string) {
  await expect(await request.post('/api/comments', { data: { body } })).toBeOK();
}

test.describe('Review-level comments — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test.describe('starting empty', () => {
    test.beforeEach(async ({ page }) => {
      await loadPage(page);
    });

    test('G opens the inline form; Escape closes it, and Ctrl+Enter submits without re-opening it', async ({ page }) => {
      await page.keyboard.press('Shift+G');
      const textarea = page.locator('#reviewConversation .comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(textarea).toBeFocused();

      await textarea.press('Escape');
      await expect(page.locator('#reviewConversation .comment-form')).toHaveCount(0);

      await page.keyboard.press('Shift+G');
      await textarea.fill('submitted with ctrl+enter');
      await textarea.press('Control+Enter');

      const cards = page.locator('#reviewConversation .comment-card');
      await expect(cards).toHaveCount(1);
      await expect(cards.first()).toContainText('submitted with ctrl+enter');
      // The form must not re-render itself with the just-submitted text.
      await expect(page.locator('#reviewConversation .comment-form')).toHaveCount(0);
      await expect(page.locator('.review-conversation-add-more')).toBeVisible();
    });

    test('empty state composer opens the form and the Submit button adds a comment', async ({ page }) => {
      const empty = page.locator('.review-conversation-empty');
      await expect(empty).toBeVisible();
      await empty.click();

      const textarea = page.locator('#reviewConversation .comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(textarea).toBeFocused();
      await textarea.fill('General feedback');
      await page.locator('#reviewConversation .comment-form .btn-primary').click();

      const cards = page.locator('#reviewConversation .comment-card');
      await expect(cards).toHaveCount(1);
      await expect(cards.first()).toContainText('General feedback');
    });
  });

  test.describe('with a seeded review comment', () => {
    test('renders on load, counts in the header, and shows in the tree conversation row', async ({ page, request }) => {
      await seedReviewComment(request, 'api review comment');
      await loadPage(page);

      await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(1);
      await expect(page.locator('#reviewConversation .comment-card').first()).toContainText('api review comment');
      await expect(page.locator('#commentCountNumber')).toHaveText('1');

      const row = page.locator('.tree-conversation-row');
      await expect(row).toBeVisible();
      await expect(row).toContainText('Review conversation');
      await expect(row.locator('.tree-conversation-badge')).toHaveText('1');
    });

    test('can edit, reply to, and delete a review comment', async ({ page, request }) => {
      await seedReviewComment(request, 'original review');
      await loadPage(page);

      const card = page.locator('#reviewConversation .comment-card').first();
      await expect(card).toBeVisible();

      await card.locator('button[title="Edit"]').click();
      const textarea = page.locator('#reviewConversation .comment-form textarea');
      await expect(textarea).toHaveValue('original review');
      await textarea.fill('updated review');
      await page.locator('#reviewConversation .comment-form .btn-primary').click();
      await expect(page.locator('#reviewConversation .comment-card .comment-body')).toContainText('updated review');

      const replyInput = card.locator('.reply-form .reply-input');
      await expect(replyInput).toBeVisible();
      await replyInput.click();
      const replyTextarea = card.locator('.reply-form .reply-textarea');
      await expect(replyTextarea).toBeVisible();
      await replyTextarea.fill('a reply');
      await card.locator('.reply-form .btn-primary').click();
      await expect(card.locator('.reply-body')).toContainText('a reply');

      await card.locator('.delete-btn').first().click();
      await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(0);
    });

    test('clicking a panel review-comment card navigates to and flashes the inline card', async ({ page, request }) => {
      await seedReviewComment(request, 'panel navigation target');
      await loadPage(page);

      await page.keyboard.press('Shift+C');
      await expect(page.locator('#commentsPanel')).not.toHaveClass(/comments-panel-hidden/);

      const panelCard = page.locator('#commentsPanelBody .comment-card').first();
      await expect(panelCard).toBeVisible();
      await panelCard.click();

      // Tree row should become active and the inline card should briefly highlight
      await expect(page.locator('.tree-conversation-row')).toHaveClass(/active/);
      await expect(page.locator('#reviewConversation .comment-card.comment-card-highlight')).toBeVisible();
    });

    test('section can be collapsed and expanded', async ({ page, request }) => {
      await seedReviewComment(request, 'a thread');
      await loadPage(page);

      const section = page.locator('#reviewConversation');
      const toggle = section.locator('.review-conversation-toggle');

      // Initially expanded — the thread is visible
      await expect(section.locator('.comment-card')).toBeVisible();

      await toggle.click();
      await expect(section).toHaveClass(/collapsed/);
      await expect(section.locator('.comment-card')).toBeHidden();

      await toggle.click();
      await expect(section).not.toHaveClass(/collapsed/);
      await expect(section.locator('.comment-card')).toBeVisible();
    });
  });
});
