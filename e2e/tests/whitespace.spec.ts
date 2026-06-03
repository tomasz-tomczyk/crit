import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// Git-mode only: the "Ignore whitespace" toggle controls whether code diffs
// are re-fetched with `&w=1` (server collapses whitespace-only changes). We
// verify the toggle renders, fires a `&w=1` diff request when enabled, and
// persists (checked + cookie) across a reload. We deliberately avoid asserting
// on diff *content* — the fixed fixture has no reliable whitespace-only hunk,
// so a content assertion would be flaky.

test.describe('Ignore whitespace', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('settings panel shows Ignore whitespace toggle in git mode', async ({ page }) => {
    await loadPage(page);
    await page.click('#settingsToggle');
    const pane = page.locator('.settings-pane[data-pane="settings"]');
    await expect(
      pane.locator('.settings-display-label').filter({ hasText: 'Ignore whitespace' }),
    ).toBeVisible();
    await expect(pane.locator('#ignoreWhitespaceToggle')).toBeAttached();
  });

  test('enabling the toggle fires a diff fetch with &w=1', async ({ page }) => {
    await loadPage(page);
    await page.click('#settingsToggle');

    const toggle = page.locator('#ignoreWhitespaceToggle');
    await expect(toggle).not.toBeChecked();

    // Toggling triggers reloadForScope(), which re-fetches every code diff.
    // Those fetches must carry &w=1 now that the setting is on.
    const diffRequest = page.waitForRequest(
      (req) => req.url().includes('/api/file/diff') && req.url().includes('w=1'),
    );
    await toggle.check();
    await diffRequest;

    await expect(toggle).toBeChecked();
  });

  test('toggle persists (checked + cookie) across reload', async ({ page }) => {
    await loadPage(page);
    await page.click('#settingsToggle');

    const toggle = page.locator('#ignoreWhitespaceToggle');
    await toggle.check();
    await expect(toggle).toBeChecked();

    // Reload and re-open settings.
    await loadPage(page);
    await page.click('#settingsToggle');

    const toggleAfter = page.locator('#ignoreWhitespaceToggle');
    await expect(toggleAfter).toBeChecked();

    // Setting persisted in the consolidated crit-settings cookie (not localStorage).
    const stored = await page.evaluate(() => {
      const match = document.cookie.match(/(?:^|;\s*)crit-settings=([^;]+)/);
      if (!match) return null;
      try {
        return JSON.parse(decodeURIComponent(match[1])).ignoreWhitespace;
      } catch {
        return null;
      }
    });
    expect(stored).toBe(true);
  });
});
