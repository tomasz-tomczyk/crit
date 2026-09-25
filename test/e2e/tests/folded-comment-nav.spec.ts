import { test, expect, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage, goSection, fileItem, diffLine } from './helpers';

// Find a new-side line number that falls inside a spacer gap between two
// diff hunks. Returns { line, gapSize } or null if no gap exists.
async function findSpacerGapLine(request: APIRequestContext): Promise<{ line: number; gapSize: number }> {
  const diffResp = await request.get('/api/file/diff?path=server.go');
  const diffData = await diffResp.json();
  const hunks = Array.isArray(diffData) ? diffData : (diffData.hunks || []);
  expect(hunks.length).toBeGreaterThan(1);

  for (let i = 0; i < hunks.length - 1; i++) {
    const prevEnd = hunks[i].NewStart + hunks[i].NewCount;
    const nextStart = hunks[i + 1].NewStart;
    const gap = nextStart - prevEnd;
    if (gap > 0) {
      // Pick the middle line of the gap
      const line = prevEnd + Math.floor(gap / 2);
      return { line, gapSize: gap };
    }
  }
  throw new Error('No spacer gap found in server.go diff');
}

// Issue #317: comments on lines inside spacer gaps (folded unchanged lines)
// should auto-expand the spacer so the comment appears at its correct position,
// not as an "outdated" comment at the bottom of the diff.
test.describe('Comments in folded code (#317)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('comment on folded line auto-expands spacer, not shown as outdated', async ({ page, request }) => {
    const { line } = await findSpacerGapLine(request);

    // Add comment on a line inside the spacer gap via API
    const resp = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: line, end_line: line, body: 'Comment on folded line' },
    });
    expect(resp.ok()).toBeTruthy();

    await loadPage(page);

    const item = await goSection(page);

    // The comment should NOT be in the outdated block
    await expect(item.locator('.outdated-diff-comments')).toHaveCount(0);

    // The comment should be rendered inline at its correct line position
    const inlineCard = item.locator('.comment-card').filter({ hasText: 'Comment on folded line' });
    await expect(inlineCard).toBeAttached();
    await inlineCard.scrollIntoViewIfNeeded();
    await expect(inlineCard).toBeVisible();
    await expect(inlineCard.locator('.outdated-badge')).toHaveCount(0);

    // The fold that held the line was expanded: the line itself renders
    // as a diff row, directly above its comment.
    const row = diffLine(item, line).first();
    await expect(row).toBeVisible();
    const rowBox = await row.boundingBox();
    const cardBox = await inlineCard.boundingBox();
    expect(rowBox!.y + rowBox!.height).toBeLessThanOrEqual(cardBox!.y + 1);
  });

  test('panel click scrolls to comment on formerly-folded line', async ({ page, request }) => {
    const { line } = await findSpacerGapLine(request);

    const resp = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: line, end_line: line, body: 'Panel nav folded' },
    });
    expect(resp.ok()).toBeTruthy();
    const comment = await resp.json();

    await loadPage(page);

    // Open the comments panel
    await page.keyboard.press('Shift+C');
    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);

    const panelCards = panel.locator('.panel-comment-block .comment-card');
    await expect(panelCards).toHaveCount(1);

    // Click the panel card to navigate
    await panelCards.first().click();

    // The inline comment card should be visible and highlighted at its correct position
    const item = fileItem(page, 'server.go');
    const inlineCard = item.locator(`.comment-card[data-comment-id="${comment.id}"]`);
    await expect(inlineCard).toBeInViewport();
    await expect(inlineCard).toHaveClass(/comment-card-highlight/);
    await expect(diffLine(item, line).first()).toBeInViewport();

    // Must NOT be in the outdated block
    await expect(item.locator('.outdated-diff-comments')).toHaveCount(0);
  });
});
