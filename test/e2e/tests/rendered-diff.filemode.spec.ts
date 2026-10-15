import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection, revealFile, diffLine, fileHeader, roundComplete, sessionCwd } from './helpers';

/**
 * Trigger a round-complete with a modified plan.md so that previousContent
 * differs from current content, producing diff hunks for the rendered diff.
 */
async function triggerRoundWithModifiedPlan(request: APIRequestContext) {
  const planPath = path.join(await sessionCwd(request), 'plan.md');

  // Read current plan.md and append unique content so repeated calls produce diffs
  const original = fs.readFileSync(planPath, 'utf-8');
  const timestamp = Date.now();
  const modified = original + `\n\n## Update ${timestamp}\n\nNew content added at ${timestamp}.\n`;
  fs.writeFileSync(planPath, modified);

  // Signal round-complete and wait until the server has re-read the file.
  await roundComplete(request);
  await expect(async () => {
    const diff = await (await request.get('/api/file/diff?path=plan.md')).json();
    expect(diff.previous_content).toBeTruthy();
    expect(diff.hunks?.length).toBeGreaterThan(0);
  }).toPass({ timeout: 5000 });
}

// Every test appends to plan.md; put the file (and the server's copy) back
// for the specs that run after this one.
let planPath: string;
let originalPlan: string;
test.beforeAll(async ({ request }) => {
  planPath = path.join(await sessionCwd(request), 'plan.md');
  originalPlan = fs.readFileSync(planPath, 'utf-8');
});
test.afterAll(async ({ request }) => {
  fs.writeFileSync(planPath, originalPlan);
  await roundComplete(request);
});

// ============================================================
// Rendered Diff — File Mode — Toggle Diff button
// ============================================================
test.describe('Rendered Diff — File Mode — Toggle Button', () => {
  test('Toggle Diff button appears once a round has diffs and toggles its active class', async ({ page, request }) => {
    // clearAllComments resets the session to round 1 with no previous
    // content, so there are no diffs yet and the button is hidden.
    await clearAllComments(request);
    await loadPage(page);
    const btn = page.locator('#diffToggle');
    await expect(btn).toBeHidden();

    // After a round-complete with changes the button appears, inactive.
    await triggerRoundWithModifiedPlan(request);
    await loadPage(page);
    await expect(btn).toBeVisible();
    await expect(btn).not.toHaveClass(/active/);

    await btn.click();
    await expect(btn).toHaveClass(/active/);
    await btn.click();
    await expect(btn).not.toHaveClass(/active/);
  });
});

// ============================================================
// Rendered Diff — File Mode — Split View
// ============================================================
test.describe('Rendered Diff — File Mode — Split View', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await triggerRoundWithModifiedPlan(request);
  });

  test('Toggle Diff shows a split diff of the round and toggles back to the document', async ({ page }) => {
    await loadPage(page);
    const section = await mdSection(page);

    // The per-file toggle has no Rendered button (the header Toggle Diff covers it).
    const toggle = fileHeader(page, 'plan.md').locator('.file-header-toggle');
    await expect(toggle).toBeVisible();
    await expect(toggle.locator('.toggle-btn[data-mode="document"]')).toBeVisible();
    await expect(toggle.locator('.toggle-btn[data-mode="rendered-diff"]')).toHaveCount(0);

    // Split view is the default.
    const btn = page.locator('#diffToggle');
    await btn.click();
    const diff = section.locator('.diff-view');
    await expect(diff).toBeVisible();
    await expect(section.locator('.document-wrapper')).toHaveCount(0);

    // Two labelled sides.
    const labels = section.locator('.diff-view-side-label');
    await expect(labels).toHaveCount(2);
    await expect(labels.nth(0)).toHaveText('Previous round');
    await expect(labels.nth(1)).toHaveText('Current round');

    // The appended section shows up as added on the current side.
    const addedBlocks = section.locator('.diff-view .line-block.diff-added');
    await expect(addedBlocks.first()).toBeVisible();
    await expect(addedBlocks.filter({ hasText: 'New content added at' }).last()).toBeVisible();

    // Cells alternate previous (even) / current (odd). Removed blocks only
    // ever sit on the previous side, added blocks only on the current side.
    const misplaced = await diff.evaluate(el => {
      const cells = Array.from(el.querySelectorAll(':scope > .diff-view-cell'));
      let bad = 0;
      cells.forEach((cell, i) => {
        if (i % 2 === 1 && cell.querySelector('.line-block.diff-removed')) bad++;
        if (i % 2 === 0 && cell.querySelector('.line-block.diff-added')) bad++;
      });
      return { bad, cells: cells.length };
    });
    expect(misplaced.cells).toBeGreaterThan(0);
    expect(misplaced.bad).toBe(0);

    // Line numbers; the left side is not commentable, the right side is.
    const lineNums = section.locator('.diff-view .line-num');
    await expect(lineNums.first()).toBeVisible();
    await expect(lineNums.first()).toHaveText(/^\d+$/);
    await expect(section.locator('.diff-view .diff-no-comment')).not.toHaveCount(0);
    await expect(section.locator('.diff-view .line-comment-gutter:not(.diff-no-comment)')).not.toHaveCount(0);

    // Clicking Toggle Diff again returns to document view.
    await btn.click();
    await expect(diff).toHaveCount(0);
    await expect(section.locator('.document-wrapper')).toBeVisible();
  });
});

// ============================================================
// Rendered Diff — File Mode — Unified View
// ============================================================
test.describe('Rendered Diff — File Mode — Unified View', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await triggerRoundWithModifiedPlan(request);
  });

  test('Split/Unified toggle follows Toggle Diff and switches between the two views', async ({ page }) => {
    await loadPage(page);
    const diffModeToggle = page.locator('#diffModeToggle');
    await expect(diffModeToggle).toBeHidden();

    // Appears when Toggle Diff is active.
    await page.locator('#diffToggle').click();
    await expect(diffModeToggle).toBeVisible();

    // Unified view replaces the split view.
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    const section = await mdSection(page);
    const unified = section.locator('.diff-view-unified');
    await expect(unified).toBeVisible();
    await expect(section.locator('.diff-view')).toHaveCount(0);

    // Added blocks and line numbers in unified view.
    const addedBlocks = section.locator('.diff-view-unified .line-block.diff-added');
    await expect(addedBlocks.first()).toBeVisible();
    await expect(addedBlocks.filter({ hasText: 'New content added at' }).last()).toBeVisible();
    const lineNums = section.locator('.diff-view-unified .line-num');
    await expect(lineNums.first()).toBeVisible();
    await expect(lineNums.first()).toHaveText(/^\d+$/);

    // Switching back to Split shows the split view.
    await page.locator('#diffModeToggle .toggle-btn[data-mode="split"]').click();
    await expect(section.locator('.diff-view')).toBeVisible();
    await expect(unified).toHaveCount(0);

    // Hides again when Toggle Diff is deactivated.
    await page.locator('#diffToggle').click();
    await expect(diffModeToggle).toBeHidden();
  });
});

// ============================================================
// Rendered Diff — File Mode — Comments in Diff View
// ============================================================
test.describe('Rendered Diff — File Mode — Comments', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await triggerRoundWithModifiedPlan(request);
  });

  test('can add comment on current side of split diff', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = await mdSection(page);

    // Find a commentable gutter (right/current side has commentable gutters)
    const gutter = section.locator('.diff-view .line-comment-gutter:not(.diff-no-comment)').first();
    await gutter.scrollIntoViewIfNeeded();
    // Hover over the parent line block to make gutter visible
    const lineBlock = gutter.locator('..');
    await lineBlock.hover();
    await gutter.click();

    // Fill and submit comment
    await page.locator('.comment-form textarea').fill('Comment in diff view');
    await page.locator('.comment-form .btn-primary').click();

    // Comment should appear
    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-body')).toContainText('Comment in diff view');
  });

  test('can add comment in unified diff view', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const section = await mdSection(page);

    // Find a commentable gutter (not diff-no-comment)
    const gutter = section.locator('.diff-view-unified .line-comment-gutter:not(.diff-no-comment)').first();
    await gutter.scrollIntoViewIfNeeded();
    const lineBlock = section.locator('.diff-view-unified .line-block').first();
    await lineBlock.hover();
    await gutter.click();

    await page.locator('.comment-form textarea').fill('Unified diff comment');
    await page.locator('.comment-form .btn-primary').click();

    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-body')).toContainText('Unified diff comment');
  });

  test('comments survive toggling diff off and back on', async ({ page, request }) => {
    // Add a comment via API on plan.md
    await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Persistent comment' },
    });

    await loadPage(page);
    await page.locator('#diffToggle').click();

    // Comment should be visible in split diff
    const section = await mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();

    // Toggle off
    await page.locator('#diffToggle').click();
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Toggle back on
    await page.locator('#diffToggle').click();
    await expect(section.locator('.diff-view')).toBeVisible();
    await expect(section.locator('.comment-card')).toBeVisible();
  });
});

// ============================================================
// Rendered Diff — File Mode — Word Diff on Paragraph Reflow
// ============================================================
test.describe('Rendered Diff — File Mode — Paragraph Reflow Word Diff', () => {
  test('word-diff does not highlight common words shifted by line reflow', async ({ page, request }) => {
    await clearAllComments(request);
    const original = fs.readFileSync(planPath, 'utf-8');

    // Replace the overview paragraph with text that wraps at ~80 chars.
    // V1 wraps as: "...from the queue and\ndispatches...ship in the\nMVP..."
    const v1Para = [
      'The delivery worker runs as a separate long-lived process. It reads from the',
      'queue and dispatches to channel handlers. Handlers are pluggable - email and',
      'webhook ship in the MVP, others can be added later without changing the core.',
    ].join('\n');
    // V2 inserts "SQS" causing reflow: "...from the SQS queue\nand dispatches...ship in\nthe MVP..."
    const v2Para = [
      'The delivery worker runs as a separate long-lived process. It reads from the',
      'SQS queue and dispatches to channel handlers. Handlers are pluggable - email',
      'and webhook ship in the MVP, others can be added later without changing the',
      'core.',
    ].join('\n');

    // Write v1 content, trigger round to snapshot it
    const v1Content = original.replace(/^## Overview\n\n.*?(?=\n\n##)/ms, '## Overview\n\n' + v1Para);
    fs.writeFileSync(planPath, v1Content);
    await roundComplete(request);

    // Now write v2 content (SQS insertion + reflow) and trigger another round.
    // The file watcher polls every 1s and must detect the edit before
    // round-complete snapshots previousContent. Wait for the watcher to
    // register the change by polling the file/diff endpoint for mtime changes,
    // then trigger round-complete once.
    const v2Content = v1Content.replace(v1Para, v2Para);
    fs.writeFileSync(planPath, v2Content);
    await expect.poll(async () => {
      const file = await (await request.get('/api/file?path=plan.md')).json();
      return file.content as string;
    }, { timeout: 5000 }).toContain('SQS');
    await roundComplete(request);
    await expect(async () => {
      const diffRes = await request.get('/api/file/diff?path=plan.md');
      const diff = await diffRes.json();
      expect(diff.hunks?.length).toBeGreaterThan(0);
    }).toPass({ timeout: 5000 });

    // Load page, enable unified diff view
    await loadPage(page);
    await page.locator('#diffToggle').click();
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    const section = await mdSection(page);
    await expect(section.locator('.diff-view-unified')).toBeVisible();

    // Collect all word-diff highlight text
    const wordDelSpans = section.locator('.diff-word-del');
    const wordAddSpans = section.locator('.diff-word-add');

    // There should be word-diff spans (SQS was added)
    await expect(wordAddSpans).not.toHaveCount(0);

    // Collect the highlighted text from word-diff spans
    const addTexts = (await wordAddSpans.allTextContents()).map(t => t.trim());
    const delTexts = (await wordDelSpans.allTextContents()).map(t => t.trim());

    // "SQS" should be highlighted as added
    expect(addTexts.some(t => t.includes('SQS'))).toBeTruthy();

    // Common words that merely shifted across line breaks must NOT be highlighted
    const falsePositives = ['and', 'the', 'in'];
    for (const word of falsePositives) {
      const inDel = delTexts.some(t => t === word);
      const inAdd = addTexts.some(t => t === word);
      expect(inDel, `"${word}" should not be in word-diff deletions`).toBeFalsy();
      expect(inAdd, `"${word}" should not be in word-diff additions`).toBeFalsy();
    }
  });
});

// ============================================================
// Rendered Diff — File Mode — Non-markdown files unaffected
// ============================================================
test.describe('Rendered Diff — File Mode — Non-markdown', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await triggerRoundWithModifiedPlan(request);
  });

  test('code files stay in normal view when Toggle Diff is active', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    // plan.md should show diff view
    const md = await mdSection(page);
    await expect(md.locator('.diff-view')).toBeVisible();

    // server.go should NOT show diff-view: it stays a whole-file code view
    const goSection = await revealFile(page, 'server.go');
    await expect(diffLine(goSection, 1)).toContainText('package main');
    await expect(goSection.locator('.diff-view')).toHaveCount(0);
    await expect(goSection.locator('.diff-view-unified')).toHaveCount(0);
    await expect(goSection.locator('code[data-additions], code[data-deletions], code[data-unified]')).toHaveCount(0);
  });
});

// ============================================================
// Rendered Diff — File Mode — SSE / Round-Complete integration
// ============================================================
test.describe('Rendered Diff — File Mode — Round-Complete', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('Toggle Diff resets to off after round-complete SSE', async ({ page, request }) => {
    await triggerRoundWithModifiedPlan(request);
    await loadPage(page);

    // Activate diff
    await page.locator('#diffToggle').click();
    await expect(page.locator('#diffToggle')).toHaveClass(/active/);
    await expect((await mdSection(page)).locator('.diff-view')).toBeVisible();

    // Trigger another round-complete (modify file again)
    const content = fs.readFileSync(planPath, 'utf-8');
    fs.writeFileSync(planPath, content + '\n\n## Appendix\n\nAdditional notes.\n');

    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // diffActive should be reset to false
    await expect(page.locator('#diffToggle')).not.toHaveClass(/active/);
    // Should show document view, not diff view
    await expect((await mdSection(page)).locator('.document-wrapper')).toBeVisible();
  });

  test('Toggle Diff button is visible after SSE round-complete with changes', async ({ page, request }) => {
    // Modify file and trigger round-complete via SSE flow
    const content = fs.readFileSync(planPath, 'utf-8');
    fs.writeFileSync(planPath, content + `\n\n## New Section ${Date.now()}\n\nAdded via test.\n`);

    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Toggle Diff should be visible after round-complete with file changes
    await expect(page.locator('#diffToggle')).toBeVisible();
  });
});
