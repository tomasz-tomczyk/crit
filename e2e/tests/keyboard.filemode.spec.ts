import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, clearFocus } from './helpers';

// ============================================================
// File-Mode-Specific Comment Shortcuts (e, d)
//
// These tests use file-mode-specific API seeding (handler.js path)
// and verify keyboard shortcuts work in file-mode's document view.
// Generic keyboard navigation tests live in keyboard.spec.ts.
// ============================================================
test.describe('Keyboard Comment Shortcuts — File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('e edits comment on focused block', async ({ page, request }) => {
    // Create a comment on line 1 of handler.js (first file alphabetically, so j lands here)
    await request.post(`/api/file/comments?path=handler.js`, {
      data: { start_line: 1, end_line: 1, body: 'Filemode edit test' },
    });

    await loadPage(page);
    const section = page.locator('.file-section').filter({ hasText: 'handler.js' });
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);

    // Verify comment exists
    await expect(section.locator('.comment-card')).toBeVisible();

    // Navigate to the first block (handler.js line 1)
    await page.keyboard.press('j');
    const focused = page.locator('.line-block.kb-nav.focused');
    await expect(focused).toHaveCount(1);

    // Check this block covers line 1
    const startLine = await focused.getAttribute('data-start-line');
    expect(parseInt(startLine!)).toBeLessThanOrEqual(1);

    await page.keyboard.press('e');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Filemode edit test');
  });

  test('d deletes comment on focused block', async ({ page, request }) => {
    // Create a comment on line 1 of handler.js (first file alphabetically)
    await request.post(`/api/file/comments?path=handler.js`, {
      data: { start_line: 1, end_line: 1, body: 'Filemode delete test' },
    });

    await loadPage(page);
    const section = page.locator('.file-section').filter({ hasText: 'handler.js' });
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);

    // Verify comment exists
    await expect(section.locator('.comment-card')).toBeVisible();

    // Navigate to the first block (handler.js line 1)
    await page.keyboard.press('j');
    await expect(page.locator('.line-block.kb-nav.focused')).toHaveCount(1);

    await page.keyboard.press('d');

    await expect(section.locator('.comment-card')).toHaveCount(0);
  });
});
