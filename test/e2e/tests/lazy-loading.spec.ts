import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments, revealFile } from './helpers';

// Under-threshold sessions mark no file lazy: internal/session
// TestNewSessionFromGitUnderThreshold (and TestNewSessionFromGitLazyThreshold
// for the over-threshold split).
test.describe('Lazy loading', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('all files render fully when under threshold', async ({ page, request }) => {
    const session = await (await request.get('/api/session')).json();
    // Git-mode fixture has <25 files — none should be lazy.
    expect(session.files.length).toBeLessThan(25);
    expect(session.files.filter((f: { lazy?: boolean }) => f.lazy)).toEqual([]);
    // Files with changed lines (the fixture's deleted.txt is an empty file).
    const paths: string[] = session.files
      .filter((f: { additions: number; deletions: number }) => f.additions + f.deletions > 0)
      .map((f: { path: string }) => f.path);
    expect(paths.length).toBeGreaterThan(5);
    await loadPage(page);

    // Every file shows its real diff — no line-count placeholder of blank
    // rows waiting to hydrate. Visit each (CodeView mounts nearby files only;
    // the tree jump also expands files that start collapsed).
    for (const p of paths) {
      const item = await revealFile(page, p);
      await expect(item.locator('[data-line]').filter({ hasText: /\S/ }).first()).toBeVisible();
    }
  });
});
