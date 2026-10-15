import { test, expect, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, goSection, switchToDocumentView } from './helpers';

// F3: visible touch-only `+` affordance.
// On touch (pointer:coarse) there is no hover, so the hover "+" button can't
// be the cue. Instead every commentable line number shows a `+` prefix (a
// ::before pseudo-element) without any interaction.

// The ::before cue is visible: "+" content (a computed `none`/`""` means no
// pseudo-element) with non-zero opacity. Polled: Pierre's shadow styles and
// the document's line gutters can apply a frame after the element mounts.
async function expectPlusCue(lineNum: Locator) {
  await expect(lineNum).toBeVisible();
  await expect
    .poll(() => lineNum.evaluate((el) => getComputedStyle(el, '::before').content))
    .toContain('+');
  await expect
    .poll(() => lineNum.evaluate((el) => parseFloat(getComputedStyle(el, '::before').opacity)))
    .toBeGreaterThan(0);
}

test.describe('Mobile add-comment affordance (F3)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('diff gutter and document line numbers show a "+" prefix on touch', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    // The cue sits on the number itself; the cell's own ::before is Pierre's
    // change bar.
    await expectPlusCue(item.locator(
      'code[data-unified] [data-gutter] > [data-column-number][data-line-type="change-addition"] [data-line-number-content]',
    ).first());

    const doc = await switchToDocumentView(page);
    await expectPlusCue(doc.locator('.line-block:not(:has(.line-comment-gutter.diff-no-comment)) .line-num').first());
  });
});
