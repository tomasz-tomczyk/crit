import { test, expect, type Page } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// ============================================================
// Theme Tests (file mode)
// ============================================================
test.describe('Theme — File Mode', () => {
  test.beforeEach(async ({ page }) => {
    // Clear theme state before each test
    await page.goto('/');
    await page.evaluate(() => localStorage.removeItem('crit-theme'));
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
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    await page.locator('.theme-pill-btn[data-for-theme="system"]').click();

    const dataTheme = await page.locator('html').getAttribute('data-theme');
    expect(dataTheme).toBeNull();
  });

  test('theme persists across page reload', async ({ page }) => {
    // Set dark theme
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    expect(await page.locator('html').getAttribute('data-theme')).toBe('dark');

    // Reload
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

    // Switch to light
    await page.locator('.theme-pill-btn[data-for-theme="light"]').click();
    const lightLeft = await indicator.evaluate(el => el.style.left);
    expect(lightLeft).toBe('33.333%');

    // Switch to dark
    await page.locator('.theme-pill-btn[data-for-theme="dark"]').click();
    const darkLeft = await indicator.evaluate(el => el.style.left);
    expect(darkLeft).toBe('66.666%');
  });
});

// ============================================================
// File Sections Tests (file mode)
// ============================================================
test.describe('File Sections — File Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('all files start expanded (none are deleted in file mode)', async ({ page }) => {
    const sections = page.locator('details.file-section');
    const count = await sections.count();
    expect(count).toBe(3); // plan.md, server.go, handler.js

    for (let i = 0; i < count; i++) {
      await expect(sections.nth(i)).toHaveAttribute('open', '');
    }
  });

  test('clicking file header toggles open/closed', async ({ page }) => {
    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    await expect(section).toHaveAttribute('open', '');

    // Click the summary to collapse
    const header = section.locator('summary.file-header');
    await header.click();

    await expect(section).not.toHaveAttribute('open', '');

    // Click again to re-open
    await header.click();
    await expect(section).toHaveAttribute('open', '');
  });
});

// ============================================================
// Table of Contents Tests (multi-file mode — TOC should be hidden)
// ============================================================
test.describe('Table of Contents — File Mode (multi-file)', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('TOC toggle button is hidden when reviewing multiple files', async ({ page }) => {
    const tocToggle = page.locator('#tocToggle');
    await expect(tocToggle).toBeHidden();
  });

  test('TOC panel stays hidden when reviewing multiple files', async ({ page }) => {
    const toc = page.locator('#toc');
    await expect(toc).toHaveClass(/toc-hidden/);
  });
});

// ============================================================
// Finish Review Tests (file mode)
// ============================================================
test.describe('Finish Review — File Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('clicking Finish Review shows the waiting overlay', async ({ page }) => {
    const finishBtn = page.locator('#finishBtn');
    await expect(finishBtn).toBeVisible();
    await expect(finishBtn).toHaveText('Finish Review');

    await finishBtn.click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);
  });

  test('waiting overlay shows "Review Complete" text', async ({ page }) => {
    await page.locator('#finishBtn').click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    const heading = overlay.locator('h3');
    await expect(heading).toHaveText('Review Complete');
  });

  test('clicking "Back to editing" hides the overlay', async ({ page }) => {
    await page.locator('#finishBtn').click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    await page.locator('#backToEditing').click();
    await expect(overlay).not.toHaveClass(/active/);
  });

  test('after going back, Finish Review button is available again', async ({ page }) => {
    const finishBtn = page.locator('#finishBtn');
    await finishBtn.click();

    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    await page.locator('#backToEditing').click();
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);

    await expect(finishBtn).toBeEnabled();
    await expect(finishBtn).toHaveText('Finish Review');
    await expect(finishBtn).toHaveClass(/btn-primary/);
  });
});
