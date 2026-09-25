import { test, expect, type Page, type Locator, type APIRequestContext } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, jsSection, revealFile, diffLine,
  diffLineNumber, openLineComment, showLine, setDiffStyle, reviewScroller,
} from './helpers';

// server.go has three git hunks (new 2..11, 20..57, 64..71). The gaps between
// them are 8 lines (new 12..19 ↔ old 9..16) and 6 lines (new 58..63 ↔ old
// 33..38) — both ≤ 8, so Crit merges them into one hunk before handing the
// diff to Pierre. Without that merge Pierre would collapse each gap behind a
// separator.
const SERVER_LAST_LINE = 71;
const GAP_LINES = [12, 13, 14, 15, 16, 17, 18, 19, 58, 59, 60, 61, 62, 63];

async function fileLines(request: APIRequestContext, path: string): Promise<string[]> {
  const res = await request.get(`/api/file?path=${encodeURIComponent(path)}`);
  expect(res.ok()).toBeTruthy();
  return ((await res.json()).content as string).split('\n');
}

// Scroll through a file and record, in visual order, every new-side line
// number and every separator Pierre renders, until `last` has been seen.
async function scanFile(page: Page, item: Locator, last: number): Promise<{ rows: string[] }> {
  const seen = new Map<string, number>();
  const rowsOf = () => item.locator('code[data-additions] [data-content], code[data-unified] [data-content]').evaluateAll(codes => {
    const out: { key: string; top: number }[] = [];
    const scroller = document.getElementById('filesContainer')!;
    for (const content of codes) {
      for (const el of Array.from(content.children)) {
        const top = Math.round(el.getBoundingClientRect().top + scroller.scrollTop);
        if (el.hasAttribute('data-separator')) out.push({ key: `sep@${top}`, top });
        else if (el.hasAttribute('data-line') && el.getAttribute('data-line-type') !== 'change-deletion') {
          out.push({ key: el.getAttribute('data-line')!, top });
        }
      }
    }
    return out;
  });
  // Start from the file's top (the header re-mounts while the file hydrates).
  await expect(async () => {
    await item.locator('.pierre-file-header').scrollIntoViewIfNeeded({ timeout: 1000 });
  }).toPass({ timeout: 10_000 });
  await expect.poll(async () => {
    for (const r of await rowsOf()) seen.set(r.key, r.top);
    if (seen.has(String(last))) return true;
    await reviewScroller(page).evaluate(el => el.scrollBy(0, 200));
    return false;
  }, { timeout: 15_000 }).toBe(true);
  return { rows: [...seen.entries()].sort((a, b) => a[1] - b[1]).map(e => e[0]) };
}

function range(from: number, to: number): string[] {
  return Array.from({ length: to - from + 1 }, (_, i) => String(from + i));
}

// ============================================================
// Auto-expand small gaps (≤ 8 lines) between diff hunks
// ============================================================
test.describe('Auto-expand small gaps — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('small gaps between hunks are auto-expanded (no separator visible)', async ({ page }) => {
    const item = await goSection(page);
    const { rows } = await scanFile(page, item, SERVER_LAST_LINE);

    expect(rows.filter(r => r.startsWith('sep'))).toEqual([]);
    for (const n of GAP_LINES) expect(rows).toContain(String(n));
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
      // Same visual row on both sides.
      const a = await diffLine(item, newNum).boundingBox();
      const b = await diffLine(item, oldNum, 'old').boundingBox();
      expect(Math.abs(a!.y - b!.y)).toBeLessThan(2);
    }
  });

  test('auto-expanded context lines are commentable (gutter + button works)', async ({ page, request }) => {
    const item = await goSection(page);
    await showLine(page, diffLine(item, 15));

    const form = await openLineComment(page, item, 15);
    await form.locator('textarea').fill('Comment on auto-expanded context line');
    await form.locator('.btn-primary').click();

    await expect(item.locator('.comment-card .comment-body')).toContainText('Comment on auto-expanded context line');
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=server.go')).json();
      return comments.map((c: { start_line: number; end_line: number; side?: string }) => `${c.start_line}-${c.end_line}:${c.side || ''}`);
    }).toEqual(['15-15:']);
  });

  test('all hunks merge into one contiguous block', async ({ page }) => {
    const item = await goSection(page);
    const { rows } = await scanFile(page, item, SERVER_LAST_LINE);
    // Every new-side line from 1 to EOF, in order, with nothing collapsed.
    expect(rows).toEqual(range(1, SERVER_LAST_LINE));
  });
});

test.describe('Auto-expand small gaps — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await setDiffStyle(page, 'unified');
    await expect((await goSection(page)).locator('code[data-unified]')).toBeVisible();
  });

  test('small gaps are auto-expanded in unified mode (no separator)', async ({ page }) => {
    const item = await goSection(page);
    const { rows } = await scanFile(page, item, SERVER_LAST_LINE);
    expect(rows.filter(r => r.startsWith('sep'))).toEqual([]);
    for (const n of GAP_LINES) expect(rows).toContain(String(n));
  });

  test('auto-expanded context lines in unified mode have correct line numbers', async ({ page, request }) => {
    const content = await fileLines(request, 'server.go');
    const item = await goSection(page);

    for (const [newNum, oldNum] of [[12, 9], [58, 33]]) {
      const line = diffLine(item, newNum);
      await showLine(page, line);
      await expect(line).toHaveAttribute('data-line-type', /^context/);
      await expect(diffLineNumber(item, newNum)).toHaveText(String(newNum));
      // The unified row carries its old-side number too.
      await expect(line).toHaveAttribute('data-alt-line', String(oldNum));
      const expected = content[newNum - 1];
      await expect(line).toHaveText(expected.length ? expected : /^\s*$/);
    }
  });

  test('auto-expanded context lines are commentable in unified mode', async ({ page, request }) => {
    const item = await goSection(page);
    await showLine(page, diffLine(item, 15));

    const form = await openLineComment(page, item, 15);
    await form.locator('textarea').fill('Unified auto-expanded comment');
    await form.locator('.btn-primary').click();

    await expect(item.locator('.comment-card .comment-body')).toContainText('Unified auto-expanded comment');
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=server.go')).json();
      return comments.map((c: { start_line: number; end_line: number; side?: string }) => `${c.start_line}-${c.end_line}:${c.side || ''}`);
    }).toEqual(['15-15:']);
  });
});

test.describe('Large gaps still show separator', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('large gaps (> 8 lines) still show separator with expand controls', async ({ page }) => {
    // routes.go has 37 unchanged lines (new 15..51) between its two hunks.
    const item = await revealFile(page, 'routes.go');
    const label = item.locator('[data-separator] [data-unmodified-lines]').filter({ visible: true }).first();
    await expect(label).toHaveText('37 unmodified lines');
    await expect(item.locator('[data-separator] [data-expand-button][data-expand-up]').filter({ visible: true }).first()).toBeVisible();
    await expect(item.locator('[data-separator] [data-expand-button][data-expand-down]').filter({ visible: true }).first()).toBeVisible();
    await expect(diffLine(item, 14)).toBeVisible();
    await expect(diffLine(item, 15)).toHaveCount(0);
  });
});

test.describe('Auto-expand does not break other files', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('handler.js (new file, single hunk) renders correctly', async ({ page }) => {
    const item = await jsSection(page);
    const { rows } = await scanFile(page, item, 19);
    expect(rows).toEqual(range(1, 19));
    await expect(diffLine(item, 1)).toHaveAttribute('data-line-type', 'change-addition');
    await expect(item.locator('[data-content] > [data-line-type^="context"]')).toHaveCount(0);
  });
});
