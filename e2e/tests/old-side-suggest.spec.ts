import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection } from './helpers';

test.describe('Old-side suggest button', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('suggest on old-side deletion line inserts old content', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // server.go split diff: find a deletion line on the left side
    const deletionSide = section.locator('.diff-split-side.deletion[data-diff-line-num]').first();
    await deletionSide.scrollIntoViewIfNeeded();
    await deletionSide.hover();
    await deletionSide.locator('.diff-comment-btn').click();

    // Click the suggest button
    const suggestBtn = page.locator('.comment-form .btn', { hasText: '± Suggest' });
    await suggestBtn.click();

    // The textarea should contain a suggestion block with OLD file content, not new
    const textarea = page.locator('.comment-form textarea');
    const value = await textarea.inputValue();
    expect(value).toContain('```suggestion');
    expect(value).toContain('```');

    // The deletion line's text content should appear in the suggestion
    const diffContent = await deletionSide.locator('.diff-content').textContent();
    expect(value).toContain(diffContent?.trim() || '');
  });

  test('suggest on new-side addition line still inserts new content', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // server.go split diff: find an addition line on the right side
    const additionSide = section.locator('.diff-split-side.addition[data-diff-line-num]').first();
    await additionSide.scrollIntoViewIfNeeded();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();

    // Click suggest
    const suggestBtn = page.locator('.comment-form .btn', { hasText: '± Suggest' });
    await suggestBtn.click();

    const textarea = page.locator('.comment-form textarea');
    const value = await textarea.inputValue();
    expect(value).toContain('```suggestion');

    // The addition line content should appear in the suggestion
    const diffContent = await additionSide.locator('.diff-content').textContent();
    expect(value).toContain(diffContent?.trim() || '');
  });
});
