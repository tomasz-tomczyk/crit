import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView } from './helpers';

test.describe('Suggestion diff rendering', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('renders suggestion block as inline diff', async ({ page, request }) => {
    await loadPage(page);
    const section = mdSection(page);
    await switchToDocumentView(page);

    // Add a comment with a suggestion block on a known line range
    const gutter = section.locator('.line-comment-gutter').first();
    await gutter.hover();
    await gutter.locator('.line-add').click();

    const textarea = section.locator('.comment-form textarea');
    await textarea.fill('Here is my suggestion:\n\n```suggestion\nreplacement line\n```\n\nPlease consider this change.');
    await section.locator('.btn-primary').click();

    // Verify the suggestion diff is rendered
    const suggestionDiff = section.locator('.suggestion-diff');
    await expect(suggestionDiff).toBeVisible();

    // Should have a "Suggested change" header
    await expect(suggestionDiff.locator('.suggestion-header')).toHaveText('Suggested change');

    // Should have at least one deletion line (original) and one addition line (suggestion)
    await expect(suggestionDiff.locator('.suggestion-line-del')).toHaveCount(1);
    await expect(suggestionDiff.locator('.suggestion-line-add')).toHaveCount(1);

    // The addition line should contain our replacement text
    await expect(suggestionDiff.locator('.suggestion-line-add .suggestion-line-content')).toHaveText('replacement line');
  });

  test('renders suggestion without original lines as addition-only', async ({ page, request }) => {
    await loadPage(page);
    const section = mdSection(page);
    await switchToDocumentView(page);

    const gutter = section.locator('.line-comment-gutter').first();
    await gutter.hover();
    await gutter.locator('.line-add').click();

    const textarea = section.locator('.comment-form textarea');
    await textarea.fill('```suggestion\nnew content here\n```');
    await section.locator('.btn-primary').click();

    const suggestionDiff = section.locator('.suggestion-diff');
    await expect(suggestionDiff).toBeVisible();
    await expect(suggestionDiff.locator('.suggestion-line-add')).toHaveCount(1);
  });

  test('regular code blocks still render normally', async ({ page, request }) => {
    await loadPage(page);
    const section = mdSection(page);
    await switchToDocumentView(page);

    const gutter = section.locator('.line-comment-gutter').first();
    await gutter.hover();
    await gutter.locator('.line-add').click();

    const textarea = section.locator('.comment-form textarea');
    await textarea.fill('```javascript\nconsole.log("hello")\n```');
    await section.locator('.btn-primary').click();

    // Should NOT render as suggestion diff
    await expect(section.locator('.suggestion-diff')).toHaveCount(0);

    // Should render as a normal code block
    const codeBlock = section.locator('.comment-body pre code');
    await expect(codeBlock).toBeVisible();
  });

  test('empty suggestion renders as deletion-only', async ({ page, request }) => {
    await loadPage(page);
    const section = mdSection(page);
    await switchToDocumentView(page);

    const gutter = section.locator('.line-comment-gutter').first();
    await gutter.hover();
    await gutter.locator('.line-add').click();

    const textarea = section.locator('.comment-form textarea');
    await textarea.fill('```suggestion\n```');
    await section.locator('.btn-primary').click();

    // Should render as suggestion diff with deletion line(s) but no addition lines
    const suggestionDiff = section.locator('.suggestion-diff');
    await expect(suggestionDiff).toBeVisible();
    await expect(suggestionDiff.locator('.suggestion-line-del')).toHaveCount(1);
    await expect(suggestionDiff.locator('.suggestion-line-add')).toHaveCount(0);
  });
});
