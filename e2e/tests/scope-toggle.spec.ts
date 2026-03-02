import { test, expect, type Page } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

async function switchScope(page: Page, scope: string) {
  const responsePromise = page.waitForResponse(resp =>
    resp.url().includes('/api/session') && resp.status() === 200
  );
  await page.click(`#scopeToggle .toggle-btn[data-scope="${scope}"]`);
  await responsePromise;
  // Wait for rendering to complete
  await page.waitForTimeout(500);
}

test.afterEach(async ({ page }) => {
  // Reset scope cookie so other test files aren't affected
  await page.evaluate(() => {
    document.cookie = 'crit-diff-scope=all; path=/; max-age=31536000; SameSite=Strict';
  });
});

test.describe('Scope Toggle', () => {
  test('scope toggle is visible in git mode with All active by default', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#scopeToggle')).toBeVisible();
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="all"]')).toHaveClass(/active/);
  });

  test('Branch button is visible on feature branch', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="branch"]')).toBeVisible();
  });

  test('switching to branch scope shows only committed files', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'branch');
    // Branch: server.go, deleted.txt, plan.md, handler.js (4 committed)
    await expect(page.locator('.file-section')).toHaveCount(4);
    await expect(page.locator('.file-section', { hasText: 'server.go' })).toBeVisible();
    await expect(page.locator('.file-section', { hasText: 'plan.md' })).toBeVisible();
  });

  test('switching to staged scope shows only staged files', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'staged');
    // Staged: utils.go only
    await expect(page.locator('.file-section')).toHaveCount(1);
    await expect(page.locator('.file-section', { hasText: 'utils.go' })).toBeVisible();
  });

  test('switching to unstaged scope shows only unstaged files', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'unstaged');
    // Unstaged: config.yaml only
    await expect(page.locator('.file-section')).toHaveCount(1);
    await expect(page.locator('.file-section', { hasText: 'config.yaml' })).toBeVisible();
  });

  test('switching back to all scope restores full file list', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'staged');
    await expect(page.locator('.file-section')).toHaveCount(1);
    await switchScope(page, 'all');
    const count = await page.locator('.file-section').count();
    expect(count).toBeGreaterThanOrEqual(5);
  });

  test('active button styling updates on click', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'staged');
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="staged"]')).toHaveClass(/active/);
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="all"]')).not.toHaveClass(/active/);
  });

  test('scope persists across page reload', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'staged');
    await expect(page.locator('.file-section')).toHaveCount(1);
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="staged"]')).toHaveClass(/active/);
    await expect(page.locator('.file-section')).toHaveCount(1);
  });

  test('file tree updates when scope changes', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'staged');
    await expect(page.locator('.tree-file')).toHaveCount(1);
    await expect(page.locator('.tree-file-name', { hasText: 'utils.go' })).toBeVisible();
  });
});
