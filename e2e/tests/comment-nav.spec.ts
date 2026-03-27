import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, addComment, switchToDocumentView, clearFocus } from './helpers';

// ============================================================
// Comment Navigation — ] / [ shortcuts and prev/next buttons
// ============================================================

test.describe('Comment Navigation — nav group visibility', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('nav group gains has-comments class when comments exist', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'Navigation test comment');
    await loadPage(page);
    await switchToDocumentView(page);

    await expect(page.locator('#commentNavGroup')).toHaveClass(/has-comments/);
  });

  test('nav group does not have has-comments class with no comments', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);

    await expect(page.locator('#commentNavGroup')).not.toHaveClass(/has-comments/);
  });

  test('prev and next buttons are visible when comments exist', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'Navigation test comment');
    await loadPage(page);
    await switchToDocumentView(page);

    await expect(page.locator('#commentNavPrev')).toBeVisible();
    await expect(page.locator('#commentNavNext')).toBeVisible();
  });
});

test.describe('Comment Navigation — ] forward shortcut', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('] highlights first comment card on initial press', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    await page.keyboard.press(']');

    await expect(page.locator('.comment-card').first()).toHaveClass(/comment-nav-highlight/);
  });

  test('] advances to second comment after first', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await addComment(request, 'plan.md', 5, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    // First press — land on first card
    await page.keyboard.press(']');
    await expect(page.locator('.comment-card').first()).toHaveClass(/comment-nav-highlight/);

    // Second press — advance to second card
    await page.keyboard.press(']');
    await expect(page.locator('.comment-card').nth(1)).toHaveClass(/comment-nav-highlight/);
  });

  test('] wraps from last comment back to first', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await addComment(request, 'plan.md', 5, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    const cards = page.locator('.comment-card');

    // Navigate to last card
    await page.keyboard.press(']');
    await page.keyboard.press(']');
    await expect(cards.last()).toHaveClass(/comment-nav-highlight/);

    // One more press wraps to first
    await page.keyboard.press(']');
    await expect(cards.first()).toHaveClass(/comment-nav-highlight/);
  });
});

test.describe('Comment Navigation — [ backward shortcut', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('[ jumps to last comment on initial press', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await addComment(request, 'plan.md', 5, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    await page.keyboard.press('[');

    await expect(page.locator('.comment-card').last()).toHaveClass(/comment-nav-highlight/);
  });

  test('[ goes to previous comment when already navigated', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await addComment(request, 'plan.md', 5, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    const cards = page.locator('.comment-card');

    // Land on second card via ]
    await page.keyboard.press(']');
    await page.keyboard.press(']');
    await expect(cards.nth(1)).toHaveClass(/comment-nav-highlight/);

    // [ should go back to first
    await page.keyboard.press('[');
    await expect(cards.first()).toHaveClass(/comment-nav-highlight/);
  });

  test('[ wraps from first comment to last', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await addComment(request, 'plan.md', 5, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    const cards = page.locator('.comment-card');

    // Land on first card via ]
    await page.keyboard.press(']');
    await expect(cards.first()).toHaveClass(/comment-nav-highlight/);

    // [ from first wraps to last
    await page.keyboard.press('[');
    await expect(cards.last()).toHaveClass(/comment-nav-highlight/);
  });
});

test.describe('Comment Navigation — prev/next buttons', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('next button highlights first comment on initial click', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await loadPage(page);
    await switchToDocumentView(page);

    await page.locator('#commentNavNext').click();

    await expect(page.locator('.comment-card').first()).toHaveClass(/comment-nav-highlight/);
  });

  test('prev button jumps to last comment on initial click', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await addComment(request, 'plan.md', 5, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);

    await page.locator('#commentNavPrev').click();

    await expect(page.locator('.comment-card').last()).toHaveClass(/comment-nav-highlight/);
  });

  test('next then prev returns to first comment', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'First comment');
    await addComment(request, 'plan.md', 5, 'Second comment');
    await loadPage(page);
    await switchToDocumentView(page);

    const cards = page.locator('.comment-card');

    // Go to first via next
    await page.locator('#commentNavNext').click();
    await expect(cards.first()).toHaveClass(/comment-nav-highlight/);

    // Prev from first wraps to last
    await page.locator('#commentNavPrev').click();
    await expect(cards.last()).toHaveClass(/comment-nav-highlight/);
  });
});

test.describe('Comment Navigation — disabled in textarea', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('] does not navigate when a comment form textarea is focused', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'Existing comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    // Open a new comment form on a line block
    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const lineBlock = section.locator('.line-block.kb-nav').first();
    await lineBlock.hover();
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    // ] typed in textarea should not navigate comments
    await textarea.pressSequentially(']');
    await expect(textarea).toHaveValue(']');
    await expect(page.locator('.comment-card.comment-nav-highlight')).toHaveCount(0);
  });

  test('[ does not navigate when a comment form textarea is focused', async ({ page, request }) => {
    await addComment(request, 'plan.md', 1, 'Existing comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    // Open a new comment form on a line block
    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const lineBlock = section.locator('.line-block.kb-nav').first();
    await lineBlock.hover();
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    // [ typed in textarea should not navigate comments
    await textarea.pressSequentially('[');
    await expect(textarea).toHaveValue('[');
    await expect(page.locator('.comment-card.comment-nav-highlight')).toHaveCount(0);
  });
});

test.describe('Comment Navigation — shortcuts overlay', () => {
  test('shortcuts overlay lists ] and [ navigation keys', async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);

    await page.keyboard.press('?');

    const overlay = page.locator('#shortcutsOverlay');
    await expect(overlay).toHaveClass(/active/);
    await expect(overlay).toContainText(']');
    await expect(overlay).toContainText('[');
  });
});
