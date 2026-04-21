import { test, expect, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage, goSection, addComment } from './helpers';

// Get the server.go file path from the session
async function getServerGoPath(request: APIRequestContext): Promise<string> {
  const session = await (await request.get('/api/session')).json();
  const goFile = session.files.find((f: { path: string }) => f.path === 'server.go');
  expect(goFile).toBeTruthy();
  return goFile.path;
}

// Collect all line:side keys present in a file's diff hunks
async function getRenderedDiffKeys(request: APIRequestContext, filePath: string): Promise<Set<string>> {
  const diffResp = await request.get(`/api/file/diff?path=${encodeURIComponent(filePath)}`);
  const diffData = await diffResp.json();
  const hunks = diffData.hunks || [];
  const keys = new Set<string>();
  for (const hunk of hunks) {
    for (const line of hunk.Lines) {
      if (line.Type === 'del' && line.OldNum) keys.add(line.OldNum + ':old');
      if (line.Type === 'add' && line.NewNum) keys.add(line.NewNum + ':');
      if (line.Type === 'context') {
        if (line.OldNum) keys.add(line.OldNum + ':old');
        if (line.NewNum) keys.add(line.NewNum + ':');
      }
    }
  }
  return keys;
}

// Find a new-side line number that is NOT in any diff hunk for the file
async function findNonHunkLine(request: APIRequestContext, filePath: string): Promise<number> {
  const keys = await getRenderedDiffKeys(request, filePath);
  expect(keys.size).toBeGreaterThan(0); // Guard: diff must have real hunks
  // Pick a line number that's not in any hunk. Start from 1000.
  for (let n = 1000; n < 2000; n++) {
    if (!keys.has(n + ':')) return n;
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

  test('comment on non-hunk line renders with Outdated badge', async ({ page, request }) => {
    const filePath = await getServerGoPath(request);

    // Create a comment at a line number that doesn't appear in any diff hunk.
    // This simulates what happens when a comment was on a line that existed in
    // a previous round's diff but the agent removed that change.
    const nonHunkLine = await findNonHunkLine(request, filePath);
    await addComment(request, filePath, nonHunkLine, 'This line was removed from the diff');

    await loadPage(page);
    const section = goSection(page);
    await expect(section).toBeVisible();

    // The comment should appear with an Outdated badge
    const outdatedBadge = section.locator('.outdated-badge');
    await expect(outdatedBadge).toBeVisible({ timeout: 5_000 });
    await expect(outdatedBadge).toHaveText('Outdated');

    // The comment body should be readable
    await expect(section.locator('.comment-body')).toContainText('This line was removed from the diff');
  });

  test('outdated diff comment is resolvable', async ({ page, request }) => {
    const filePath = await getServerGoPath(request);
    const nonHunkLine = await findNonHunkLine(request, filePath);

    const comment = await addComment(request, filePath, nonHunkLine, 'Resolve me after outdated');

    await loadPage(page);
    const section = goSection(page);

    // Verify outdated badge appears
    await expect(section.locator('.outdated-badge')).toBeVisible({ timeout: 5_000 });

    // Resolve the comment via API
    await request.put(`/api/comment/${comment.id}/resolve?path=${encodeURIComponent(filePath)}`, {
      data: { resolved: true },
    });

    // Reload and verify resolved state with outdated badge
    await loadPage(page);
    const sectionAfter = goSection(page);
    await expect(sectionAfter.locator('.outdated-badge')).toBeVisible();
    await expect(sectionAfter.locator('.comment-card.resolved-card')).toBeVisible();
  });

  test('outdated comment shows in All Comments panel', async ({ page, request }) => {
    const filePath = await getServerGoPath(request);
    const nonHunkLine = await findNonHunkLine(request, filePath);

    await addComment(request, filePath, nonHunkLine, 'Panel outdated check');

    await loadPage(page);

    // Open comments panel
    await page.locator('#commentCount').click();
    await expect(page.locator('#commentsPanel')).toBeVisible();

    // Comment should appear in the panel
    await expect(page.locator('#commentsPanel')).toContainText('Panel outdated check');
  });

  test('normal diff comment does NOT get Outdated badge', async ({ page, request }) => {
    const filePath = await getServerGoPath(request);

    // Find a line that IS in a diff hunk (addition line)
    const diffResp = await request.get(`/api/file/diff?path=${encodeURIComponent(filePath)}`);
    const diffData = await diffResp.json();
    const hunks = diffData.hunks || [];
    let inHunkLine = 0;
    for (const hunk of hunks) {
      for (const line of hunk.Lines) {
        if (line.Type === 'add' && line.NewNum) {
          inHunkLine = line.NewNum;
          break;
        }
      }
      if (inHunkLine) break;
    }
    expect(inHunkLine).toBeGreaterThan(0);

    await addComment(request, filePath, inHunkLine, 'Normal diff comment');

    await loadPage(page);
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Comment should appear WITHOUT Outdated badge
    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-body')).toContainText('Normal diff comment');
    await expect(section.locator('.outdated-badge')).toHaveCount(0);
  });

  test('outdated comment has full CRUD (edit and delete)', async ({ page, request }) => {
    const filePath = await getServerGoPath(request);
    const nonHunkLine = await findNonHunkLine(request, filePath);

    const comment = await addComment(request, filePath, nonHunkLine, 'Editable outdated comment');

    await loadPage(page);
    const section = goSection(page);

    // Verify outdated badge appears
    await expect(section.locator('.outdated-badge')).toBeVisible({ timeout: 5_000 });
    await expect(section.locator('.comment-body')).toContainText('Editable outdated comment');

    // Edit the comment via API
    await request.put(`/api/comment/${comment.id}?path=${encodeURIComponent(filePath)}`, {
      data: { body: 'Edited outdated comment' },
    });

    await loadPage(page);
    await expect(goSection(page).locator('.comment-body')).toContainText('Edited outdated comment');
    await expect(goSection(page).locator('.outdated-badge')).toBeVisible();

    // Delete the comment via API
    await request.delete(`/api/comment/${comment.id}?path=${encodeURIComponent(filePath)}`);

    await loadPage(page);
    await expect(goSection(page).locator('.outdated-badge')).toHaveCount(0);
    await expect(goSection(page).locator('.comment-card')).toHaveCount(0);
  });
});
