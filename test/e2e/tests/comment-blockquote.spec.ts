import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView } from './helpers';

test.describe('Comment blockquotes', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('renders markdown blockquotes and agent-style **> question** lines', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill([
      'Intro.',
      '',
      '> proper blockquote',
      '',
      '**> bold-wrapped quoted question?**',
      '',
      'Answer.',
    ].join('\n'));
    await page.locator('.comment-form .btn-primary').click();

    const blockquotes = section.locator('.comment-card .comment-body blockquote');
    await expect(blockquotes).toHaveCount(2);
    await expect(blockquotes.first()).toHaveText('proper blockquote');
    await expect(blockquotes.nth(1)).toHaveText('bold-wrapped quoted question?');
    await expect(section.locator('.comment-card .comment-body strong')).toHaveCount(0);
  });
});
