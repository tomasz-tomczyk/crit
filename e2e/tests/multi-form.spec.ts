import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection, jsSection, mdSection, switchToDocumentView } from './helpers';

test.describe('Multi-Form Comments', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('opening a new comment form does not close existing form', async ({ page }) => {
    // Open form on server.go diff
    const goSec = goSection(page);
    const goAddition = goSec.locator('.diff-split-side.addition').first();
    await goAddition.hover();
    await goAddition.locator('.diff-comment-btn').click();

    const firstForm = goSec.locator('.comment-form');
    await expect(firstForm).toBeVisible();

    // Type text in first form
    const firstTextarea = firstForm.locator('textarea');
    await firstTextarea.fill('Comment on server.go');

    // Open form on handler.js diff
    const jsSec = jsSection(page);
    const jsAddition = jsSec.locator('.diff-split-side.addition').first();
    await jsAddition.scrollIntoViewIfNeeded();
    await jsAddition.hover();
    await jsAddition.locator('.diff-comment-btn').click();

    const secondForm = jsSec.locator('.comment-form');
    await expect(secondForm).toBeVisible();

    // Verify both forms visible
    await expect(page.locator('.comment-form')).toHaveCount(2);

    // First form retains text
    await expect(firstForm.locator('textarea')).toHaveValue('Comment on server.go');

    // Second form textarea is focused
    await expect(secondForm.locator('textarea')).toBeFocused();
  });

  test('submitting one form does not affect other open forms', async ({ page }) => {
    // Open form on server.go
    const goSec = goSection(page);
    const goAddition = goSec.locator('.diff-split-side.addition').first();
    await goAddition.hover();
    await goAddition.locator('.diff-comment-btn').click();
    const firstForm = goSec.locator('.comment-form');
    await firstForm.locator('textarea').fill('Keep this open');

    // Open form on handler.js
    const jsSec = jsSection(page);
    const jsAddition = jsSec.locator('.diff-split-side.addition').first();
    await jsAddition.scrollIntoViewIfNeeded();
    await jsAddition.hover();
    await jsAddition.locator('.diff-comment-btn').click();
    const secondForm = jsSec.locator('.comment-form');
    await secondForm.locator('textarea').fill('Submit this one');

    // Submit second form
    await secondForm.locator('.btn-primary').click();

    // Second becomes a comment card
    await expect(jsSec.locator('.comment-card')).toBeVisible();
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    // First form still open with text
    await expect(goSec.locator('.comment-form')).toBeVisible();
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Keep this open');
  });

  test('cancelling one form does not affect other open forms', async ({ page }) => {
    // Open form on server.go
    const goSec = goSection(page);
    const goAddition = goSec.locator('.diff-split-side.addition').first();
    await goAddition.hover();
    await goAddition.locator('.diff-comment-btn').click();
    const firstForm = goSec.locator('.comment-form');
    await firstForm.locator('textarea').fill('Keep this open');

    // Open form on handler.js
    const jsSec = jsSection(page);
    const jsAddition = jsSec.locator('.diff-split-side.addition').first();
    await jsAddition.scrollIntoViewIfNeeded();
    await jsAddition.hover();
    await jsAddition.locator('.diff-comment-btn').click();
    const secondForm = jsSec.locator('.comment-form');
    await expect(secondForm).toBeVisible();

    // Cancel second form
    await secondForm.getByRole('button', { name: 'Cancel' }).click();

    // Second form gone
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    // First still open with text
    await expect(goSec.locator('.comment-form')).toBeVisible();
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Keep this open');
  });

  test('Escape in textarea cancels only that form', async ({ page }) => {
    // Open form on server.go
    const goSec = goSection(page);
    const goAddition = goSec.locator('.diff-split-side.addition').first();
    await goAddition.hover();
    await goAddition.locator('.diff-comment-btn').click();
    const firstForm = goSec.locator('.comment-form');
    await firstForm.locator('textarea').fill('Keep this open');

    // Open form on handler.js
    const jsSec = jsSection(page);
    const jsAddition = jsSec.locator('.diff-split-side.addition').first();
    await jsAddition.scrollIntoViewIfNeeded();
    await jsAddition.hover();
    await jsAddition.locator('.diff-comment-btn').click();
    const secondForm = jsSec.locator('.comment-form');
    await expect(secondForm).toBeVisible();

    // Press Escape in second form's textarea
    await secondForm.locator('textarea').press('Escape');

    // Second form gone
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    // First still open with text
    await expect(goSec.locator('.comment-form')).toBeVisible();
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Keep this open');
  });

  test('Ctrl+Enter in textarea submits only that form', async ({ page }) => {
    // Open form on server.go
    const goSec = goSection(page);
    const goAddition = goSec.locator('.diff-split-side.addition').first();
    await goAddition.hover();
    await goAddition.locator('.diff-comment-btn').click();
    const firstForm = goSec.locator('.comment-form');
    await firstForm.locator('textarea').fill('Keep this open');

    // Open form on handler.js
    const jsSec = jsSection(page);
    const jsAddition = jsSec.locator('.diff-split-side.addition').first();
    await jsAddition.scrollIntoViewIfNeeded();
    await jsAddition.hover();
    await jsAddition.locator('.diff-comment-btn').click();
    const secondForm = jsSec.locator('.comment-form');
    await secondForm.locator('textarea').fill('Submit via shortcut');

    // Ctrl+Enter in second form
    await secondForm.locator('textarea').press('Control+Enter');

    // Second becomes a comment card
    await expect(jsSec.locator('.comment-card')).toBeVisible();
    await expect(jsSec.locator('.comment-form')).toHaveCount(0);

    // First form still open with text
    await expect(goSec.locator('.comment-form')).toBeVisible();
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Keep this open');
  });

  test('clicking same gutter line twice does not duplicate form', async ({ page }) => {
    const goSec = goSection(page);
    const goAddition = goSec.locator('.diff-split-side.addition').first();

    // Open form
    await goAddition.hover();
    await goAddition.locator('.diff-comment-btn').click();
    const form = goSec.locator('.comment-form');
    await expect(form).toBeVisible();

    // Type text
    await form.locator('textarea').fill('Some text');

    // Click same gutter again
    await goAddition.hover();
    await goAddition.locator('.diff-comment-btn').click();

    // Still only one form, text preserved
    await expect(goSec.locator('.comment-form')).toHaveCount(1);
    await expect(goSec.locator('.comment-form textarea')).toHaveValue('Some text');
  });

  test('multiple forms on same file at different lines', async ({ page }) => {
    // Switch to document view for markdown file
    await switchToDocumentView(page);

    const section = mdSection(page);

    // Open form on first gutter
    const firstLineBlock = section.locator('.line-block').first();
    await firstLineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const firstForm = section.locator('.comment-form').first();
    await expect(firstForm).toBeVisible();
    await firstForm.locator('textarea').fill('First line comment');

    // Open form on a different gutter (use nth to get a different line)
    const thirdLineBlock = section.locator('.line-block').nth(2);
    await thirdLineBlock.hover();
    await section.locator('.line-comment-gutter').nth(2).click();

    // Verify two forms exist
    await expect(section.locator('.comment-form')).toHaveCount(2);
  });
});
