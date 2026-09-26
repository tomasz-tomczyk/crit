import { test, expect } from '@playwright/test';

// /themes: fixed samples for flipping through the bundled themes.
test.describe('Theme preview page', () => {
  test('switches themes with the keyboard and saves the choice', async ({ page, context }) => {
    await page.goto('/themes#dark/dracula');
    const html = page.locator('html');
    await expect(html).toHaveAttribute('data-crit-palette', 'dracula');
    await expect(html).toHaveAttribute('data-theme', 'dark');
    // The sample diff is a real Pierre render in the chosen theme.
    await expect(page.locator('#previewDiff [data-line]').first()).toBeVisible();
    await expect(page.locator('#previewDiff .comment-card')).toBeVisible();

    await page.locator('#themeList').focus();
    await page.keyboard.press('ArrowDown');
    await expect(html).not.toHaveAttribute('data-crit-palette', 'dracula');
    const next = await html.getAttribute('data-crit-palette');
    await expect(page).toHaveURL(new RegExp(`#dark/${next}$`));
    await expect(page.locator(`#theme-${next}`)).toHaveAttribute('aria-selected', 'true');

    await page.locator('#useTheme').click();
    await expect(page.locator('#useTheme')).toBeDisabled();
    const cookie = (await context.cookies()).find(c => c.name === 'crit-settings');
    expect(JSON.parse(decodeURIComponent(cookie!.value)).darkPalette).toBe(next);
  });

  test('light and dark lists only offer their own themes', async ({ page }) => {
    await page.goto('/themes#light/github-light-default');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    await expect(page.locator('#theme-github-light-default')).toBeVisible();
    await expect(page.locator('#theme-dracula')).toHaveCount(0);
    await page.locator('[data-preview-mode="dark"]').click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(page.locator('#theme-dracula')).toBeVisible();
    await expect(page.locator('#theme-github-light-default')).toHaveCount(0);
  });

  test('system colour changes preserve the palette being previewed', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto('/themes#light/min-light');
    await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'min-light');
    await page.emulateMedia({ colorScheme: 'light' });
    await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'min-light');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    await expect(page.locator('[data-preview-mode="light"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('[data-preview-mode="dark"]')).toHaveAttribute('aria-pressed', 'false');
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'min-light');
    await expect(page.locator('#previewDiff [data-line]').first()).toBeVisible();
  });

  test('follows display settings and edits them in the settings dialog', async ({ page, context }) => {
    await page.goto('/themes#dark/tokyo-night');
    const pre = page.locator('#previewDiff pre').first();
    await expect(pre).toBeVisible();
    await expect(pre).not.toHaveAttribute('data-disable-line-numbers');

    await page.locator('#settingsToggle').click();
    await page.locator('#lineNumbersSelect').selectOption('off');
    await expect(page.locator('#previewDiff pre').first()).toHaveAttribute('data-disable-line-numbers', '');
    await page.locator('#codeOverflowSelect').selectOption('wrap');
    await expect(page.locator('#previewDiff pre').first()).toHaveAttribute('data-overflow', 'wrap');
    // Review-only rows and the link back to this page are not shown here.
    await expect(page.locator('.settings-theme-preview-link')).toHaveCount(0);
    await page.locator('#darkPaletteSelect').selectOption('nord');
    await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'nord');
    await page.keyboard.press('Escape');

    const settings = JSON.parse(decodeURIComponent((await context.cookies()).find(c => c.name === 'crit-settings')!.value));
    expect(settings).toMatchObject({ lineNumbers: 'off', codeOverflow: 'wrap', darkPalette: 'nord' });
    // Saved settings apply on the next visit too.
    await page.reload();
    await expect(page.locator('#previewDiff pre').first()).toHaveAttribute('data-disable-line-numbers', '');
  });

  test('boosting syntax contrast darkens faint theme colours', async ({ page }) => {
    await page.goto('/themes#light/min-light');
    // Min Light's comments are #c2c3c5 (1.76:1 on white) as designed.
    const comment = page.locator('#previewDiff [data-line] span', { hasText: 'authMiddleware checks the session' }).first();
    await expect(comment).toHaveCSS('color', 'rgb(194, 195, 197)');
    await page.locator('#settingsToggle').click();
    await page.locator('#boostContrastSelect').selectOption('on');
    await expect.poll(async () => {
      const rgb = await page.locator('#previewDiff [data-line] span', { hasText: 'authMiddleware checks the session' }).first()
        .evaluate(el => getComputedStyle(el).color.match(/\d+/g)!.map(Number));
      return rgb[0] < 120 && Math.abs(rgb[0] - rgb[2]) < 20; // darker, still grey
    }).toBe(true);
    await page.locator('#boostContrastSelect').selectOption('off');
    await expect(page.locator('#previewDiff [data-line] span', { hasText: 'authMiddleware checks the session' }).first())
      .toHaveCSS('color', 'rgb(194, 195, 197)');
  });

  test('settings link opens the preview', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    await expect(page.getByRole('link', { name: 'Preview all themes' })).toHaveAttribute('href', '/themes');
  });
});
