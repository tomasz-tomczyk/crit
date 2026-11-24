import { test, expect, type Locator, type Page } from '@playwright/test';
import { addComment, clearAllComments, loadPage } from './helpers';

// Links to comments (#1218): the page URL with the comment id as the
// fragment. Opening one scrolls to the card, the card's link button copies
// it, and a markdown link to one inside a comment body jumps in-page.

function card(page: Page, id: string): Locator {
  return page.locator(`#filesContainer .comment-card[data-comment-id="${id}"]`);
}

// A fresh page load at /#<id>. Going to /#<id> from an open review only
// changes the hash (a hashchange, not a load), so leave the page first.
// loadPage before this sets the settings cookie the fixture needs.
async function openLink(page: Page, id: string) {
  await page.goto('about:blank');
  await page.goto('/#' + id);
}

test.describe('Comment links', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('opening a comment link scrolls to its card', async ({ page, request }) => {
    // server.go is below the fold on load.
    const target = await addComment(request, 'server.go', 1, 'Linked comment');

    await loadPage(page);
    await openLink(page, target.id);
    await expect(card(page, target.id)).toBeInViewport();
  });

  test('a later hash change to a comment id scrolls to it', async ({ page, request }) => {
    const target = await addComment(request, 'server.go', 1, 'Linked comment');
    await loadPage(page);
    await page.evaluate((id) => { window.location.hash = '#' + id; }, target.id);
    await expect(card(page, target.id)).toBeInViewport();
  });

  test('the link button copies the comment URL', async ({ page, context, request }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    const comment = await addComment(request, 'config.yaml', 1, 'Copy my link');
    await loadPage(page);

    const btn = card(page, comment.id).locator('.comment-link-btn');
    await expect(btn).toHaveAttribute('aria-label', 'Copy link to comment');
    await btn.click();

    await expect(btn).toHaveClass(/comment-link-copied/);
    await expect(page).toHaveURL(new RegExp('#' + comment.id + '$'));
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText()))
      .toBe(page.url());
  });

  test('a markdown link to a comment jumps to it in-page', async ({ page, request }) => {
    // config.yaml is the first file, server.go is below the fold: start at
    // the linking comment so the jump has to scroll back up.
    const target = await addComment(request, 'config.yaml', 1, 'The main explanation');
    const linker = await addComment(request, 'server.go', 1, `Same problem as [the first one](#${target.id}).`);
    await loadPage(page);
    await openLink(page, linker.id);
    await expect(card(page, linker.id)).toBeInViewport();
    await expect(card(page, target.id)).not.toBeInViewport();

    const link = card(page, linker.id).locator('.comment-body a');
    await expect(link).toHaveAttribute('href', '#' + target.id);
    await link.click();
    await expect(card(page, target.id)).toBeInViewport();
    await expect(page).toHaveURL(new RegExp('#' + target.id + '$'));
  });
});
