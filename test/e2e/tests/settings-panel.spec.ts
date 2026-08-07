import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

test.describe('Settings Panel', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('gear icon opens panel to Settings tab', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await expect(page.locator('.settings-tab.active')).toHaveText('Settings');
    await expect(page.locator('.settings-pane[data-pane="settings"]')).toHaveClass(/active/);
  });

  test('? key opens panel to Shortcuts tab', async ({ page }) => {
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await expect(page.locator('.settings-tab.active')).toHaveText('Shortcuts');
  });

  test('Escape closes the panel', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await page.keyboard.press('Escape');
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);
  });

  test('clicking outside closes the panel', async ({ page }) => {
    await page.click('#settingsToggle');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    // Click the overlay background (not the dialog)
    await page.locator('.settings-overlay').click({ position: { x: 10, y: 10 } });
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);
  });

  test('tab switching works', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('.settings-tab[data-tab="shortcuts"]');
    await expect(page.locator('.settings-pane[data-pane="shortcuts"]')).toHaveClass(/active/);
    await page.click('.settings-tab[data-tab="about"]');
    await expect(page.locator('.settings-pane[data-pane="about"]')).toHaveClass(/active/);
    await page.click('.settings-tab[data-tab="settings"]');
    await expect(page.locator('.settings-pane[data-pane="settings"]')).toHaveClass(/active/);
  });

  test('? key toggles shortcuts tab when panel is open on shortcuts', async ({ page }) => {
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);
  });

  test('? key switches to shortcuts tab when panel is open on different tab', async ({ page }) => {
    await page.click('#settingsToggle'); // opens to Settings tab
    await expect(page.locator('.settings-tab.active')).toHaveText('Settings');
    await page.keyboard.press('?');
    await expect(page.locator('.settings-tab.active')).toHaveText('Shortcuts');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
  });

  test('shortcuts pane shows grouped keyboard shortcuts', async ({ page }) => {
    await page.keyboard.press('?');
    const pane = page.locator('.settings-pane[data-pane="shortcuts"]');
    // Code-review mode shows five groups: Navigation, Comments, Review, Story,
    // View (the Live group is filtered out; Story shortcuts carry a "story mode"
    // badge and always render, like the "file mode"/"vcs mode" badged entries).
    await expect(pane.locator('.shortcuts-group-label')).toHaveCount(5);
    await expect(pane.locator('.shortcuts-group-label').first()).toHaveText('Navigation');
    await expect(pane.locator('.shortcuts-group-label').nth(3)).toHaveText('Story');
  });

  test('custom shortcut is applied immediately, persists, and can be reset', async ({ page }) => {
    await page.keyboard.press('?');
    const nextBlock = page.locator('[data-shortcut-id="next_block"]');
    await nextBlock.click();
    await page.keyboard.press('ArrowDown');
    await expect(page.locator('[data-shortcut-id="next_block"]')).toContainText('ArrowDown');

    await page.locator('.settings-overlay').click({ position: { x: 10, y: 10 } });
    await page.keyboard.press('j');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);
    await page.keyboard.press('ArrowDown');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);

    // A write through app.js must merge with the freshly saved shortcut,
    // rather than replacing it with an older cached settings object.
    await page.keyboard.press('h');
    await page.reload();
    await page.keyboard.press('?');
    await expect(page.locator('[data-shortcut-id="next_block"]')).toContainText('ArrowDown');
    await page.locator('.shortcut-reset-all').click();
    await page.locator('.settings-overlay').click({ position: { x: 10, y: 10 } });
    await page.keyboard.press('j');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
  });

  test('shortcut conflicts are shown without changing the row height', async ({ page }) => {
    await page.keyboard.press('?');
    await page.locator('.settings-dialog').evaluate(async (element) => {
      await Promise.all(element.getAnimations().map((animation) => animation.finished));
    });
    const previousBlock = page.locator('[data-shortcut-id="previous_block"]');
    const row = previousBlock.locator('xpath=ancestor::tr');
    const heightBefore = await row.evaluate((element) => element.getBoundingClientRect().height);
    await previousBlock.click();
    const heightWhileEditing = await row.evaluate((element) => element.getBoundingClientRect().height);
    expect(heightWhileEditing).toBe(heightBefore);
    await page.keyboard.press('j');

    await expect(page.locator('.mini-toast--error')).toContainText(
      'already assigned to “Next block”',
    );
    await expect(row.locator('.shortcut-inline-feedback')).toHaveCount(0);
  });

  test('modified submit chords stay reserved', async ({ page }) => {
    await page.keyboard.press('?');
    const finishReview = page.locator('[data-shortcut-id="finish_review"]');
    await finishReview.click();
    await page.keyboard.press('Control+Shift+Enter');

    await expect(page.locator('.mini-toast--error')).toContainText('is reserved by Crit');
    await expect(finishReview).toContainText('Shift+F');
  });

  test('settings pane shows display section with theme, code font and width', async ({ page }) => {
    await page.click('#settingsToggle');
    const pane = page.locator('.settings-pane[data-pane="settings"]');
    await expect(pane.locator('.settings-display-label').first()).toHaveText('Theme');
    await expect(pane.locator('.settings-display-label').filter({ hasText: 'Code font' })).toBeVisible();
    await expect(pane.locator('.settings-display-label').filter({ hasText: 'Content Width' })).toBeVisible();
  });

  test('system code font overrides --crit-font-mono and persists across reload', async ({ page }) => {
    await page.click('#settingsToggle');
    const monoFont = () =>
      page.evaluate(() =>
        getComputedStyle(document.documentElement).getPropertyValue('--crit-font-mono').trim(),
      );
    expect(await monoFont()).toContain('JetBrains Mono');

    await page.selectOption('#codeFontSelect', 'system');
    await expect(page.locator('#codeFontCustomRow')).toBeHidden();
    await expect.poll(monoFont).toBe('ui-monospace, SFMono-Regular, Menlo, Consolas, monospace');

    await page.reload();
    await expect.poll(monoFont).toBe('ui-monospace, SFMono-Regular, Menlo, Consolas, monospace');

    // Back to Default drops the override entirely rather than pinning a copy.
    await page.click('#settingsToggle');
    await page.selectOption('#codeFontSelect', 'default');
    await expect.poll(monoFont).toContain('JetBrains Mono');
  });

  test('custom code font applies, and an invalid value is rejected', async ({ page }) => {
    await page.click('#settingsToggle');
    const monoFont = () =>
      page.evaluate(() =>
        getComputedStyle(document.documentElement).getPropertyValue('--crit-font-mono').trim(),
      );

    await page.selectOption('#codeFontSelect', 'custom');
    const input = page.locator('#codeFontCustomInput');
    await expect(input).toBeVisible();
    await input.fill("'Comic Mono', monospace");
    await input.blur();
    await expect.poll(monoFont).toBe("'Comic Mono', monospace");

    // A value that could escape the declaration falls back to the default.
    await input.fill('monospace; background: red');
    await input.blur();
    await expect(page.locator('.mini-toast--error')).toContainText('valid font-family');
    await expect(input).toHaveAttribute('aria-invalid', 'true');
    await expect.poll(monoFont).toContain('JetBrains Mono');
  });

  test('settings pane shows configuration cards', async ({ page }) => {
    await page.click('#settingsToggle');
    const pane = page.locator('.settings-pane[data-pane="settings"]');
    // Core cards — always rendered (in various states). Integration title varies
    // (AI Integration vs Integration Available); share title varies (Share vs Sharing enabled).
    await expect(pane.locator('.config-card-title', { hasText: 'Account' })).toBeVisible();
    await expect(pane.locator('.config-card-title', { hasText: 'Agent Command' })).toBeVisible();
    await expect(
      pane.locator('.config-card-title', { hasText: /AI Integration|Integration Available/ }).first(),
    ).toBeVisible();
    await expect(
      pane.locator('.config-card-title', { hasText: /Share|Sharing enabled/ }).first(),
    ).toBeVisible();
  });

  test('about pane shows version and session info', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('.settings-tab[data-tab="about"]');
    const pane = page.locator('.settings-pane[data-pane="about"]');
    await expect(pane.locator('.about-header h2')).toHaveText('Crit');
    await expect(pane.locator('.about-session')).toBeVisible();
    await expect(pane.locator('.about-links')).toBeVisible();
  });

  test('theme toggle in settings panel changes theme', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="dark"]');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.click('[data-settings-theme="light"]');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });

  test('width toggle changes content width', async ({ page }) => {
    await page.click('#settingsToggle');
    await page.click('[data-settings-width="compact"]');
    await expect(page.locator('html')).toHaveAttribute('data-width', 'compact');
    await page.click('[data-settings-width="wide"]');
    await expect(page.locator('html')).toHaveAttribute('data-width', 'wide');
  });

  test('theme pill is not in header', async ({ page }) => {
    await expect(page.locator('.header .theme-pill')).toHaveCount(0);
  });

  test('no old shortcuts overlay in DOM', async ({ page }) => {
    await expect(page.locator('#shortcutsOverlay')).toHaveCount(0);
  });
});
