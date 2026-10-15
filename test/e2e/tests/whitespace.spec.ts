import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// Git-mode only: the "Ignore whitespace" toggle controls whether code diffs
// are re-fetched with `&w=1` (server collapses whitespace-only changes). We
// verify the toggle renders, fires a `&w=1` diff request when enabled, and
// persists (checked + cookie) across a reload. Diff content is covered by
// story.spec.ts "Story keeps raw hunk anchors while Diff honors Ignore whitespace".
//
// The toggle uses the `.comments-panel-switch` pattern: a visually-hidden
// <input opacity:0> behind a styled <span> track. Playwright's `.check()`
// hit-test lands on the track, not the input, so we click the wrapping
// <label> (exactly how a real user flips it) instead of checking the input.

// Clicks the switch label wrapping #ignoreWhitespaceToggle (toggles the input).
function switchLabel(page: Page) {
  return page.locator('label.comments-panel-switch', {
    has: page.locator('#ignoreWhitespaceToggle'),
  });
}

test.describe('Ignore whitespace', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('settings toggle re-fetches diffs with &w=1 and persists (checked + cookie) across reload', async ({ page }) => {
    await loadPage(page);
    await page.click('#settingsToggle');
    const pane = page.locator('.settings-pane[data-pane="settings"]');
    await expect(pane.locator('.settings-display-label').filter({ hasText: 'Ignore whitespace' })).toBeVisible();

    const toggle = page.locator('#ignoreWhitespaceToggle');
    await expect(toggle).toBeAttached();
    await expect(toggle).not.toBeChecked();

    // Toggling triggers reloadForScope(), which re-fetches every code diff.
    // Those fetches must carry &w=1 now that the setting is on.
    const diffRequest = page.waitForRequest(
      (req) => req.url().includes('/api/file/diff') && req.url().includes('w=1'),
    );
    await switchLabel(page).click();
    await diffRequest;
    await expect(toggle).toBeChecked();

    // Reload and re-open settings.
    await loadPage(page);
    await page.click('#settingsToggle');
    await expect(page.locator('#ignoreWhitespaceToggle')).toBeChecked();

    // Setting persisted in the consolidated crit-settings cookie (not localStorage).
    const cookie = (await page.context().cookies()).find(c => c.name === 'crit-settings');
    expect(JSON.parse(decodeURIComponent(cookie!.value)).ignoreWhitespace).toBe(true);
  });
});
