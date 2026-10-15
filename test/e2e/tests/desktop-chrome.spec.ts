import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection, hoverLine, openLineComment, switchToDocumentView } from './helpers';

// Desktop invariants — guards against mobile chrome work (F1) bleeding into
// the desktop layout. Runs in the git-mode project at default viewport. All
// read-only, so one page load covers them.
test.describe('Desktop chrome invariants', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('desktop layout keeps its sidebar, header controls, split diff and hover affordances', async ({ page }) => {
    await loadPage(page);

    // File-tree sidebar is visible; the mobile file picker bar exists in the
    // DOM but is display:none above the mobile breakpoint.
    await expect(page.locator('#fileTreePanel')).toBeVisible();
    await expect(page.locator('#mobileFilePickerBar')).toBeHidden();

    // The mobile breakpoint uses !important to hide these because JS sets
    // their inline display='' unconditionally. Guard against the !important
    // rule leaking to desktop viewport. Target #scopeToggle specifically: the
    // story Diff/Story toggle (#storyViewToggle) reuses the .scope-toggle pill
    // styling, so the bare class matches two elements.
    await expect(page.locator('#branchContext')).toBeVisible();
    await expect(page.locator('#diffModeToggle')).toBeVisible();
    await expect(page.locator('#scopeToggle')).toBeVisible();

    // F5 forces unified mode on mobile only; desktop must still default to split.
    const goSec = await goSection(page);
    await expect(goSec.locator('code[data-additions]')).toBeVisible();
    await expect(goSec.locator('code[data-deletions]')).toBeVisible();
    await expect(goSec.locator('code[data-unified]')).toHaveCount(0);

    // F6 hides .file-header-viewed on mobile only, and splits the path into
    // .dir / .filename spans (universal, not media-gated).
    await expect(page.locator('.file-header-viewed').first()).toBeVisible();
    await expect(page.locator('.pierre-file-header').first().locator('.file-header-name .filename')).toHaveCount(1);

    // F2 expands touch targets to 44x44 under @media (pointer: coarse); they
    // must not leak into pointer:fine and inflate desktop button sizes.
    const themeToggle = page.locator('.theme-toggle').first();
    await expect(themeToggle).toBeVisible();
    const box = await themeToggle.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.height).toBeLessThan(44);

    // F3/F4 must not break the desktop diff affordance: hovering a row shows
    // the gutter "+", and clicking it opens a form.
    const btn = await hoverLine(page, goSec, 1);
    await expect(btn).toBeVisible();
    const form = await openLineComment(page, goSec, 1);
    await expect(form).toBeVisible();

    // F3 adds a ::before "+" prefix to .line-num only under (pointer: coarse);
    // on desktop its content must be 'none'. .line-num lives in the rendered
    // markdown document (Pierre owns diff gutters).
    const doc = await switchToDocumentView(page);
    const lineNum = doc.locator('.line-num').first();
    await expect(lineNum).toBeAttached();
    expect(await lineNum.evaluate((el) => getComputedStyle(el, '::before').content)).toBe('none');
  });
});
