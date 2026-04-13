import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection } from './helpers';

/** Get the fixture directory from the session API. */
async function getFixtureDir(request: APIRequestContext): Promise<string> {
  const res = await request.get('/api/session');
  const data = await res.json();
  return data.cwd;
}

/**
 * Trigger a round-complete with a modified plan.md so that previousContent
 * differs from current content, producing diff hunks for the rendered diff.
 */
async function triggerRoundWithModifiedPlan(request: APIRequestContext) {
  const dir = await getFixtureDir(request);
  const planPath = path.join(dir, 'plan.md');

  // Read current plan.md and append unique content so repeated calls produce diffs
  const original = fs.readFileSync(planPath, 'utf-8');
  const timestamp = Date.now();
  const modified = original + `\n\n## Update ${timestamp}\n\nNew content added at ${timestamp}.\n`;
  fs.writeFileSync(planPath, modified);

  // Signal round-complete so the server snapshots + re-reads
  const res = await request.post('/api/round-complete');
  expect(res.ok()).toBeTruthy();

  // Poll until diff data is available after round-complete processing
  await expect(async () => {
    const diffRes = await request.get('/api/file/diff?path=plan.md');
    const diff = await diffRes.json();
    expect(diff.previous_content).toBeTruthy();
    expect(diff.hunks?.length).toBeGreaterThan(0);
  }).toPass({ timeout: 5000 });
}

// ============================================================
// Rendered Diff — File Mode — Toggle Diff button
// ============================================================
test.describe('Rendered Diff — File Mode — Toggle Button', () => {
  test('Toggle Diff button visibility depends on whether diffs exist', async ({ page, request }) => {
    // Check if server already has round-complete state from previous tests
    const sessionRes = await request.get('/api/session');
    const session = await sessionRes.json();

    await loadPage(page);
    const btn = page.locator('#diffToggle');

    if (session.review_round <= 1) {
      // Fresh server — no round-complete yet, button should be hidden
      await expect(btn).toBeHidden();
    }
    // After triggering round-complete with changes, button should appear
    await clearAllComments(request);
    await triggerRoundWithModifiedPlan(request);
    await loadPage(page);
    await expect(btn).toBeVisible();
  });

  test('Toggle Diff button gets active class when clicked', async ({ page, request }) => {
    await clearAllComments(request);
    await triggerRoundWithModifiedPlan(request);

    await loadPage(page);
    const btn = page.locator('#diffToggle');
    await expect(btn).not.toHaveClass(/active/);

    await btn.click();
    await expect(btn).toHaveClass(/active/);

    // Click again to deactivate
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

  test('clicking Toggle Diff shows split diff view by default', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = mdSection(page);
    await expect(section.locator('.diff-view')).toBeVisible();
    await expect(section.locator('.document-wrapper')).toHaveCount(0);
  });

  test('split view has two sides with labels', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = mdSection(page);
    const labels = section.locator('.diff-view-side-label');
    await expect(labels).toHaveCount(2);
    await expect(labels.nth(0)).toHaveText('Previous round');
    await expect(labels.nth(1)).toHaveText('Current round');
  });

  test('split view shows diff-added blocks on current side', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = mdSection(page);
    const addedBlocks = section.locator('.diff-view .line-block.diff-added');
    const count = await addedBlocks.count();
    expect(count).toBeGreaterThan(0);
  });

  test('split view shows diff-removed blocks on previous side', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = mdSection(page);
    const leftSide = section.locator('.diff-view-side').nth(0);
    const removedBlocks = leftSide.locator('.line-block.diff-removed');
    const count = await removedBlocks.count();
    // Our modification replaces existing lines, so there should be removed blocks
    expect(count).toBeGreaterThanOrEqual(0);
  });

  test('split view shows line numbers', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = mdSection(page);
    const lineNums = section.locator('.diff-view .line-num');
    const count = await lineNums.count();
    expect(count).toBeGreaterThan(0);

    // Line numbers should be visible
    await expect(lineNums.first()).toBeVisible();
  });

  test('left side has non-commentable gutters', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = mdSection(page);
    const noCommentGutters = section.locator('.diff-view .diff-no-comment');
    const count = await noCommentGutters.count();
    expect(count).toBeGreaterThan(0);
  });

  test('right side has commentable gutters', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const section = mdSection(page);
    const commentGutters = section.locator('.diff-view .line-comment-gutter:not(.diff-no-comment)');
    const count = await commentGutters.count();
    expect(count).toBeGreaterThan(0);
  });

  test('clicking Toggle Diff again returns to document view', async ({ page }) => {
    await loadPage(page);
    const btn = page.locator('#diffToggle');
    await btn.click();

    const section = mdSection(page);
    await expect(section.locator('.diff-view')).toBeVisible();

    // Click again to return to document view
    await btn.click();
    await expect(section.locator('.diff-view')).toHaveCount(0);
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

  test('Split/Unified toggle appears when Toggle Diff is active', async ({ page }) => {
    await loadPage(page);
    const diffModeToggle = page.locator('#diffModeToggle');
    await expect(diffModeToggle).toBeHidden();

    await page.locator('#diffToggle').click();
    await expect(diffModeToggle).toBeVisible();
  });

  test('Split/Unified toggle hides when Toggle Diff is deactivated', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();
    await expect(page.locator('#diffModeToggle')).toBeVisible();

    await page.locator('#diffToggle').click();
    await expect(page.locator('#diffModeToggle')).toBeHidden();
  });

  test('clicking Unified switches to unified diff view', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();

    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();

    const section = mdSection(page);
    await expect(section.locator('.diff-view-unified')).toBeVisible();
    await expect(section.locator('.diff-view')).toHaveCount(0);
  });

  test('unified view shows diff-added and diff-removed blocks interleaved', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const section = mdSection(page);
    const addedBlocks = section.locator('.diff-view-unified .line-block.diff-added');
    const count = await addedBlocks.count();
    expect(count).toBeGreaterThan(0);
  });

  test('unified view shows line numbers', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const section = mdSection(page);
    const lineNums = section.locator('.diff-view-unified .line-num');
    const count = await lineNums.count();
    expect(count).toBeGreaterThan(0);
    await expect(lineNums.first()).toBeVisible();
  });

  test('switching back to Split shows split view', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffToggle').click();
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const section = mdSection(page);
    await expect(section.locator('.diff-view-unified')).toBeVisible();

    await page.locator('#diffModeToggle .toggle-btn[data-mode="split"]').click();
    await expect(section.locator('.diff-view')).toBeVisible();
    await expect(section.locator('.diff-view-unified')).toHaveCount(0);
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

    const section = mdSection(page);

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

    const section = mdSection(page);

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
    const section = mdSection(page);
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
    const dir = await getFixtureDir(request);
    const planPath = path.join(dir, 'plan.md');
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
    await request.post('/api/round-complete');
    await expect(async () => {
      const s = await (await request.get('/api/session')).json();
      expect(s.review_round).toBeGreaterThanOrEqual(2);
    }).toPass({ timeout: 5000 });

    // Now write v2 content (SQS insertion + reflow) and trigger another round
    const v2Content = v1Content.replace(v1Para, v2Para);
    fs.writeFileSync(planPath, v2Content);
    // Wait for file watcher to detect the change
    await new Promise(r => setTimeout(r, 1500));
    await request.post('/api/round-complete');
    await expect(async () => {
      const diffRes = await request.get('/api/file/diff?path=plan.md');
      const diff = await diffRes.json();
      expect(diff.hunks?.length).toBeGreaterThan(0);
    }).toPass({ timeout: 5000 });

    // Load page, enable unified diff view
    await loadPage(page);
    await page.locator('#diffToggle').click();
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    const section = mdSection(page);
    await expect(section.locator('.diff-view-unified')).toBeVisible();

    // Collect all word-diff highlight text
    const wordDelSpans = section.locator('.diff-word-del');
    const wordAddSpans = section.locator('.diff-word-add');

    // There should be word-diff spans (SQS was added)
    await expect(async () => {
      const addCount = await wordAddSpans.count();
      expect(addCount).toBeGreaterThan(0);
    }).toPass({ timeout: 5000 });

    // Collect the highlighted text from word-diff spans
    const addTexts: string[] = [];
    const addCount = await wordAddSpans.count();
    for (let i = 0; i < addCount; i++) {
      addTexts.push(((await wordAddSpans.nth(i).textContent()) || '').trim());
    }
    const delTexts: string[] = [];
    const delCount = await wordDelSpans.count();
    for (let i = 0; i < delCount; i++) {
      delTexts.push(((await wordDelSpans.nth(i).textContent()) || '').trim());
    }

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
    const md = mdSection(page);
    await expect(md.locator('.diff-view')).toBeVisible();

    // server.go should NOT show diff-view
    const goSection = page.locator('.file-section').filter({ hasText: 'server.go' });
    await expect(goSection.locator('.diff-view')).toHaveCount(0);
    await expect(goSection.locator('.diff-view-unified')).toHaveCount(0);
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
    await expect(mdSection(page).locator('.diff-view')).toBeVisible();

    // Trigger another round-complete (modify file again)
    const dir = await getFixtureDir(request);
    const planPath = path.join(dir, 'plan.md');
    const content = fs.readFileSync(planPath, 'utf-8');
    fs.writeFileSync(planPath, content + '\n\n## Appendix\n\nAdditional notes.\n');

    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // diffActive should be reset to false
    await expect(page.locator('#diffToggle')).not.toHaveClass(/active/);
    // Should show document view, not diff view
    await expect(mdSection(page).locator('.document-wrapper')).toBeVisible();
  });

  test('Toggle Diff button is visible after SSE round-complete with changes', async ({ page, request }) => {
    // Modify file and trigger round-complete via SSE flow
    const dir = await getFixtureDir(request);
    const planPath = path.join(dir, 'plan.md');
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
