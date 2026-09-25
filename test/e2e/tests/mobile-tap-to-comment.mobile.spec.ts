import { test, expect, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, goSection, tapCenter } from './helpers';

// F4: reliable single-tap comment opening.
// On touch, tapping the diff gutter (the line number of a commentable line)
// opens a comment form for that line. No browser cancellation, no need to
// drag, no hover step.
//
// IMPORTANT LIMITATION: Playwright's page.touchscreen.tap() synthesizes
// pointerdown → pointerup deterministically. It does NOT produce the
// pointercancel that real touch hardware emits when the browser's
// scroll/zoom gesture recognizer wins the touch sequence. These tests
// verify the JS event routing path, NOT real-hardware race prevention.
// The real-hardware fix is touch-action:none on the gutter tap target;
// that rule is verified by asserting the CSS property at mobile viewport.
// Long-press text-selection is browser-native and similarly not
// reproducible in Playwright — must be verified manually on real hardware.

// Line-number cell of an added line (mobile renders unified diffs).
function additionGutter(item: Locator, nth = 0): Locator {
  return item.locator(
    'code[data-unified] [data-gutter] > [data-column-number][data-line-type="change-addition"]',
  ).nth(nth);
}

test.describe('Mobile tap-to-comment (F4)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('tap on a diff gutter line number opens a comment form', async ({ page }) => {
    const section = await goSection(page);
    const gutter = additionGutter(section);
    await expect(gutter).toBeVisible();

    await tapCenter(page, gutter);

    // A single tap must open the form — no second tap on a hover "+".
    await expect(page.locator('#filesContainer .comment-form textarea')).toBeVisible();
  });

  test('diff gutter line number has touch-action:none on mobile (race-prevention)', async ({ page }) => {
    // The real-hardware reliability fix is touch-action:none on the tap
    // target. Playwright doesn't reproduce the pointercancel race, but it
    // CAN verify the CSS property is applied. If this regresses, real
    // hardware will start dropping taps even though the JS tests still pass.
    const gutter = additionGutter(await goSection(page));
    await expect(gutter).toBeVisible();
    await expect(gutter).toHaveCSS('touch-action', 'none');
  });

  test('repeated taps reliably open the form (≥9/10)', async ({ page }) => {
    // The original reliability problem manifested as cancelled-tap races
    // (pointercancel before pointerup). Tap gutter lines repeatedly,
    // cancelling the form between attempts, and require ≥9 of 10 to open it.
    // Alternate between two lines so no attempt rides on hover state left
    // over from the previous tap on the same row.
    const section = await goSection(page);
    const gutters = [additionGutter(section, 0), additionGutter(section, 3)];
    await expect(gutters[1]).toBeVisible();
    const form = page.locator('#filesContainer .comment-form');

    let successes = 0;
    for (let i = 0; i < 10; i++) {
      await tapCenter(page, gutters[i % 2]);
      try {
        await form.locator('textarea').waitFor({ state: 'visible', timeout: 1500 });
        successes++;
        // Close via the form's Escape shortcut: a mouse click on Cancel
        // would first scroll, and Pierre ignores the pointer right after a
        // scroll. (Tapping Cancel works; mobile-comment-composer covers it.)
        await form.locator('textarea').press('Escape');
        await expect(form).toHaveCount(0, { timeout: 1500 });
      } catch {
        // tap didn't open the form
      }
    }
    expect(successes).toBeGreaterThanOrEqual(9);
  });
});
