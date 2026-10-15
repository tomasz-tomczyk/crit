import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  loadPage, goSection, revealFile, fileHeader, diffLine, diffLineNumber,
  hoverLine, selectedLines, clearAllComments, showLine, setDiffStyle, waitForScrollStable,
} from './helpers';

// Git-mode files render through Pierre (see helpers.ts). server.go has
// adjacent del/add pairs. Separators and context expansion (routes.go's
// 37-line gap) are covered by incremental-expand.spec.ts.

test.describe('Diff Rendering — Split Mode (default)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('split diff by default: old left, new right, additions and deletions on their own side', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    await expect(item.locator('pre[data-diff-type="split"]')).toBeVisible();
    await expect(item.locator('code[data-unified]')).toHaveCount(0);

    // Old code on the left, new code on the right, side by side.
    const left = item.locator('code[data-deletions]');
    const right = item.locator('code[data-additions]');
    await expect(left).toBeVisible();
    await expect(right).toBeVisible();
    const l = await left.boundingBox();
    const r = await right.boundingBox();
    expect(l!.x + l!.width).toBeLessThanOrEqual(r!.x + 1);

    // server.go new line 5 (`"log"` import) is added, on the new side only.
    const added = diffLine(item, 5);
    await expect(added).toBeVisible();
    await expect(added).toHaveAttribute('data-line-type', 'change-addition');
    await expect(added).toContainText('"log"');
    await expect(item.locator('code[data-deletions] [data-line-type="change-addition"]')).toHaveCount(0);

    // Old line 23 is deleted, on the old side only.
    const deleted = diffLine(item, 23, 'old');
    await showLine(page, deleted);
    await expect(deleted).toHaveAttribute('data-line-type', 'change-deletion');
    await expect(deleted).toContainText('r.URL.Path[1:]');
    await expect(item.locator('code[data-additions] [data-line-type="change-deletion"]')).toHaveCount(0);

    // Old line 42 was replaced by new line 67.
    await showLine(page, diffLine(item, 67));
    await expect(diffLine(item, 42, 'old')).toContainText('fmt.Println("Server starting on :8080")');
    await expect(diffLine(item, 67)).toContainText('log.Printf("Server starting on :%s", port)');
  });

  test('deleted file shows "This file was deleted."', async ({ page }) => {
    await loadPage(page);
    const item = await revealFile(page, 'deleted.txt');
    const header = fileHeader(page, 'deleted.txt');
    await expect(header.locator('.file-header-badge.deleted')).toBeVisible();
    if (await header.evaluate(el => el.classList.contains('collapsed'))) {
      await header.locator('.file-header-name').click();
      await expect(header).not.toHaveClass(/\bcollapsed\b/);
    }
    // The expanded body must explain itself rather than render empty.
    await expect(item.getByText('This file was deleted.')).toBeVisible();
  });
});

test.describe('Diff Mode Toggle', () => {
  test('switches to unified, persists across reload, and switches back to split', async ({ page }) => {
    await loadPage(page);
    let item = await goSection(page);
    await expect(item.locator('pre[data-diff-type="split"]')).toBeVisible();

    await setDiffStyle(page, 'unified');
    await expect(item.locator('code[data-unified]')).toBeVisible();
    await expect(item.locator('code[data-additions], code[data-deletions]')).toHaveCount(0);

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
    await expect(item.locator('pre[data-diff-type="split"]')).toHaveCount(0);

    await setDiffStyle(page, 'split');
    await expect(item.locator('pre[data-diff-type="split"]')).toBeVisible();
    await expect(item.locator('code[data-unified]')).toHaveCount(0);
  });
});

test.describe('Unified Mode — Drag Indicator Across Line Types', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  // server.go unified order around here: 40+ | 21- | 41+ | 42 | 43+
  function unifiedSelected(item: Locator): Locator {
    return item.locator('code[data-unified] [data-content] > [data-selected-line]');
  }

  // Press the "+" of `from` and move (button held) to the row of `to`. Both
  // rows are mounted first, and `to` is measured only after the list has
  // stopped scrolling — Pierre re-mounts rows while it settles.
  async function dragHeld(page: Page, item: Locator, from: Locator, to: Locator, fromLine: number, fromSide: 'new' | 'old') {
    await showLine(page, from);
    await showLine(page, to);
    const plus = await hoverLine(page, item, fromLine, fromSide);
    await waitForScrollStable(page);
    const start = await plus.boundingBox();
    const end = await to.boundingBox();
    expect(start && end).toBeTruthy();
    await page.mouse.move(start!.x + start!.width / 2, start!.y + start!.height / 2);
    await page.mouse.down();
    await page.mouse.move(start!.x + start!.width / 2, end!.y + end!.height / 2, { steps: 8 });
  }

  test('drag indicator shows on deletion lines when dragging from addition line', async ({ page }) => {
    await loadPage(page);
    await setDiffStyle(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();

    await dragHeld(page, item, diffLine(item, 41), diffLineNumber(item, 21, 'old'), 41, 'new');

    // The deletion line the pointer is on is part of the drag range.
    await expect(unifiedSelected(item).and(item.locator('[data-line-type="change-deletion"]'))).toHaveCount(1);
    await expect(diffLine(item, 21, 'old')).toHaveAttribute('data-selected-line', /.*/);
    await expect(diffLine(item, 41)).toHaveAttribute('data-selected-line', /.*/);

    await page.mouse.up();
    await expect(page.locator('#filesContainer .comment-form textarea')).toBeVisible();
  });

  test('all lines between drag endpoints are selected in unified mode', async ({ page }) => {
    await loadPage(page);
    await setDiffStyle(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();

    // Drag from the deletion (old 21) down to the addition (new 43):
    // four visual rows of mixed type.
    await dragHeld(page, item, diffLine(item, 21, 'old'), diffLineNumber(item, 43), 21, 'old');

    await expect(unifiedSelected(item)).toHaveCount(4);
    for (const line of [diffLine(item, 21, 'old'), diffLine(item, 41), diffLine(item, 42), diffLine(item, 43)]) {
      await expect(line).toHaveAttribute('data-selected-line', /.*/);
    }
    await expect(selectedLines(page)).toHaveCount(4);

    await page.mouse.up();
  });
});
