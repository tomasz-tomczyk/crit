import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, revealFile, diffLine, selectInLine } from './helpers';

// File mode renders code files as whole-file Pierre items (no diff): the
// selection toolbar resolves those selections like `c` does.

test.describe('Selection toolbar (file mode) — code file view', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('Comment on a code selection comments on its line with the quote', async ({ page, request }) => {
    const item = await revealFile(page, 'server.go');
    await expect(diffLine(item, 1)).toContainText('package main');
    const selected = await selectInLine(page, item, 1);

    const bar = page.locator('.crit-selection-toolbar');
    await expect(bar).toBeVisible();
    await expect(item.locator('.comment-form')).toHaveCount(0);
    await bar.getByRole('button', { name: 'Comment on selection' }).click();

    const textarea = item.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
    await expect(bar).toBeHidden();
    await textarea.fill('Code comment via toolbar');
    await textarea.press('Control+Enter');
    await expect(item.locator('.comment-card', { hasText: 'Code comment via toolbar' })).toBeVisible();

    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=server.go')).json();
      return comments.map((c: { start_line: number; end_line: number; body: string; quote?: string }) =>
        `${c.start_line}-${c.end_line}:${c.body}:${c.quote}`);
    }).toEqual([`1-1:Code comment via toolbar:${selected.trim()}`]);
  });
});
