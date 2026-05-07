import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// Regression: in unified diff, a single-line comment on a new-side context line
// below a deletion block must NOT highlight deletion lines further up just
// because their old-side line numbers happen to share a value with the
// commented context line's new-side number.
//
// Fixture (legacy.go):
//   - Initial: package + 4 header comments + blank + func Old1..Old12 (lines 8-19) + blank + func Keep1..Keep4 (lines 21-24)
//   - Modified: func Old1..Old12 replaced with func New1..New4 (new lines 8-11)
//   - Resulting hunk: del lines (oldNum 8-19) + add lines (newNum 8-11) + context blank (newNum 12) + context func Keep1 (newNum 13)
//   - oldNum 13 is `func Old6() {}` deep in the deletion; newNum 13 is `func Keep1() {}` below the additions.
test.describe('Unified diff — comment range scoping', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('single-line comment on new-side context line below a deletion block does not highlight the deletion', async ({ page, request }) => {
    // Sanity-check the diff shape produced by the fixture.
    const diffRes = await request.get('/api/file/diff?path=legacy.go');
    const diffData = await diffRes.json();
    const hunks = diffData.hunks || [];
    expect(hunks.length).toBeGreaterThan(0);

    const allLines = hunks.flatMap((h: { Lines: { Type: string; OldNum: number; NewNum: number }[] }) => h.Lines);
    // Confirm the collision exists: a del line with OldNum===13 AND a context line with NewNum===13.
    const delAt13 = allLines.find((l: { Type: string; OldNum: number }) => l.Type === 'del' && l.OldNum === 13);
    const contextAt13 = allLines.find((l: { Type: string; NewNum: number }) => l.Type === 'context' && l.NewNum === 13);
    expect(delAt13).toBeTruthy();
    expect(contextAt13).toBeTruthy();

    // Comment on new-side line 13 (the context line `func Keep1() {}`).
    const res = await request.post('/api/file/comments?path=legacy.go', {
      data: { start_line: 13, end_line: 13, body: 'comment on Keep1' },
    });
    expect(res.ok()).toBeTruthy();

    await loadPage(page);

    const section = page.locator('#file-section-legacy\\.go');
    await expect(section).toBeVisible();

    // Switch to unified mode (toggle is in the page header, not per-file).
    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await expect(unifiedBtn).toBeVisible();
    await unifiedBtn.click();

    // Exactly one line should carry has-comment, and it must be a context line (not a deletion).
    const highlighted = section.locator('.diff-line.has-comment');
    await expect(highlighted).toHaveCount(1);

    // The deletion block must not be highlighted.
    await expect(section.locator('.diff-line.deletion.has-comment')).toHaveCount(0);
    // The addition block must not be highlighted either.
    await expect(section.locator('.diff-line.addition.has-comment')).toHaveCount(0);
  });
});
