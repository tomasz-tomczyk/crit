import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection, hoverLine, openLineComment, switchToDocumentView } from './helpers';

// Desktop invariants — guards against mobile chrome work (F1) bleeding into
// the desktop layout. Runs in the git-mode project at default viewport.
test.describe('Desktop chrome invariants', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('file-tree sidebar is visible on desktop', async ({ page }) => {
    const fileTree = page.locator('#fileTreePanel');
    await expect(fileTree).toBeVisible();
  });

  test('mobile file picker bar is not visible on desktop', async ({ page }) => {
    // The bar element exists in the DOM but should be display:none above the
    // mobile breakpoint.
    const pickerBar = page.locator('#mobileFilePickerBar');
    await expect(pickerBar).toBeHidden();
  });

  test('secondary header controls remain visible on desktop', async ({ page }) => {
    // The mobile breakpoint uses !important to hide these because JS sets
    // their inline display='' unconditionally. Guard against the !important
    // rule leaking to desktop viewport.
    await expect(page.locator('#branchContext')).toBeVisible();
    await expect(page.locator('#diffModeToggle')).toBeVisible();
    // The commit-scope toggle only shows in git mode with commits — the
    // git-mode fixture satisfies that; if it's hidden on desktop something else
    // went wrong. Target #scopeToggle specifically: the story Diff/Story toggle
    // (#storyViewToggle) reuses the .scope-toggle pill styling, so the bare
    // class matches two elements.
    await expect(page.locator('#scopeToggle')).toBeVisible();
  });

  test('diff defaults to split on desktop', async ({ page }) => {
    // F5 forces unified mode on mobile only; desktop must still default to split.
    const goSec = await goSection(page);
    await expect(goSec.locator('code[data-additions]')).toBeVisible();
    await expect(goSec.locator('code[data-deletions]')).toBeVisible();
    await expect(goSec.locator('code[data-unified]')).toHaveCount(0);
  });

  test('file-header-viewed checkbox remains visible on desktop', async ({ page }) => {
    // F6 hides .file-header-viewed on mobile only.
    const viewed = page.locator('.file-header-viewed').first();
    await expect(viewed).toBeVisible();
  });

  test('filename is wrapped in a .filename span for independent truncation', async ({ page }) => {
    // F6 splits the file path into <span class='dir'> and <span class='filename'>
    // so they can shrink independently with ellipsis. The rule is universal
    // (not media-gated). Asserting on desktop guards against the JS template
    // regressing the markup.
    const fileHeader = page.locator('.pierre-file-header').first();
    await expect(fileHeader.locator('.file-header-name .filename')).toHaveCount(1);
  });

  test('header icon buttons stay compact on desktop', async ({ page }) => {
    // F2 expands touch targets to 44x44 under @media (pointer: coarse).
    // Guard against those rules leaking into pointer:fine and inflating
    // desktop button sizes. The exact upper bound matches the pre-F2
    // computed size of .theme-toggle on main (small icon button).
    const themeToggle = page.locator('.theme-toggle').first();
    await expect(themeToggle).toBeVisible();
    const box = await themeToggle.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.height).toBeLessThan(44);
  });

  test('line-num ::before "+" prefix is not rendered on desktop', async ({ page }) => {
    // F3 adds the ::before "+" prefix to .line-num only under
    // @media (pointer: coarse). On desktop the hover "+" button is the
    // affordance, NOT the ::before. The pseudo-element's content must
    // be 'none' (the unset default) so nothing renders. .line-num lives in
    // the rendered markdown document (Pierre owns diff gutters).
    const doc = await switchToDocumentView(page);
    const lineNum = doc.locator('.line-num').first();
    await expect(lineNum).toBeAttached();
    const content = await lineNum.evaluate((el) =>
      getComputedStyle(el, '::before').content
    );
    expect(content).toBe('none');
  });

  test('desktop diff blue + button appears on row hover', async ({ page }) => {
    // F3 must not break the desktop diff affordance: the gutter "+"
    // becomes visible when a diff line is hovered.
    const item = await goSection(page);
    const btn = await hoverLine(page, item, 1);
    await expect(btn).toBeVisible();
  });

  test('desktop click on diff + button opens comment form', async ({ page }) => {
    // F4 must not break the desktop click path.
    const item = await goSection(page);
    const form = await openLineComment(page, item, 1);
    await expect(form).toBeVisible();
  });
});
