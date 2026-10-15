import { test, expect, type Locator, type Page } from '@playwright/test';
import { clearAllComments, loadPage, goSection, diffLine, openLineComment, type DiffSide } from './helpers';

// First changed line of the given type on one side, as { line, text }.
async function firstChangedLine(item: Locator, side: DiffSide): Promise<{ line: number; text: string }> {
  const code = side === 'old' ? 'code[data-deletions]' : 'code[data-additions]';
  const type = side === 'old' ? 'change-deletion' : 'change-addition';
  const row = item.locator(`${code} [data-content] > [data-line-type="${type}"]`).first();
  await expect(row).toBeVisible();
  const line = Number(await row.getAttribute('data-line'));
  expect(line).toBeGreaterThan(0);
  const text = (await diffLine(item, line, side).first().textContent()) ?? '';
  expect(text.trim()).not.toBe('');
  return { line, text: text.replace(/\n$/, '') };
}

async function suggestOn(page: Page, item: Locator, line: number, side: DiffSide): Promise<Locator> {
  const form = await openLineComment(page, item, line, side);
  await form.locator('.btn', { hasText: '± Suggest' }).click();
  return form.locator('textarea');
}

test.describe('Old-side suggest button', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('Suggest inserts the old content on a deletion line and the new content on an addition line', async ({ page }) => {
    await loadPage(page);
    const section = await goSection(page);

    const old = await firstChangedLine(section, 'old');
    // The same line number on the new side holds different text, so an
    // old-side suggestion that reads the new file is caught.
    const newText = (await diffLine(section, old.line, 'new').first().textContent()) ?? '';
    expect(newText.trim()).not.toBe(old.text.trim());
    const oldTextarea = await suggestOn(page, section, old.line, 'old');
    await expect.poll(() => oldTextarea.inputValue()).toBe('```suggestion\n' + old.text + '\n```');
    // Close it so the next form is the only one open.
    await section.locator('.comment-form').getByRole('button', { name: 'Cancel' }).click();
    await expect(section.locator('.comment-form')).toHaveCount(0);

    const added = await firstChangedLine(section, 'new');
    const newTextarea = await suggestOn(page, section, added.line, 'new');
    await expect.poll(() => newTextarea.inputValue()).toBe('```suggestion\n' + added.text + '\n```');
  });
});
