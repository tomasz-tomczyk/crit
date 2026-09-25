import { test, expect, type Locator, type Page } from '@playwright/test';
import { clearAllComments, loadPage, goSection, switchToDocumentView } from './helpers';

async function expectDocumentHighlightRange(
  section: Locator,
  startLine: number,
  endLine: number,
) {
  const states = await section.locator('.line-block[data-start-line][data-end-line]').evaluateAll(elements =>
    elements.map((element) => {
      const block = element as HTMLElement;
      return {
        range: `${block.dataset.startLine}-${block.dataset.endLine}`,
        start: Number(block.dataset.startLine),
        end: Number(block.dataset.endLine),
        highlighted: block.classList.contains('has-comment'),
      };
    }),
  );
  const expected = states
    .filter(block => block.end >= startLine && block.start <= endLine)
    .map(block => block.range);
  const actual = states.filter(block => block.highlighted).map(block => block.range);
  expect(actual).toEqual(expected);
  expect(expected.length).toBeGreaterThan(0);
}

// ============================================================
// Document View — Comment Range Highlighting
// ============================================================
test.describe('Comment Range Highlighting — Document View', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('multi-line comment highlights all blocks in range', async ({ page, request }) => {
    // Add a comment spanning lines 3-7 on plan.md via API
    const res = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 3, end_line: 7, body: 'Range test' },
    });
    expect(res.ok()).toBeTruthy();
    await loadPage(page);
    const section = await switchToDocumentView(page);
    await expect(section.locator('.line-block.has-comment').first()).toBeVisible();
    await expectDocumentHighlightRange(section, 3, 7);
  });

  test('single-line comment highlights only that block', async ({ page, request }) => {
    // Add comment on a single line
    const res = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Single line' },
    });
    expect(res.ok()).toBeTruthy();
    await loadPage(page);
    const section = await switchToDocumentView(page);
    await expect(section.locator('.line-block.has-comment').first()).toBeVisible();
    await expectDocumentHighlightRange(section, 1, 1);
  });

  test('deleting comment removes its highlight from all blocks', async ({ page, request }) => {
    // Two comments; delete the ranged one. Only the surviving comment's block
    // stays highlighted (proves highlighting runs and the deleted range clears).
    const res = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 3, end_line: 7, body: 'Will be deleted' },
    });
    const comment = await res.json();
    const keep = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Survivor' },
    });
    expect(keep.ok()).toBeTruthy();

    await request.delete(`/api/comment/${comment.id}?path=plan.md`);
    await loadPage(page);
    const section = await switchToDocumentView(page);
    await expect(section.locator('.comment-card', { hasText: 'Survivor' })).toBeVisible();
    await expectDocumentHighlightRange(section, 1, 1);
  });
});

// ============================================================
// Diff view — commented lines carry the comment-range tint
// (--crit-comment-range-bg, rgba(210, 153, 34, a)) instead of the
// addition/deletion colour, on the commented side only.
// ============================================================

const COMMENT_TINT = '210, 153, 34';

type RowState = { index: number; line: number; type: string; tinted: boolean };

// Rendered rows of one Pierre column in visual order, with whether each
// line's content cell shows the comment-range tint.
async function rowStates(item: Locator, column: 'unified' | 'additions' | 'deletions'): Promise<RowState[]> {
  const rows = item.locator(`code[data-${column}] [data-content] > [data-line]`);
  await expect(rows.first()).toBeVisible();
  return rows.evaluateAll((els, tint) => els.map((el, index) => ({
    index,
    line: Number((el as HTMLElement).dataset.line),
    type: (el as HTMLElement).dataset.lineType || '',
    tinted: getComputedStyle(el).backgroundColor.includes(tint),
  })), COMMENT_TINT);
}

// Pierre virtualizes lines inside a file too: an annotation below the fold
// stays unslotted until its line mounts. Wheel down through the file until
// the comment (and the lines around it) is on screen.
async function showCard(item: Locator, body: string) {
  const page = item.page();
  const card = item.locator('.comment-card', { hasText: body });
  await expect(card).toBeAttached();
  await expect(async () => {
    if (!(await card.isVisible())) {
      const box = await page.locator('#filesContainer').boundingBox();
      await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
      await page.mouse.wheel(0, 300);
    }
    await expect(card).toBeVisible({ timeout: 500 });
  }).toPass({ timeout: 15_000 });
}

async function useDiffStyle(page: Page, mode: 'split' | 'unified') {
  const btn = page.locator(`#diffModeToggle .toggle-btn[data-mode="${mode}"]`);
  await expect(btn).toBeVisible();
  await btn.click();
  await expect(btn).toHaveClass(/active/);
}

type DiffLine = { Type: string; OldNum: number; NewNum: number };
type Hunk = { Lines: DiffLine[] };

async function serverHunks(page: Page): Promise<Hunk[]> {
  const res = await page.request.get('/api/file/diff?path=server.go');
  const data = await res.json();
  const hunks = (data.hunks || []) as Hunk[];
  expect(hunks.length).toBeGreaterThan(0);
  return hunks;
}

// A deletion whose change block sits between two context lines. Returns the
// bracketing new-side context lines and the deletion's old line number.
function bracketedChange(hunks: Hunk[], which: 'first' | 'last') {
  const found: { startLine: number; endLine: number; delOld: number }[] = [];
  for (const hunk of hunks) {
    const lines = hunk.Lines;
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].Type !== 'del') continue;
      const before = lines.slice(0, i).reverse().find(l => l.Type !== 'del');
      const after = lines.slice(i + 1).find(l => l.Type === 'context');
      if (before?.Type === 'context' && after) {
        found.push({ startLine: before.NewNum, endLine: after.NewNum, delOld: lines[i].OldNum });
      }
    }
  }
  expect(found.length, 'fixture needs a deletion bracketed by context lines').toBeGreaterThan(0);
  return which === 'first' ? found[0] : found[found.length - 1];
}

test.describe('Comment Range Highlighting — Unified Diff', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('multi-line comment highlights all lines including deletions', async ({ page, request }) => {
    await loadPage(page);
    // A whole change block (deletions + additions) bracketed by context:
    // start on the context line before the first deletion, end on the next
    // context line after it.
    const { startLine, endLine } = bracketedChange(await serverHunks(page), 'first');

    const res = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: startLine, end_line: endLine, body: 'Unified range test' },
    });
    expect(res.ok()).toBeTruthy();

    await loadPage(page);
    await useDiffStyle(page, 'unified');
    const item = await goSection(page);
    await showCard(item, 'Unified range test');

    await expect(async () => {
      const states = await rowStates(item, 'unified');
      const isNew = (r: RowState) => r.type !== 'change-deletion';
      const startIdx = states.find(r => r.line === startLine && isNew(r))?.index;
      const endIdx = states.find(r => r.line === endLine && isNew(r))?.index;
      expect(startIdx).toBeDefined();
      expect(endIdx).toBeDefined();
      const expected = states.filter(r => r.index >= startIdx! && r.index <= endIdx!).map(r => r.index);
      // The span must include a deletion row, or this proves nothing.
      expect(states.filter(r => expected.includes(r.index) && r.type === 'change-deletion').length).toBeGreaterThan(0);
      expect(states.filter(r => r.tinted).map(r => r.index)).toEqual(expected);
    }).toPass();
  });

  test('deletion lines within comment range get the comment tint', async ({ page, request }) => {
    await loadPage(page);
    const { startLine, endLine, delOld } = bracketedChange(await serverHunks(page), 'last');

    const res = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: startLine, end_line: endLine, body: 'Spans deletion' },
    });
    expect(res.ok()).toBeTruthy();

    await loadPage(page);
    await useDiffStyle(page, 'unified');
    const item = await goSection(page);
    await showCard(item, 'Spans deletion');
    await expect.poll(async () => (await rowStates(item, 'unified'))
      .find(r => r.type === 'change-deletion' && r.line === delOld)?.tinted).toBe(true);
  });
});

test.describe('Comment Range Highlighting — Split Diff', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('multi-line comment highlights correct side in split view', async ({ page, request }) => {
    await loadPage(page);
    let startLine = 0;
    let endLine = 0;
    for (const hunk of await serverHunks(page)) {
      const adds = hunk.Lines.filter(l => l.Type === 'add');
      if (adds.length >= 3) { startLine = adds[0].NewNum; endLine = adds[2].NewNum; break; }
    }
    expect(startLine).toBeGreaterThan(0);

    const res = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: startLine, end_line: endLine, body: 'Split range test' },
    });
    expect(res.ok()).toBeTruthy();

    await loadPage(page);
    await useDiffStyle(page, 'split');
    const item = await goSection(page);
    await showCard(item, 'Split range test');

    const expectedLines = Array.from({ length: endLine - startLine + 1 }, (_, i) => startLine + i);
    await expect.poll(async () => (await rowStates(item, 'additions')).filter(r => r.tinted).map(r => r.line))
      .toEqual(expectedLines);
    expect((await rowStates(item, 'deletions')).filter(r => r.tinted)).toEqual([]);
  });
});
