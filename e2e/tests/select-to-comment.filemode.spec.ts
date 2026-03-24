import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// File mode renders code files as document view (not diff), which is a unique
// rendering path not covered by git-mode tests.  Markdown document view tests
// are intentionally omitted — they duplicate git-mode coverage.

test.describe('Select-to-comment (file mode) — code document view', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Navigate to server.go via file tree
    await page.locator('.tree-file', { hasText: 'server.go' }).click();
  });

  test('selecting code text opens comment form', async ({ page }) => {
    const section = page.locator('#file-section-server\\.go');
    await expect(section).toBeVisible();
    const block = section.locator('.line-block', { hasText: 'package main' });
    await block.scrollIntoViewIfNeeded();
    await expect(block).toBeVisible();

    const blockBox = await block.boundingBox();
    expect(blockBox).toBeTruthy();
    if (!blockBox) return;

    await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
    await page.mouse.up();

    const textarea = section.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();
  });

  test('selecting code text and submitting comment', async ({ page }) => {
    const section = page.locator('#file-section-server\\.go');
    await expect(section).toBeVisible();
    const block = section.locator('.line-block', { hasText: 'respondJSON' }).first();
    await block.scrollIntoViewIfNeeded();
    await expect(block).toBeVisible();

    const blockBox = await block.boundingBox();
    expect(blockBox).toBeTruthy();
    if (!blockBox) return;

    await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
    await page.mouse.up();

    const textarea = section.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('Code comment via selection');
    await textarea.press('Control+Enter');

    const comment = section.locator('.comment-card');
    await expect(comment).toBeVisible();
    await expect(comment).toContainText('Code comment via selection');
  });
});
