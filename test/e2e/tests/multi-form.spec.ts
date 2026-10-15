import { test, expect, type Page, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, goSection, jsSection, switchToDocumentView, openLineComment, hoverLine, diffLine, selectedRows } from './helpers';

// server.go line 5 (the added "log" import) and handler.js line 1 are
// additions. The two files are far enough apart that CodeView never mounts
// both at once, so each is brought back into view before it is checked.
async function openGoForm(page: Page, line = 5): Promise<Locator> {
  const goSec = await goSection(page);
  await openLineComment(page, goSec, line);
  return goSec.locator(`.comment-form[data-form-key="server.go:${line}:${line}:"]`);
}

async function openJsForm(page: Page): Promise<Locator> {
  const jsSec = await jsSection(page);
  await openLineComment(page, jsSec, 1);
  return jsSec.locator('.comment-form[data-form-key="handler.js:1:1:"]');
}

test.describe('Multi-Form Comments', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('opening a new comment form does not close existing form', async ({ page }) => {
    // Open form on server.go diff and type in it
    const firstForm = await openGoForm(page);
    await firstForm.locator('textarea').fill('Comment on server.go');

    // Open form on handler.js diff
    const secondForm = await openJsForm(page);
    // Second form textarea is focused
    await expect(secondForm.locator('textarea')).toBeFocused();

    // First form is still open on server.go and retains its text
    const goSec = await goSection(page);
    await expect(goSec.locator('.comment-form')).toHaveCount(1);
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Comment on server.go');
    const jsSec = await jsSection(page);
    await expect(jsSec.locator('.comment-form')).toHaveCount(1);
  });

  test('opening a new comment form closes existing empty form', async ({ page }) => {
    // Open form on server.go (leave empty)
    await openGoForm(page);

    // Open form on handler.js without filling first
    const jsSec = await jsSection(page);
    await openLineComment(page, jsSec, 1);

    // First (empty) form should be closed; only second remains
    await expect(jsSec.locator('.comment-form')).toHaveCount(1);
    const goSec = await goSection(page);
    await expect(goSec.locator('.pierre-file-header')).toBeVisible();
    await expect(goSec.locator('.comment-form')).toHaveCount(0);
  });

  test('clicking same gutter line twice does not duplicate form', async ({ page }) => {
    const goSec = await goSection(page);
    const form = await openLineComment(page, goSec, 5);
    await form.locator('textarea').fill('Some text');

    // Click the same line's gutter "+" again
    const button = await hoverLine(page, goSec, 5);
    const box = await button.boundingBox();
    expect(box).toBeTruthy();
    await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2);

    // Still only one form, text preserved and focused
    await expect(goSec.locator('.comment-form')).toHaveCount(1);
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Some text');
    await expect(goSec.locator('.comment-form textarea')).toBeFocused();
  });

  test('submitting one form (button or Ctrl+Enter) does not affect other open forms', async ({ page }) => {
    const firstForm = await openGoForm(page);
    await firstForm.locator('textarea').fill('Keep this open');

    const secondForm = await openJsForm(page);
    await secondForm.locator('textarea').fill('Submit this one');
    await secondForm.locator('.btn-primary').click();

    // Second becomes a comment card
    const jsSec = await jsSection(page);
    await expect(jsSec.locator('.comment-card')).toBeVisible();
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    // Ctrl+Enter submits only the form it is typed in.
    const thirdForm = await openJsForm(page);
    await thirdForm.locator('textarea').fill('Submit via shortcut');
    await thirdForm.locator('textarea').press('Control+Enter');
    await expect(jsSec.locator('.comment-card')).toHaveCount(2);
    await expect(jsSec.locator('.comment-card .comment-body').last()).toContainText('Submit via shortcut');
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    // First form still open with text, and nothing submitted on server.go
    const goSec = await goSection(page);
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Keep this open');
    await expect(goSec.locator('.comment-card')).toHaveCount(0);
  });

  test('Cancel and Escape close only their own form', async ({ page }) => {
    const firstForm = await openGoForm(page);
    await firstForm.locator('textarea').fill('Keep this open');

    const secondForm = await openJsForm(page);
    await secondForm.getByRole('button', { name: 'Cancel' }).click();
    const jsSec = await jsSection(page);
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    const thirdForm = await openJsForm(page);
    await thirdForm.locator('textarea').press('Escape');
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    const goSec = await goSection(page);
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Keep this open');
  });

  test('two forms on different lines of one document: the first keeps its text and range highlight', async ({ page }) => {
    const section = await switchToDocumentView(page);

    // Open form on the first block and fill it, so it isn't auto-closed.
    const firstLineBlock = section.locator('.line-block').first();
    await firstLineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await expect(section.locator('.comment-form')).toHaveCount(1);
    await section.locator('.comment-form textarea').fill('First line comment');

    // Open a second form on a different line.
    const thirdLineBlock = section.locator('.line-block').nth(2);
    await thirdLineBlock.hover();
    await section.locator('.line-comment-gutter').nth(2).click();

    await expect(section.locator('.comment-form')).toHaveCount(2);
    await expect(section.locator('.comment-form textarea').first()).toHaveValue('First line comment');
    // First block carries form-selected (its range is covered by an open
    // form); the third carries selected (it is the current selection).
    await expect(firstLineBlock).toHaveClass(/form-selected/);
    await expect(thirdLineBlock).toHaveClass(/selected/);
  });

  test('second form on another line of the same file keeps the first open (split diff)', async ({ page }) => {
    const goSec = await goSection(page);
    // Two distinct commentable addition lines in the same hunk.
    await expect(diffLine(goSec, 5)).toHaveAttribute('data-line-type', 'change-addition');
    await expect(diffLine(goSec, 7)).toHaveAttribute('data-line-type', 'change-addition');

    const first = await openLineComment(page, goSec, 5);
    await expect(goSec.locator('.comment-form')).toHaveCount(1);
    // Fill so it isn't auto-closed when the second form opens
    await first.locator('textarea').fill('first');

    await openLineComment(page, goSec, 7);
    await expect(goSec.locator('.comment-form')).toHaveCount(2);
    await expect(goSec.locator('.comment-form-header')).toHaveText(['Comment on Line 5', 'Comment on Line 7']);
    await expect(goSec.locator('.comment-form textarea').first()).toHaveValue('first');
    await expect(goSec.locator('.comment-form textarea').nth(1)).toBeFocused();
    // The first form's range stays tinted after the second opens.
    await expect.poll(() => selectedRows(goSec)).toEqual(['5:change-addition', '7:change-addition']);
  });
});
