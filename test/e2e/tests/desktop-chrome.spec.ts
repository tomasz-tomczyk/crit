import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

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
    // .scope-toggle only shows in git mode with commits — the git-mode fixture
    // satisfies that; if it's hidden on desktop something else went wrong.
    await expect(page.locator('.scope-toggle')).toBeVisible();
  });

  test('diff defaults to split on desktop', async ({ page }) => {
    // F5 forces unified mode on mobile only; desktop must still default to split.
    const goSec = page.locator('#file-section-server\\.go');
    await expect(goSec).toBeVisible();
    await expect(goSec.locator('.diff-container.split')).toBeVisible();
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
    const fileSection = page.locator('.file-section').first();
    await expect(fileSection.locator('.file-header-name .filename')).toHaveCount(1);
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
    // F3 adds the ::before "+" prefix only under @media (pointer: coarse).
    // On desktop the existing .line-add blue button on hover is the
    // affordance, NOT the ::before. The pseudo-element's content must
    // be 'none' (the unset default) so nothing renders.
    const lineNum = page.locator('.diff-gutter-num').first();
    await expect(lineNum).toBeAttached();
    const content = await lineNum.evaluate((el) =>
      getComputedStyle(el, '::before').content
    );
    expect(content).toBe('none');
  });

  test('desktop diff blue + button appears on row hover', async ({ page }) => {
    // F3 must not break the existing desktop diff affordance:
    // .diff-comment-btn becomes visible when its parent diff line / split
    // side is hovered.
    const splitSide = page.locator('#file-section-server\\.go .diff-split-side.addition').first();
    await expect(splitSide).toBeAttached();
    await splitSide.scrollIntoViewIfNeeded();
    await splitSide.hover();
    const btn = splitSide.locator('.diff-comment-btn');
    await expect(btn).toBeVisible();
  });

  test('desktop click on diff + button opens comment form', async ({ page }) => {
    // F4 must not break the existing desktop click path.
    const splitSide = page.locator('#file-section-server\\.go .diff-split-side.addition').first();
    await splitSide.scrollIntoViewIfNeeded();
    await splitSide.hover();
    const btn = splitSide.locator('.diff-comment-btn');
    await expect(btn).toBeVisible();
    await btn.click();
    await expect(page.locator('.comment-form')).toBeVisible();
  });
});
