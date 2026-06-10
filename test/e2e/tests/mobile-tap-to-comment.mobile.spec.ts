import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection } from './helpers';

// F4: reliable single-tap comment opening.
// On touch, tapping the diff gutter (the area showing the line number +
// `+` prefix) opens a comment form for that line. No browser cancellation,
// no need to drag.
//
// IMPORTANT LIMITATION: Playwright's page.touchscreen.tap() synthesizes
// pointerdown → pointerup deterministically. It does NOT produce the
// pointercancel that real touch hardware emits when the browser's
// scroll/zoom gesture recognizer wins the touch sequence. These tests
// verify the JS event routing path, NOT real-hardware race prevention.
// The real-hardware fix is touch-action:none on .diff-gutter-num (in F3's
// CSS block); that rule MUST be present but is verified by the asserting
// the css property on .diff-gutter-num at mobile viewport. Long-press
// text-selection is browser-native and similarly not reproducible in
// Playwright — must be verified manually on real hardware at PR review.
test.describe('Mobile tap-to-comment (F4)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('tap on a diff gutter line number opens a comment form', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    // In unified mode (forced on mobile by F5), .diff-gutter-num is the
    // visible "tap me" affordance. Find a commentable addition line.
    const additionLine = section.locator('.diff-container.unified .diff-line.addition').first();
    await expect(additionLine).toBeAttached();
    await additionLine.scrollIntoViewIfNeeded();

    // The diff-gutter-num inside this line is what shows the `+` prefix
    // on mobile (per F3) and is what the user perceives as tappable.
    const gutter = additionLine.locator('.diff-gutter-num').first();
    await expect(gutter).toBeVisible();

    const box = await gutter.boundingBox();
    expect(box).not.toBeNull();
    await page.touchscreen.tap(box!.x + box!.width / 2, box!.y + box!.height / 2);

    // A comment form must appear within a reasonable time.
    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
  });

  test('tapping does not flash the blue + button on touch', async ({ page }) => {
    // The JS sets .drag-endpoint on the tapped gutter during the
    // pointerdown chain. A CSS rule makes .diff-comment-btn opacity:1
    // when its gutter has .drag-endpoint — that rule would flash the
    // blue button on touch between pointerdown and form-render.
    // The structural fix: .diff-comment-btn is display:none by default
    // and only display:flex inside @media (pointer: fine). On touch the
    // element never renders, regardless of the .drag-endpoint state.
    const section = goSection(page);
    const additionLine = section.locator('.diff-container.unified .diff-line.addition').first();
    await additionLine.scrollIntoViewIfNeeded();
    const gutter = additionLine.locator('.diff-gutter-num').first();
    const box = await gutter.boundingBox();
    expect(box).not.toBeNull();
    await page.touchscreen.tap(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await expect(page.locator('.comment-form')).toBeVisible();
    // Even with .drag-endpoint set on the gutter, the button must remain
    // display:none on touch.
    const btn = additionLine.locator('.diff-comment-btn').first();
    const display = await btn.evaluate((el) =>
      getComputedStyle(el).display
    );
    expect(display).toBe('none');
  });

  test('diff-gutter-num has touch-action:none on mobile (race-prevention)', async ({ page }) => {
    // The real-hardware reliability fix is touch-action:none on the tap
    // target. Playwright doesn't reproduce the pointercancel race, but it
    // CAN verify the CSS property is applied. If this regresses, real
    // hardware will start dropping taps even though the JS tests still
    // pass.
    const gutter = goSection(page).locator('.diff-gutter-num').first();
    await expect(gutter).toBeAttached();
    const touchAction = await gutter.evaluate((el) =>
      getComputedStyle(el).touchAction
    );
    expect(touchAction).toBe('none');
  });

  test('repeated taps reliably open the form (≥9/10)', async ({ page }) => {
    // The original reliability problem manifested as cancelled-tap races
    // (pointercancel before pointerup). This test taps the same gutter
    // line repeatedly, with form-cancel between each, and asserts at
    // least 9 of 10 attempts open the form.
    const section = goSection(page);
    await expect(section).toBeVisible();
    const additionLine = section.locator('.diff-container.unified .diff-line.addition').first();
    await additionLine.scrollIntoViewIfNeeded();
    const gutter = additionLine.locator('.diff-gutter-num').first();

    let successes = 0;
    for (let i = 0; i < 10; i++) {
      const box = await gutter.boundingBox();
      expect(box).not.toBeNull();
      await page.touchscreen.tap(box!.x + box!.width / 2, box!.y + box!.height / 2);
      // Poll briefly for the form. If it appears, count success and cancel.
      try {
        await page.locator('.comment-form').waitFor({ state: 'visible', timeout: 1500 });
        successes++;
        // Cancel to reset for the next iteration.
        const cancelBtn = page.locator('.comment-form button', { hasText: 'Cancel' }).first();
        if (await cancelBtn.isVisible()) await cancelBtn.click();
        await expect(page.locator('.comment-form')).toBeHidden({ timeout: 1500 });
      } catch {
        // tap didn't open the form
      }
    }
    expect(successes).toBeGreaterThanOrEqual(9);
  });
});
