import { test, expect, type APIRequestContext } from '@playwright/test';
import {
  clearAllComments, loadPage, revealFile, diffLine, reviewScroller, waitForScrollStable,
  recordClassAdditions, expectClassAddedTo,
} from './helpers';

// A line inside a gap Pierre folds behind a separator. Gaps of 8 lines or
// fewer are merged into the hunks before rendering (auto-expand-gaps.spec.ts)
// and never fold, so only a larger gap exercises this path: routes.go has a
// 37-line gap (new 15..51) between its two hunks.
async function findFoldedLine(request: APIRequestContext, path: string): Promise<number> {
  const diffData = await (await request.get(`/api/file/diff?path=${encodeURIComponent(path)}`)).json();
  const hunks = Array.isArray(diffData) ? diffData : (diffData.hunks || []);
  for (let i = 0; i < hunks.length - 1; i++) {
    const prevEnd = hunks[i].NewStart + hunks[i].NewCount;
    const gap = hunks[i + 1].NewStart - prevEnd;
    if (gap > 8) return prevEnd + Math.floor(gap / 2);
  }
  throw new Error(`No folded gap (> 8 lines) in ${path}`);
}

// Issue #317: comments on lines inside spacer gaps (folded unchanged lines)
// should auto-expand the spacer so the comment appears at its correct position,
// not as an "outdated" comment at the bottom of the diff.
test.describe('Comments in folded code (#317)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('comment on a folded line expands the fold, renders inline, and the panel navigates to it', async ({ page, request }) => {
    const line = await findFoldedLine(request, 'routes.go');

    const resp = await request.post('/api/file/comments?path=routes.go', {
      data: { start_line: line, end_line: line, body: 'Comment on folded line' },
    });
    expect(resp.ok()).toBeTruthy();
    const comment = await resp.json();

    await loadPage(page);
    const item = await revealFile(page, 'routes.go');

    // The comment is NOT in the outdated block
    await expect(item.locator('.outdated-diff-comments')).toHaveCount(0);

    // It renders inline at its line, without an outdated badge.
    const inlineCard = item.locator(`.comment-card[data-comment-id="${comment.id}"]`);
    await expect(inlineCard).toBeAttached();
    await inlineCard.scrollIntoViewIfNeeded();
    await expect(inlineCard).toBeVisible();
    await expect(inlineCard.locator('.outdated-badge')).toHaveCount(0);

    // The fold that held the line was expanded: the line itself renders
    // as a diff row, directly above its comment.
    const row = diffLine(item, line).first();
    await expect(row).toBeVisible();
    await expect.poll(async () => {
      const rowBox = await row.boundingBox();
      const cardBox = await inlineCard.boundingBox();
      return !!rowBox && !!cardBox && rowBox.y + rowBox.height <= cardBox.y + 1;
    }).toBe(true);

    // From the top of the review, the panel card navigates back to it.
    await reviewScroller(page).evaluate(el => { el.scrollTop = 0; });
    await waitForScrollStable(page);
    await expect(inlineCard).not.toBeInViewport();
    await recordClassAdditions(page, 'comment-card-highlight');

    await page.keyboard.press('Shift+C');
    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    const panelCards = panel.locator('.panel-comment-block .comment-card');
    await expect(panelCards).toHaveCount(1);

    await expectClassAddedTo(page, 'comment-card-highlight', () => panelCards.first().click(), comment.id);
    await expect(inlineCard).toBeInViewport();
    await expect(diffLine(item, line).first()).toBeInViewport();
    await expect(item.locator('.outdated-diff-comments')).toHaveCount(0);
  });
});
