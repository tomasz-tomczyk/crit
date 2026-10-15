import { test, expect, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage, goSection, addComment, resolveComment } from './helpers';

const FILE = 'server.go';

interface DiffLine { Type: string; OldNum: number; NewNum: number }

async function diffLines(request: APIRequestContext): Promise<DiffLine[]> {
  const diffData = await (await request.get(`/api/file/diff?path=${encodeURIComponent(FILE)}`)).json();
  const lines = (diffData.hunks || []).flatMap((h: { Lines: DiffLine[] }) => h.Lines);
  expect(lines.length).toBeGreaterThan(0); // Guard: diff must have real hunks
  return lines;
}

// A new-side line number that is NOT in any diff hunk for the file.
async function findNonHunkLine(request: APIRequestContext): Promise<number> {
  const newSide = new Set((await diffLines(request)).filter(l => l.Type !== 'del' && l.NewNum).map(l => l.NewNum));
  for (let n = 1000; n < 2000; n++) {
    if (!newSide.has(n)) return n;
  }
  throw new Error('Could not find a non-hunk line number');
}

// ============================================================
// Outdated Diff Comments — lines no longer in diff hunks
//
// When a comment's end_line:side key doesn't match any line rendered
// in the current diff hunks, the comment should still appear inline
// with an "Outdated" badge. This covers the scenario where an agent
// undoes a code change after the reviewer commented on it.
// ============================================================
test.describe('Outdated Diff Comments', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('a comment on a non-hunk line renders in the Outdated block, stays there when resolved, is listed in the panel, and can be edited and deleted', async ({ page, request }) => {
    // Simulates a comment on a line that existed in a previous round's diff
    // but whose change the agent removed.
    const nonHunkLine = await findNonHunkLine(request);
    const outdated = await addComment(request, FILE, nonHunkLine, 'This line was removed from the diff');
    const resolved = await addComment(request, FILE, nonHunkLine, 'Resolve me after outdated');
    await resolveComment(request, FILE, resolved.id);

    await loadPage(page);
    const section = await goSection(page);

    const outdatedBadge = section.locator('.outdated-badge');
    await expect(outdatedBadge.first()).toHaveText('Outdated');
    const block = section.locator('.outdated-diff-comments');
    await expect(block.locator('.comment-card .comment-body').first()).toContainText('This line was removed from the diff');
    // The resolved one keeps its resolved state inside the outdated block.
    await expect(block.locator('.comment-card.resolved-card')).toHaveCount(1);
    await expect(block.locator('.comment-card:not(.resolved-card)')).toHaveCount(1);

    await page.locator('#commentCount').click();
    await expect(page.locator('#commentsPanel')).toBeVisible();
    await expect(page.locator('#commentsPanel')).toContainText('This line was removed from the diff');

    // Outdated comments keep full CRUD: edit one and delete the other.
    await expect(await request.put(`/api/comment/${outdated.id}?path=${encodeURIComponent(FILE)}`, {
      data: { body: 'Edited outdated comment' },
    })).toBeOK();
    await expect(await request.delete(`/api/comment/${resolved.id}?path=${encodeURIComponent(FILE)}`)).toBeOK();
    await loadPage(page);
    const after = (await goSection(page)).locator('.outdated-diff-comments');
    await expect(after.locator('.comment-card')).toHaveCount(1);
    await expect(after.locator('.comment-body')).toContainText('Edited outdated comment');
    await expect(after.locator('.outdated-badge')).toBeVisible();
  });

  test('a normal diff comment does NOT get an Outdated badge', async ({ page, request }) => {
    const inHunkLine = (await diffLines(request)).find(l => l.Type === 'add' && l.NewNum)?.NewNum ?? 0;
    expect(inHunkLine).toBeGreaterThan(0);
    await addComment(request, FILE, inHunkLine, 'Normal diff comment');

    await loadPage(page);
    const section = await goSection(page);

    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-body')).toContainText('Normal diff comment');
    await expect(section.locator('.outdated-badge')).toHaveCount(0);
    await expect(section.locator('.outdated-diff-comments')).toHaveCount(0);
  });
});
