import { test, expect } from '@playwright/test';
import { addComment, clearAllComments, getMdPath, loadPage, switchToDocumentView } from './helpers';

test.describe('Suggestion diff rendering', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('suggestion blocks render as inline diffs; regular code blocks do not', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    // Each comment on line 1 suggests a replacement for "# Authentication Plan".
    await addComment(request, mdPath, 1, 'Here is my suggestion:\n\n```suggestion\nreplacement line\n```\n\nPlease consider this change.');
    await addComment(request, mdPath, 1, '```suggestion\nnew content here\n```');
    await addComment(request, mdPath, 1, '```suggestion\n```');
    await addComment(request, mdPath, 1, '```javascript\nconsole.log("hello")\n```');
    await loadPage(page);
    const section = await switchToDocumentView(page);
    const card = (text: string) => section.locator('.comment-card').filter({ hasText: text });

    // Replacement: a "Suggested change" with the original line removed and
    // the suggestion added.
    const replacement = card('Please consider this change.').locator('.suggestion-diff');
    await expect(replacement).toBeVisible();
    await expect(replacement.locator('.suggestion-header')).toHaveText('Suggested change');
    await expect(replacement.locator('.suggestion-line-del')).toHaveCount(1);
    await expect(replacement.locator('.suggestion-line-add')).toHaveCount(1);
    await expect(replacement.locator('.suggestion-line-add .suggestion-line-content')).toHaveText('replacement line');

    // A suggestion-only body still renders its addition.
    await expect(card('new content here').locator('.suggestion-diff .suggestion-line-add')).toHaveCount(1);

    // An empty suggestion is a deletion only.
    const empty = section.locator('.comment-card').filter({ has: page.locator('.suggestion-diff') })
      .filter({ hasNotText: 'replacement line' }).filter({ hasNotText: 'new content here' });
    await expect(empty).toHaveCount(1);
    await expect(empty.locator('.suggestion-line-del')).toHaveCount(1);
    await expect(empty.locator('.suggestion-line-add')).toHaveCount(0);

    // A regular code block renders as code, not as a suggestion.
    const code = card('console.log');
    await expect(code.locator('.suggestion-diff')).toHaveCount(0);
    await expect(code.locator('.comment-body pre code')).toBeVisible();
    await expect(section.locator('.suggestion-diff')).toHaveCount(3);
  });
});
