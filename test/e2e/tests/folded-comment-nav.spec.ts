import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

function serverSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

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

    const section = serverSection(page);
    await expect(section).toBeVisible();

    // The comment should NOT be in the outdated section
    await expect(section.locator('.outdated-diff-comments .comment-card')).toHaveCount(0);

    // The comment should be rendered inline at its correct line position
    const inlineCard = section.locator('.comment-card').filter({ hasText: 'Comment on folded line' });
    await expect(inlineCard).toBeVisible({ timeout: 5000 });

    // The spacer that contained this line should have been expanded
    // (fewer spacers than before, or the comment is between diff lines not in outdated)
    await expect(inlineCard.locator('.outdated-badge')).toHaveCount(0);
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
    const section = serverSection(page);
    const inlineCard = section.locator(`.comment-card[data-comment-id="${comment.id}"]`);
    await expect(inlineCard).toBeVisible();
    await expect(inlineCard).toHaveClass(/comment-card-highlight/);

    // Must NOT be in the outdated section
    await expect(section.locator('.outdated-diff-comments .comment-card')).toHaveCount(0);
  });
});
