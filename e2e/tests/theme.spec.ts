import { test, expect, type Page } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// ============================================================
// Theme Tests (git mode)
// ============================================================
test.describe('Theme — Git Mode', () => {
  test.beforeEach(async ({ page, context }) => {
    // Clear theme cookie before each test
    await context.clearCookies();
    await loadPage(page);
  });

  test('clicking light theme button sets data-theme="light" on <html>', async ({ page }) => {
    const lightBtn = page.locator('.theme-pill-btn[data-for-theme="light"]');
    await lightBtn.click();

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('light');
  });

  test('clicking dark theme button sets data-theme="dark" on <html>', async ({ page }) => {
    const darkBtn = page.locator('.theme-pill-btn[data-for-theme="dark"]');
    await darkBtn.click();

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('dark');
  });

  test('clicking system theme button removes data-theme from <html>', async ({ page }) => {
    // First set to dark, then switch to system
    const darkBtn = page.locator('.theme-pill-btn[data-for-theme="dark"]');
    await darkBtn.click();
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    const systemBtn = page.locator('.theme-pill-btn[data-for-theme="system"]');
    await systemBtn.click();

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBeNull();
  });

  test('theme persists across page reload', async ({ page }) => {
    // Set dark theme
    const darkBtn = page.locator('.theme-pill-btn[data-for-theme="dark"]');
    await darkBtn.click();
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    // Reload the page
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Should still be dark
    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBe('dark');
  });

  test('theme pill indicator moves when theme changes', async ({ page }) => {
    const indicator = page.locator('.theme-pill-indicator');

    // System theme: indicator at 0%
    const systemLeft = await indicator.evaluate(el => el.style.left);
    expect(systemLeft).toBe('0%');

    // Switch to light: indicator at 33.333%
    await page.locator('.theme-pill-btn[data-for-theme="light"]').click();
    const lightLeft = await indicator.evaluate(el => el.style.left);
    expect(lightLeft).toBe('33.333%');

    // Switch to dark: indicator at 66.666%
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    const darkLeft = await indicator.evaluate(el => el.style.left);
    expect(darkLeft).toBe('66.666%');
  });
});

// ============================================================
// File Sections Tests (git mode)
// ============================================================
test.describe('File Sections — Git Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('file sections are <details> elements', async ({ page }) => {
    const sections = page.locator('details.file-section');
    const count = await sections.count();
    expect(count).toBeGreaterThan(0);
  });

  test('first non-deleted file starts open', async ({ page }) => {
    // In git mode we have: plan.md (added), server.go (modified), handler.js (added), deleted.txt (deleted)
    // Non-deleted files should start open
    const planSection = page.locator('#file-section-plan\\.md');
    await expect(planSection).toHaveAttribute('open', '');

    const serverSection = page.locator('#file-section-server\\.go');
    await expect(serverSection).toHaveAttribute('open', '');
  });

  test('clicking file header toggles open/closed', async ({ page }) => {
    const section = page.locator('#file-section-plan\\.md');
    await expect(section).toHaveAttribute('open', '');

    // Click the summary to collapse
    const header = section.locator('summary.file-header');
    await header.click();

    // Should now be collapsed (no open attribute)
    await expect(section).not.toHaveAttribute('open', '');

    // Click again to re-open
    await header.click();
    await expect(section).toHaveAttribute('open', '');
  });

  test('file headers show status badges', async ({ page }) => {
    // plan.md is "added" -> badge says "New File"
    const planBadge = page.locator('#file-section-plan\\.md .file-header-badge');
    await expect(planBadge).toHaveText('New File');

    // server.go is "modified" -> badge says "Modified"
    const serverBadge = page.locator('#file-section-server\\.go .file-header-badge');
    await expect(serverBadge).toHaveText('Modified');

    // deleted.txt is "deleted" -> badge says "Deleted"
    const deletedBadge = page.locator('#file-section-deleted\\.txt .file-header-badge');
    await expect(deletedBadge).toHaveText('Deleted');

    // handler.js is "added" -> badge says "New File"
    const handlerBadge = page.locator('#file-section-handler\\.js .file-header-badge');
    await expect(handlerBadge).toHaveText('New File');
  });

  test('file headers show diff stats with additions and deletions', async ({ page }) => {
    // server.go has both additions and deletions
    const serverStats = page.locator('#file-section-server\\.go .file-header-stats');
    await expect(serverStats).toBeVisible();

    const addSpan = serverStats.locator('.add');
    await expect(addSpan).toBeVisible();
    await expect(addSpan).toContainText('+');

    const delSpan = serverStats.locator('.del');
    await expect(delSpan).toBeVisible();
    await expect(delSpan).toContainText('-');
  });

  test('deleted file starts collapsed', async ({ page }) => {
    const deletedSection = page.locator('#file-section-deleted\\.txt');
    await expect(deletedSection).toBeVisible();
    // Deleted file should NOT have the open attribute
    await expect(deletedSection).not.toHaveAttribute('open', '');
  });
});

// ============================================================
// Table of Contents Tests (git mode — TOC is hidden)
// ============================================================
test.describe('Table of Contents — Git Mode', () => {
  test('TOC toggle button is hidden in git mode', async ({ page }) => {
    await loadPage(page);

    const tocToggle = page.locator('#tocToggle');
    await expect(tocToggle).toBeHidden();
  });
});

// ============================================================
// Finish Review Tests (git mode)
// ============================================================
test.describe('Finish Review — Git Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('clicking Finish Review shows the waiting overlay', async ({ page }) => {
    const finishBtn = page.locator('#finishBtn');
    await expect(finishBtn).toBeVisible();
    await expect(finishBtn).toHaveText('Finish Review');

    await finishBtn.click();

    // Waiting overlay should become active
    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);
  });

  test('waiting overlay shows "Review Complete" text', async ({ page }) => {
    await page.locator('#finishBtn').click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    // The overlay dialog should contain "Review Complete"
    const heading = overlay.locator('h3');
    await expect(heading).toHaveText('Review Complete');
  });

  test('clicking "Back to editing" hides the overlay', async ({ page }) => {
    await page.locator('#finishBtn').click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    // Click back to editing
    const backBtn = page.locator('#backToEditing');
    await backBtn.click();

    // Overlay should no longer have the active class
    await expect(overlay).not.toHaveClass(/active/);
  });

  test('after going back, Finish Review button is available again', async ({ page }) => {
    const finishBtn = page.locator('#finishBtn');
    await finishBtn.click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    // Go back
    await page.locator('#backToEditing').click();
    await expect(overlay).not.toHaveClass(/active/);

    // Finish button should be enabled and say "Finish Review"
    await expect(finishBtn).toBeEnabled();
    await expect(finishBtn).toHaveText('Finish Review');
    await expect(finishBtn).toHaveClass(/btn-primary/);
  });
});
