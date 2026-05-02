import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Review-Level (General) Comments — Git Mode
// ============================================================
//
// As of the Review Conversation refactor: the compose/edit form lives in the
// inline `#reviewConversation` section at the top of the document, not in the
// side panel. Cards still mirror in the panel for navigation; clicking a panel
// card scrolls to the inline section.
test.describe('Review-level comments — Git Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('G shortcut opens review comment form inline', async ({ page }) => {
    await page.keyboard.press('Shift+G');

    const form = page.locator('#reviewConversation .comment-form textarea');
    await expect(form).toBeVisible();
    await expect(form).toBeFocused();
  });

  test('can add a review-level comment via G shortcut', async ({ page }) => {
    await page.keyboard.press('Shift+G');
    await page.locator('#reviewConversation .comment-form textarea').fill('General feedback');
    await page.locator('#reviewConversation .comment-form .btn-primary').click();

    const cards = page.locator('#reviewConversation .comment-card');
    await expect(cards).toHaveCount(1);
    await expect(cards.first()).toContainText('General feedback');
  });

  test('review comments added via API render on load', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'api review comment' } });
    await loadPage(page);

    await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(1);
    await expect(page.locator('#reviewConversation .comment-card').first()).toContainText('api review comment');
  });

  test('can delete review comments', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'to delete' } });
    await loadPage(page);

    const card = page.locator('#reviewConversation .comment-card').first();
    await expect(card).toBeVisible();
    await card.locator('.delete-btn').click();

    await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(0);
  });

  test('empty state composer opens form', async ({ page }) => {
    const empty = page.locator('.review-conversation-empty');
    await expect(empty).toBeVisible();
    await empty.click();

    const form = page.locator('#reviewConversation .comment-form textarea');
    await expect(form).toBeVisible();
    await expect(form).toBeFocused();
  });

  test('Escape closes review comment form', async ({ page }) => {
    await page.keyboard.press('Shift+G');
    const textarea = page.locator('#reviewConversation .comment-form textarea');
    await expect(textarea).toBeVisible();

    await textarea.press('Escape');

    await expect(page.locator('#reviewConversation .comment-form')).toHaveCount(0);
  });

  test('Ctrl+Enter submits review comment', async ({ page }) => {
    await page.keyboard.press('Shift+G');
    const textarea = page.locator('#reviewConversation .comment-form textarea');
    await textarea.fill('submitted with ctrl+enter');
    await textarea.press('Control+Enter');

    await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(1);
    await expect(page.locator('#reviewConversation .comment-card').first()).toContainText('submitted with ctrl+enter');
  });

  test('review comments included in comment count', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'count test' } });
    await loadPage(page);

    const badge = page.locator('#commentCount');
    await expect(badge).toBeVisible();
  });

  test('can edit a review comment', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'original review' } });
    await loadPage(page);

    const card = page.locator('#reviewConversation .comment-card').first();
    await expect(card).toBeVisible();

    await card.locator('button[title="Edit"]').click();

    const textarea = page.locator('#reviewConversation .comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('original review');

    await textarea.clear();
    await textarea.fill('updated review');
    await page.locator('#reviewConversation .comment-form .btn-primary').click();

    await expect(page.locator('#reviewConversation .comment-card .comment-body')).toContainText('updated review');
  });

  test('file tree has Review conversation row with badge', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'tree row test' } });
    await loadPage(page);

    const row = page.locator('.tree-conversation-row');
    await expect(row).toBeVisible();
    await expect(row).toContainText('Review conversation');
    await expect(row.locator('.tree-conversation-badge')).toHaveText('1');
  });

  test('can reply to a review-level comment', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'parent thread' } });
    await loadPage(page);

    const card = page.locator('#reviewConversation .comment-card').first();
    const replyInput = card.locator('.reply-form .reply-input');
    await expect(replyInput).toBeVisible();
    await replyInput.click();

    const textarea = card.locator('.reply-form .reply-textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('a reply');
    await card.locator('.reply-form .btn-primary').click();

    await expect(card.locator('.reply-body')).toContainText('a reply');
  });

  test('clicking a panel review-comment card navigates to and flashes the inline card', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'panel navigation target' } });
    await loadPage(page);

    // Open the comments panel
    await page.keyboard.press('Shift+C');
    await expect(page.locator('#commentsPanel')).not.toHaveClass(/comments-panel-hidden/);

    const panelCard = page.locator('#commentsPanelBody .comment-card').first();
    await expect(panelCard).toBeVisible();
    await panelCard.click();

    // Tree row should become active and the inline card should briefly highlight
    await expect(page.locator('.tree-conversation-row')).toHaveClass(/active/);
    await expect(page.locator('#reviewConversation .comment-card.comment-card-highlight')).toBeVisible();
  });

  test('after submit, form does not re-open pre-populated with submitted text', async ({ page }) => {
    await page.keyboard.press('Shift+G');
    const textarea = page.locator('#reviewConversation .comment-form textarea');
    await textarea.fill('one-shot comment');
    await page.locator('#reviewConversation .comment-form .btn-primary').click();

    await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(1);
    // The form must not re-render itself with the just-submitted text.
    await expect(page.locator('#reviewConversation .comment-form')).toHaveCount(0);
    await expect(page.locator('.review-conversation-add-more')).toBeVisible();
  });

  test('section can be collapsed and expanded', async ({ page, request }) => {
    await request.post('/api/comments', { data: { body: 'a thread' } });
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
