import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, fileHeader } from './helpers';

// F1: mobile chrome layout — at ≤768px viewport, the file-tree and comments
// sidebars are hidden and a sticky <select> file picker takes the file-tree's
// place so multi-file reviews stay navigable on phones.
test.describe('Mobile chrome layout (F1)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('sidebars and secondary controls are hidden; the file picker lists every session file', async ({ page, request }) => {
    await expect(page.locator('#fileTreePanel')).toBeHidden();
    await expect(page.locator('#commentsPanel')).toBeHidden();
    // app.js init runs style.display = '' on these unconditionally; the
    // mobile breakpoint uses !important to win.
    await expect(page.locator('#branchContext')).toBeHidden();
    await expect(page.locator('#diffModeToggle')).toBeHidden();
    // #scopeToggle specifically: the story Diff/Story toggle (#storyViewToggle)
    // reuses the .scope-toggle pill styling, so the bare class matches two.
    await expect(page.locator('#scopeToggle')).toBeHidden();

    await expect(page.locator('#mobileFilePickerBar')).toBeVisible();
    const picker = page.locator('#mobileFilePicker');
    await expect(picker).toBeVisible();
    // renderFileTree fills the picker after the session loads.
    const session = await (await request.get('/api/session?scope=all')).json();
    await expect(picker.locator('option')).toHaveCount(session.files.length);
  });

  test('custom arrow shortcut does not hijack the mobile file picker', async ({ page }) => {
    const picker = page.locator('#mobileFilePicker');
    await expect.poll(() => picker.locator('option').count()).toBeGreaterThanOrEqual(2);

    await page.locator('#settingsToggle').click();
    await page.locator('.settings-tab[data-tab="shortcuts"]').click();
    await page.locator('[data-shortcut-id="next_block"]').click();
    await page.keyboard.press('ArrowDown');
    await page.locator('.settings-overlay').click({ position: { x: 10, y: 10 } });

    // A window bubble-phase listener runs after the app's document keydown
    // handler, so it sees whether that handler cancelled the key.
    await page.evaluate(() => {
      const w = window as typeof window & { __pickerDefaultPrevented?: boolean };
      w.__pickerDefaultPrevented = undefined;
      window.addEventListener('keydown', (event) => {
        if (event.target === document.querySelector('#mobileFilePicker')) {
          w.__pickerDefaultPrevented = event.defaultPrevented;
        }
      }, { once: true });
    });
    await picker.focus();
    await page.keyboard.press('ArrowDown');
    await expect.poll(() => page.evaluate(() =>
      (window as typeof window & { __pickerDefaultPrevented?: boolean }).__pickerDefaultPrevented,
    )).toBe(false);
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);
  });

  test('selecting a file scrolls that file section into view', async ({ page }) => {
    const picker = page.locator('#mobileFilePicker');
    await expect.poll(() => picker.locator('option').count()).toBeGreaterThanOrEqual(2);

    // Pick the LAST file in the list so it's most likely to be below the fold.
    const options = await picker.locator('option').allTextContents();
    const targetPath = options[options.length - 1];
    await picker.selectOption(targetPath);

    // The file's header should be on screen after the jump.
    await expect(fileHeader(page, targetPath)).toBeInViewport();
  });
});
