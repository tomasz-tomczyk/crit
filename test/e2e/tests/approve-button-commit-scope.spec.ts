import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, addComment } from './helpers';

// Regression: switching commit scope hid out-of-scope unresolved comments from the
// finish button label — it showed "Approve" while hidden_unresolved > 0.
test.describe('Approve Button Text — commit scope', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('shows Finish Review when unresolved comment is outside selected commit', async ({ page, request }) => {
    // config.yaml is untracked — visible on "All commits" but not when a single commit is selected.
    await addComment(request, 'config.yaml', 1, 'Fix config');

    await loadPage(page);
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');

    await page.click('#commitDropdownBtn');
    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await commitItem.click();
    await responsePromise;

    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
  });
});
