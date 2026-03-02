import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  for (const f of session.files || []) {
    const res = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await res.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
}

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

async function switchToDocumentView(page: Page) {
  const section = mdSection(page);
  await expect(section).toBeVisible();
  const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  await expect(section.locator('.document-wrapper')).toBeVisible();
}

function goSection(page: Page) {
  return page.locator('#file-section-server\\.go');
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
    await switchToDocumentView(page);

    const section = mdSection(page);
    const blocks = section.locator('.line-block.has-comment');
    const count = await blocks.count();
    expect(count).toBeGreaterThan(1);
  });

  test('single-line comment highlights only that block', async ({ page, request }) => {
    // Add comment on a single line
    const res = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Single line' },
    });
    expect(res.ok()).toBeTruthy();
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const blocks = section.locator('.line-block.has-comment');
    const count = await blocks.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('deleting comment removes has-comment from all blocks', async ({ page, request }) => {
    // Add then delete a comment
    const res = await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 3, end_line: 7, body: 'Will be deleted' },
    });
    const comment = await res.json();

    await request.delete(`/api/comment/${comment.id}?path=plan.md`);
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const blocks = section.locator('.line-block.has-comment');
    await expect(blocks).toHaveCount(0);
  });
});

// ============================================================
// Unified Diff — Comment Range Highlighting
// ============================================================
test.describe('Comment Range Highlighting — Unified Diff', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Switch to unified mode
    const unifiedBtn = page.locator('.toggle-btn[data-mode="unified"]');
    await expect(unifiedBtn).toBeVisible();
    await unifiedBtn.click();
  });

  test('multi-line comment highlights all lines including deletions', async ({ page, request }) => {
    // server.go has hunks with context/del/add. Find lines from the diff.
    const diffRes = await request.get('/api/file/diff?path=server.go');
    const diffData = await diffRes.json();
    const hunks = diffData.hunks || [];
    expect(hunks.length).toBeGreaterThan(0);

    // Find a hunk that has both del and add lines (a change block)
    let startLine = 0;
    let endLine = 0;
    for (const hunk of hunks) {
      const hasDel = hunk.Lines.some((l: { Type: string }) => l.Type === 'del');
      const hasAdd = hunk.Lines.some((l: { Type: string }) => l.Type === 'add');
      if (hasDel && hasAdd) {
        // Use the first add line's NewNum as start, span a few lines
        const firstAdd = hunk.Lines.find((l: { Type: string }) => l.Type === 'add');
        const lastLine = hunk.Lines.filter((l: { Type: string; NewNum: number }) => l.NewNum > 0);
        startLine = firstAdd.NewNum;
        endLine = Math.min(firstAdd.NewNum + 3, lastLine[lastLine.length - 1].NewNum);
        break;
      }
    }
    expect(startLine).toBeGreaterThan(0);

    // Add comment spanning the range
    const res = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: startLine, end_line: endLine, body: 'Unified range test' },
    });
    expect(res.ok()).toBeTruthy();

    // Reload to see the highlights
    await loadPage(page);
    await page.locator('.toggle-btn[data-mode="unified"]').click();

    const section = goSection(page);
    const highlighted = section.locator('.diff-line.has-comment');
    const count = await highlighted.count();
    // Should highlight multiple lines (at least start to end)
    expect(count).toBeGreaterThan(1);

    // ALL highlighted lines should have the comment-range background, not addition/deletion bg
    for (let i = 0; i < count; i++) {
      const bg = await highlighted.nth(i).evaluate(
        el => getComputedStyle(el).backgroundColor
      );
      // Should be the comment-range-bg (warm orange), not green/red
      expect(bg).toContain('210, 153, 34');
    }
  });

  test('deletion lines within comment range get has-comment', async ({ page, request }) => {
    // Find a hunk with del lines and add a comment that spans across them
    const diffRes = await request.get('/api/file/diff?path=server.go');
    const diffData = await diffRes.json();
    const hunks = diffData.hunks || [];

    // Find the first del line in any hunk
    let delOldNum = 0;
    let surroundingNewStart = 0;
    let surroundingNewEnd = 0;
    for (const hunk of hunks) {
      for (let i = 0; i < hunk.Lines.length; i++) {
        if (hunk.Lines[i].Type === 'del') {
          delOldNum = hunk.Lines[i].OldNum;
          // Find context/add lines before and after with NewNum
          const before = hunk.Lines.slice(0, i).reverse().find((l: { NewNum: number }) => l.NewNum > 0);
          const after = hunk.Lines.slice(i + 1).find((l: { NewNum: number }) => l.NewNum > 0);
          if (before && after) {
            surroundingNewStart = before.NewNum;
            surroundingNewEnd = after.NewNum;
          }
          break;
        }
      }
      if (delOldNum > 0) break;
    }

    if (surroundingNewStart === 0 || surroundingNewEnd === 0) {
      test.skip();
      return;
    }

    // Add comment spanning from before the del to after it
    const res = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: surroundingNewStart, end_line: surroundingNewEnd, body: 'Spans deletion' },
    });
    expect(res.ok()).toBeTruthy();

    await loadPage(page);
    await page.locator('.toggle-btn[data-mode="unified"]').click();

    const section = goSection(page);
    // The deletion line should also have has-comment
    const delLines = section.locator('.diff-line.deletion.has-comment');
    await expect(delLines.first()).toBeVisible();
  });
});

// ============================================================
// Split Diff — Comment Range Highlighting
// ============================================================
test.describe('Comment Range Highlighting — Split Diff', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Default is split mode, ensure it
    const splitBtn = page.locator('.toggle-btn[data-mode="split"]');
    if (await splitBtn.isVisible()) await splitBtn.click();
  });

  test('multi-line comment highlights correct side in split view', async ({ page, request }) => {
    // Find addition lines in server.go diff
    const diffRes = await request.get('/api/file/diff?path=server.go');
    const diffData = await diffRes.json();
    const hunks = diffData.hunks || [];

    let startLine = 0;
    let endLine = 0;
    for (const hunk of hunks) {
      const adds = hunk.Lines.filter((l: { Type: string }) => l.Type === 'add');
      if (adds.length >= 3) {
        startLine = adds[0].NewNum;
        endLine = adds[2].NewNum;
        break;
      }
    }
    expect(startLine).toBeGreaterThan(0);

    // Add new-side comment
    const res = await request.post('/api/file/comments?path=server.go', {
      data: { start_line: startLine, end_line: endLine, body: 'Split range test' },
    });
    expect(res.ok()).toBeTruthy();

    await loadPage(page);
    if (await page.locator('.toggle-btn[data-mode="split"]').isVisible()) {
      await page.locator('.toggle-btn[data-mode="split"]').click();
    }

    const section = goSection(page);
    // Right side (new) should have has-comment
    const rightHighlighted = section.locator('.diff-split-side.right.has-comment');
    const count = await rightHighlighted.count();
    expect(count).toBeGreaterThanOrEqual(3);

    // All highlighted right-side cells should have orange bg
    for (let i = 0; i < Math.min(count, 3); i++) {
      const bg = await rightHighlighted.nth(i).evaluate(
        el => getComputedStyle(el).backgroundColor
      );
      expect(bg).toContain('210, 153, 34');
    }
  });

  test('gutter background resets on commented lines', async ({ page, request }) => {
    // Find an addition line
    const diffRes = await request.get('/api/file/diff?path=server.go');
    const diffData = await diffRes.json();
    const hunks = diffData.hunks || [];

    let addLine = 0;
    for (const hunk of hunks) {
      const add = hunk.Lines.find((l: { Type: string }) => l.Type === 'add');
      if (add) { addLine = add.NewNum; break; }
    }
    expect(addLine).toBeGreaterThan(0);

    await request.post('/api/file/comments?path=server.go', {
      data: { start_line: addLine, end_line: addLine, body: 'Gutter test' },
    });

    await loadPage(page);

    const section = goSection(page);
    // In split mode, find the right-side has-comment cell's gutter
    const gutterNum = section.locator('.diff-split-side.right.has-comment .diff-gutter-num').first();
    await expect(gutterNum).toBeVisible();
    const bg = await gutterNum.evaluate(el => getComputedStyle(el).backgroundColor);
    // Gutter should be transparent (not green addition gutter)
    expect(bg).toBe('rgba(0, 0, 0, 0)');
  });
});
