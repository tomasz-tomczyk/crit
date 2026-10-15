import { test, expect, type Page } from '@playwright/test';
import { addComment, clearAllComments, fileHeader, loadPage, realCommitItems, switchScope } from './helpers';

async function openCommitPicker(page: Page) {
  await page.click('#commitDropdownBtn');
  await expect(page.locator('#commitDropdown')).toHaveClass(/open/);
}

// Click a picker row (optionally with Alt) and wait for the session refetch.
async function pick(page: Page, row: ReturnType<Page['locator']>, alt = false) {
  const response = page.waitForResponse(r => r.url().includes('/api/session') && r.status() === 200);
  await row.click(alt ? { modifiers: ['Alt'] } : undefined);
  await response;
}

const allItem = (page: Page) => page.locator('.commit-picker-item[data-commit=""]');
const label = (page: Page) => page.locator('#commitDropdownLabel');

test.describe('Commit Selection', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('picker shows "All commits", lists commits, and closes on Escape or outside click', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#commitDropdown')).toBeVisible();
    await expect(label(page)).toHaveText('All commits');

    await openCommitPicker(page);
    await expect(allItem(page)).toBeVisible();
    await expect(allItem(page)).toHaveClass(/active/);
    const firstCommit = realCommitItems(page).first();
    await expect(firstCommit).toBeVisible();
    await expect(firstCommit.locator('.commit-picker-item-sha')).toBeVisible();
    await expect(firstCommit.locator('.commit-picker-item-msg')).toBeVisible();
    await expect(firstCommit.locator('.commit-picker-item-msg')).toContainText('add auth');

    await page.keyboard.press('Escape');
    await expect(page.locator('#commitDropdown')).not.toHaveClass(/open/);

    await openCommitPicker(page);
    await page.click('.main-content');
    await expect(page.locator('#commitDropdown')).not.toHaveClass(/open/);
  });

  test('from and through pins filter the review and "All commits" clears them', async ({ page }) => {
    await loadPage(page);
    await openCommitPicker(page);
    const commits = realCommitItems(page);
    await expect(commits).toHaveCount(2);

    // Clicking a commit sets the from pin and filters files; the dropdown stays open.
    await pick(page, commits.first());
    await expect(page.locator('#commitDropdown')).toHaveClass(/open/);
    await expect(label(page)).toContainText('only');
    await expect(commits.first()).toHaveClass(/is-from/);
    await expect(allItem(page)).not.toHaveClass(/active/);
    // Auth commit touches six files (skill.md included, branch fixture);
    // handler.js (other commit) and the uncommitted files drop out.
    await expect(page.locator('.tree-file')).toHaveCount(6);
    await expect(page.locator('.tree-file[data-tree-path="skill.md"]')).toHaveCount(1);
    await expect(page.locator('.tree-file[data-tree-path="handler.js"]')).toHaveCount(0);
    // The diff pane re-renders with the filtered list
    await expect(fileHeader(page, 'deleted.txt')).toBeVisible();
    await expect(fileHeader(page, 'config.yaml')).toHaveCount(0);

    // "All commits" restores the full view.
    await pick(page, allItem(page));
    await expect(label(page)).toHaveText('All commits');

    // List is newest-first: older commit as from, Alt+click the newer as
    // through — a range request, and the dropdown stays open across both.
    await pick(page, commits.nth(1));
    const rangeRequest = page.waitForRequest(
      r => r.url().includes('/api/session') && /[?&]commit=[^&]*\.\.[^&]*/.test(r.url())
    );
    await pick(page, commits.nth(0), true);
    await rangeRequest;
    await expect(page.locator('#commitDropdown')).toHaveClass(/open/);
    await expect(label(page)).toContainText('→');
    await expect(commits.nth(1)).toHaveClass(/is-from/);
    await expect(commits.nth(0)).toHaveClass(/is-through/);

    // Alt+click the through pin again clears it.
    await pick(page, commits.nth(0), true);
    await expect(label(page)).toContainText('only');
    await expect(page.locator('#commitDropdownList .commit-picker-item.is-through')).toHaveCount(0);

    // "All commits" clears both pins.
    await pick(page, commits.nth(0), true);
    await expect(label(page)).toContainText('→');
    await pick(page, allItem(page));
    await expect(label(page)).toHaveText('All commits');
    await expect(page.locator('#commitDropdownList .commit-picker-item.is-from')).toHaveCount(0);
    await expect(page.locator('#commitDropdownList .commit-picker-item.is-through')).toHaveCount(0);
  });

  test('picker is hidden on Staged scope and shown on All and Branch scopes', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#commitDropdown')).toBeVisible();

    await switchScope(page, 'staged');
    await expect(page.locator('#commitDropdown')).toBeHidden();
    await switchScope(page, 'all');
    await expect(page.locator('#commitDropdown')).toBeVisible();

    await switchScope(page, 'staged');
    await expect(page.locator('#commitDropdown')).toBeHidden();
    await switchScope(page, 'branch');
    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('selected commit resets on page reload', async ({ page }) => {
    await loadPage(page);
    await openCommitPicker(page);
    await pick(page, realCommitItems(page).first());
    await expect(label(page)).toContainText('only');

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(label(page)).toHaveText('All commits');
  });

  test('commit picker hidden when only one commit exists', async ({ page, request }) => {
    const commits = await (await request.get('/api/commits')).json();
    expect(Array.isArray(commits) && commits.length).toBeGreaterThan(1);
    await page.route('**/api/commits', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(commits.slice(0, 1)),
      });
    });

    const commitsResponse = page.waitForResponse(r => r.url().includes('/api/commits'));
    await loadPage(page);
    await commitsResponse;
    await expect(page.locator('#commitDropdown')).toBeHidden();
  });

  // Regression: switching commit scope hid out-of-scope unresolved comments
  // from the finish button label — it showed "Approve" while hidden_unresolved > 0.
  test('finish button says Finish Review while an unresolved comment is outside the selected commit', async ({ page, request }) => {
    // config.yaml is untracked — visible on "All commits" but not when a single commit is selected.
    await addComment(request, 'config.yaml', 1, 'Fix config');

    await loadPage(page);
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');

    await openCommitPicker(page);
    await pick(page, realCommitItems(page).first());
    // Wait for the filtered view (the comment's file is gone) before checking
    // the label, so a stale "Finish Review" cannot pass.
    await expect(label(page)).toContainText('only');
    await expect(page.locator('.tree-file[data-tree-path="config.yaml"]')).toHaveCount(0);
    await expect(fileHeader(page, 'deleted.txt')).toBeVisible();
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
  });
});
