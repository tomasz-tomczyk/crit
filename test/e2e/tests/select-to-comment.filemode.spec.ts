import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, revealFile, diffLine } from './helpers';

// File mode renders code files as whole-file Pierre items (no diff), which is
// a rendering path not covered by git-mode tests. Markdown document view
// tests are intentionally omitted — they duplicate git-mode coverage.

test.describe('Select-to-comment (file mode) — code file view', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('selecting code text preserves copy selection then creates a comment with c', async ({ page, request }) => {
    const item = await revealFile(page, 'server.go');
    const line = diffLine(item, 1);
    await expect(line).toContainText('package main');

    // Drag across the line's text, starting clear of the gutter "+" that
    // overlaps the line's left edge. Pierre pauses pointer events briefly
    // after a scroll, so retry until the drag produces a text selection.
    await expect(async () => {
      await line.scrollIntoViewIfNeeded({ timeout: 1000 });
      const box = await line.boundingBox();
      expect(box).toBeTruthy();
      await page.mouse.move(box!.x + 60, box!.y + box!.height / 2);
      await page.mouse.down();
      await page.mouse.move(box!.x + 200, box!.y + box!.height / 2, { steps: 5 });
      await page.mouse.up();
      expect(await page.evaluate(() => String(window.getSelection() || ''))).toContain('main');
    }).toPass({ timeout: 10_000 });

    // Selection alone should not open the form
    await expect(item.locator('.comment-form')).toHaveCount(0);

    await page.keyboard.press('c');

    const textarea = item.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();
    await textarea.fill('Code comment via selection');
    await textarea.press('Control+Enter');

    const comment = item.locator('.comment-card');
    await expect(comment).toBeVisible();
    await expect(comment).toContainText('Code comment via selection');

    // Anchored on the selected line of server.go
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=server.go')).json();
      return comments.map((c: { start_line: number; end_line: number; body: string }) => `${c.start_line}-${c.end_line}:${c.body}`);
    }).toEqual(['1-1:Code comment via selection']);
  });
});
