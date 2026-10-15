import { test, expect, type APIRequestContext } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, jsSection, diffLine,
  diffLineNumber, showLine, scanDiffRows, lineRange,
} from './helpers';

// server.go has three git hunks (new 2..11, 20..57, 64..71). The gaps between
// them are 8 lines (new 12..19 ↔ old 9..16) and 6 lines (new 58..63 ↔ old
// 33..38) — both ≤ 8, so Crit merges them into one hunk before handing the
// diff to Pierre. Without that merge Pierre would collapse each gap behind a
// separator.
//
// Also covered elsewhere: commenting on an auto-expanded line
// (expanded-comments.spec.ts) and a >8-line gap keeping its separator
// (incremental-expand.spec.ts, routes.go).
const SERVER_LAST_LINE = 71;

async function fileLines(request: APIRequestContext, path: string): Promise<string[]> {
  const res = await request.get(`/api/file?path=${encodeURIComponent(path)}`);
  expect(res.ok()).toBeTruthy();
  return ((await res.json()).content as string).split('\n');
}

// ============================================================
// Auto-expand small gaps (≤ 8 lines) between diff hunks
// ============================================================
test.describe('Auto-expand small gaps', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('small gaps are expanded: every hunk merges into one contiguous block', async ({ page }) => {
    const item = await goSection(page);
    // Every new-side line from 1 to EOF, in order, with no separator.
    expect(await scanDiffRows(page, item, SERVER_LAST_LINE)).toEqual(lineRange(1, SERVER_LAST_LINE));
    await expect(item.locator('[data-separator]')).toHaveCount(0);
  });

  test('auto-expanded context lines render with correct line numbers', async ({ page, request }) => {
    const content = await fileLines(request, 'server.go');
    const item = await goSection(page);

    // First line of each merged gap, paired with its old-side number.
    for (const [newNum, oldNum] of [[12, 9], [19, 16], [58, 33], [63, 38]]) {
      await showLine(page, diffLine(item, newNum));
      await expect(diffLine(item, newNum)).toHaveAttribute('data-line-type', /^context/);
      await expect(diffLineNumber(item, newNum)).toHaveText(String(newNum));
      await expect(diffLineNumber(item, oldNum, 'old')).toHaveText(String(oldNum));
      const expected = content[newNum - 1];
      await expect(diffLine(item, newNum)).toHaveText(expected.length ? expected : /^\s*$/);
      await expect(diffLine(item, oldNum, 'old')).toHaveText(expected.length ? expected : /^\s*$/);
      // Same visual row on both sides (re-measured until the rows settle).
      await expect.poll(async () => {
        const a = await diffLine(item, newNum).boundingBox();
        const b = await diffLine(item, oldNum, 'old').boundingBox();
        return !!a && !!b && Math.abs(a.y - b.y) < 2;
      }).toBe(true);
    }
  });

  test('handler.js (new file, single hunk) renders correctly', async ({ page }) => {
    const item = await jsSection(page);
    expect(await scanDiffRows(page, item, 19)).toEqual(lineRange(1, 19));
    await expect(diffLine(item, 1)).toHaveAttribute('data-line-type', 'change-addition');
    await expect(item.locator('[data-content] > [data-line-type^="context"]')).toHaveCount(0);
  });
});
