import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, fileHeader, revealFile, switchScope } from './helpers';

// Pierre file items currently mounted in the diff pane. CodeView virtualizes
// long lists, so exact counts are only meaningful for short (fully mounted)
// file lists; the file tree is the authority for the full list.
function paneFiles(page: Page) {
  return page.locator('#filesContainer .pierre-file-header');
}

function scopeBtn(page: Page, scope: string) {
  return page.locator(`#scopeToggle .toggle-btn[data-scope="${scope}"]`);
}

// Saved scope lives in the consolidated crit-settings cookie (diffScope).
// Set it before loadPage, which only defaults diffScope when it is unset.
async function saveScope(page: Page, scope: string) {
  await page.context().addCookies([{
    name: 'crit-settings',
    value: encodeURIComponent(JSON.stringify({ diffScope: scope })),
    domain: 'localhost',
    path: '/',
  }]);
}

// Serve /api/session with only `scopes` available.
async function limitScopes(page: Page, scopes: string[]) {
  await page.route('**/api/session*', async route => {
    const response = await route.fetch();
    const json = await response.json();
    json.available_scopes = scopes;
    await route.fulfill({ json });
  });
}

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test.describe('Scope Toggle', () => {
  test('scope toggle is visible in git mode with All active and Branch offered on a feature branch', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#scopeToggle')).toBeVisible();
    await expect(scopeBtn(page, 'all')).toHaveClass(/active/);
    await expect(scopeBtn(page, 'branch')).toBeVisible();
  });

  test('switching to branch scope shows only committed files', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'branch');
    // Branch: server.go, deleted.txt, plan.md, skill.md, handler.js, routes.go, legacy.go (7 committed)
    await expect(page.locator('.tree-file')).toHaveCount(7);
    await expect(page.locator('.tree-file[data-tree-path="utils.go"]')).toHaveCount(0);
    await expect(page.locator('.tree-file[data-tree-path="config.yaml"]')).toHaveCount(0);
    // The diff pane re-renders with the branch files (first in order on top)
    await expect(fileHeader(page, 'deleted.txt')).toBeVisible();
    await expect(fileHeader(page, 'config.yaml')).toHaveCount(0);
    await revealFile(page, 'server.go');
    await revealFile(page, 'plan.md');
  });

  test('staged scope shows only staged files in pane and tree, survives a reload, and All restores the full list', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'staged');
    // Only the clicked button is active.
    await expect(scopeBtn(page, 'all')).not.toHaveClass(/active/);
    // Staged: utils.go, login.feature
    await expect(paneFiles(page)).toHaveCount(2);
    await expect(fileHeader(page, 'utils.go')).toBeVisible();
    await expect(fileHeader(page, 'login.feature')).toBeVisible();
    await expect(page.locator('.tree-file')).toHaveCount(2);
    await expect(page.locator('.tree-file-name', { hasText: 'utils.go' })).toBeVisible();

    // Scope persists across reload.
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden();
    await expect(scopeBtn(page, 'staged')).toHaveClass(/active/);
    await expect(paneFiles(page)).toHaveCount(2);
    await expect(fileHeader(page, 'utils.go')).toBeVisible();

    // Back to All: the full list returns (branch + unstaged files render).
    await switchScope(page, 'all');
    await expect.poll(() => page.locator('.tree-file').count()).toBeGreaterThan(7);
    await expect(fileHeader(page, 'config.yaml')).toBeVisible();
    await revealFile(page, 'server.go');
  });

  test('switching to unstaged scope shows only unstaged files', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'unstaged');
    // Unstaged: config.yaml only
    await expect(paneFiles(page)).toHaveCount(1);
    await expect(fileHeader(page, 'config.yaml')).toBeVisible();
  });

  test('unavailable scopes are disabled, not hidden, and clicking one does nothing', async ({ page }) => {
    await limitScopes(page, ['all', 'branch']);
    await loadPage(page);
    // Buttons are visible but disabled
    await expect(scopeBtn(page, 'staged')).toBeVisible();
    await expect(scopeBtn(page, 'unstaged')).toBeVisible();
    await expect(scopeBtn(page, 'staged')).toBeDisabled();
    await expect(scopeBtn(page, 'unstaged')).toBeDisabled();
    // Available scopes are enabled
    await expect(scopeBtn(page, 'all')).toBeEnabled();
    await expect(scopeBtn(page, 'branch')).toBeEnabled();

    // force: Playwright won't click disabled elements otherwise.
    await scopeBtn(page, 'staged').click({ force: true });
    await expect(scopeBtn(page, 'all')).toHaveClass(/active/);
    await expect(scopeBtn(page, 'staged')).not.toHaveClass(/active/);
  });

  test('falls back to all when saved scope becomes unavailable', async ({ page }) => {
    await saveScope(page, 'staged');
    await limitScopes(page, ['all', 'branch']);
    const scopes: (string | null)[] = [];
    page.on('request', req => {
      if (new URL(req.url()).pathname === '/api/session') scopes.push(new URL(req.url()).searchParams.get('scope'));
    });
    await loadPage(page);
    // The saved scope really was requested first, then dropped for "all".
    expect(scopes[0]).toBe('staged');
    await expect(scopeBtn(page, 'all')).toHaveClass(/active/);
    await expect(scopeBtn(page, 'staged')).not.toHaveClass(/active/);
    // All includes the unstaged config.yaml, which staged would not.
    await expect(fileHeader(page, 'config.yaml')).toBeVisible();
  });

  test('falls back to all and re-fetches files when stale scope returns empty list', async ({ page }) => {
    // Simulate: user was on a feature branch with "branch" scope, then switched to
    // the default branch where branch scope returns no files.
    await saveScope(page, 'branch');

    let requestCount = 0;
    await page.route('**/api/session*', async route => {
      requestCount++;
      const url = new URL(route.request().url());
      const scope = url.searchParams.get('scope');
      const response = await route.fetch();
      const json = await response.json();
      if (scope === 'branch') {
        // Simulate being on the default branch: branch scope has no files
        json.files = [];
        json.available_scopes = ['all', 'staged', 'unstaged'];
      }
      await route.fulfill({ json });
    });

    await loadPage(page);

    // Should fall back to "all" and re-fetch — files must render
    await expect(scopeBtn(page, 'all')).toHaveClass(/active/);
    await expect(scopeBtn(page, 'branch')).not.toHaveClass(/active/);
    await expect(paneFiles(page).first()).toBeVisible();
    // "all" includes the unstaged config.yaml, which branch scope would not
    await expect(fileHeader(page, 'config.yaml')).toBeVisible();
    // Must have made at least 2 session requests: initial (branch) + corrected (all)
    expect(requestCount).toBeGreaterThanOrEqual(2);
  });

  test('Shift+1..4 switch between All, Branch, Staged and Unstaged', async ({ page }) => {
    await loadPage(page);
    const shifted: Record<string, string> = { '1': '!', '2': '@', '3': '#', '4': '$' };
    const press = async (digit: string, scope: string) => {
      await page.keyboard.press(shifted[digit]);
      await expect(scopeBtn(page, scope)).toHaveClass(/active/);
      await expect(page.locator('#scopeToggle .toggle-btn.active')).toHaveCount(1);
    };

    // Shift+1 while All is already active leaves it active.
    await page.keyboard.press('!');
    await expect(scopeBtn(page, 'all')).toHaveClass(/active/);

    await press('2', 'branch');
    await press('3', 'staged');
    await press('4', 'unstaged');
    await press('1', 'all');
  });
});
