import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { clearAllComments, loadPage, revealFile, switchToDocumentView, mdDocument, setDiffStyle, openLineComment } from './helpers';

test.beforeEach(async ({ page, request }) => {
  await clearAllComments(request);
  await page.emulateMedia({ colorScheme: 'dark' });
  await loadPage(page);
});

test('code theme, wrapping and diff controls persist and update the renderer', async ({ page }) => {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const theme = page.locator('#darkPaletteSelect');
  await expect(theme).toBeVisible();
  await expect.poll(() => theme.locator('option').count()).toBeGreaterThan(20);
  await theme.selectOption('nord');
  await expect(theme).toBeEnabled();
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'nord');
  await page.locator('#codeOverflowSelect').selectOption('wrap');
  await expect(page.locator('#hunkSeparatorsSelect')).toHaveCount(0);
  await page.locator('#inlineDiffSelect').selectOption('char');
  await page.locator('#changeIndicatorsSelect').selectOption('bars');
  await page.locator('#unchangedContextSelect').selectOption('expanded');
  await page.keyboard.press('Escape');
  const file = await revealFile(page, 'routes.go');
  await expect(file.locator('pre[data-overflow="wrap"]').first()).toBeVisible();
  await expect(file.locator('pre[data-indicators="bars"]').first()).toBeVisible();
  await expect(file.locator('[data-separator="line-info"]')).toHaveCount(0);
  await expect(file.locator('[data-column-number="20"]').first()).toBeVisible();
  await loadPage(page);
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(theme).toHaveValue('nord');
  await expect(page.locator('#codeOverflowSelect')).toHaveValue('wrap');
  await expect(page.locator('#inlineDiffSelect')).toHaveValue('char');
  await expect(page.locator('#changeIndicatorsSelect')).toHaveValue('bars');
  await expect(page.locator('#unchangedContextSelect')).toHaveValue('expanded');
});

test('separators remain expandable with a theme-relative colour override', async ({ page }) => {
  const file = await revealFile(page, 'routes.go');
  const separator = file.locator('[data-separator="line-info"]').first();
  await expect(separator).toBeVisible();
  await expect.poll(() => separator.evaluate(el => getComputedStyle(el).getPropertyValue('--diffs-bg-separator-override'))).toContain('color-mix');
  await expect(file.locator('[data-expand-button]').first()).toBeVisible();
  await expect(file.getByText(/@@ -/)).toHaveCount(0);
});

test('inline granularity changes the worker-rendered word highlights', async ({ page }) => {
  const file = await revealFile(page, 'routes.go');
  await expect.poll(() => file.locator('[data-diff-span]').count()).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const granularity = page.locator('#inlineDiffSelect');
  await granularity.selectOption('none');
  await expect(granularity).toBeEnabled();
  await page.keyboard.press('Escape');
  await expect(file.locator('[data-diff-span]')).toHaveCount(0);
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await granularity.selectOption('char');
  await expect(granularity).toBeEnabled();
  await page.keyboard.press('Escape');
  await expect.poll(() => file.locator('[data-diff-span]').count()).toBeGreaterThan(0);
});

test('paired palettes theme UI and markdown and follow explicit and system mode', async ({ page }) => {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  const dark = page.locator('#darkPaletteSelect');
  const light = page.locator('#lightPaletteSelect');
  await dark.selectOption('nord');
  await expect(dark).toBeEnabled();
  await light.selectOption('catppuccin-latte');
  await expect(light).toBeEnabled();
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'nord');
  await page.keyboard.press('Escape');
  await switchToDocumentView(page);
  await expect(mdDocument(page)).toBeVisible();
  expect(await mdDocument(page).evaluate(el => {
    const host = el.closest('diffs-container');
    const gutter = host?.shadowRoot?.querySelector('[data-gutter]');
    return !!gutter && getComputedStyle(gutter).display === 'none';
  })).toBe(true);
  expect(await mdDocument(page).evaluate(el => {
    const root = getComputedStyle(document.documentElement);
    const expected = document.createElement('span');
    expected.style.color = root.getPropertyValue('--crit-palette-fg');
    return getComputedStyle(el).color === expected.style.color;
  })).toBe(true);
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.locator('[data-settings-theme="light"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'catppuccin-latte');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'catppuccin-latte');
  await page.locator('[data-settings-theme="system"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'nord');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'catppuccin-latte');
  await page.keyboard.press('Escape');
  await loadPage(page);
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'catppuccin-latte');
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(dark).toHaveValue('nord');
  await expect(light).toHaveValue('catppuccin-latte');
});

test('Crit-owned chrome and palette controls retain contrast in both modes', async ({ page }) => {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.addStyleTag({ content: '* { transition: none !important; animation: none !important; }' });
  for (const mode of ['dark', 'light']) {
    await page.locator('[data-settings-theme="' + mode + '"]').click();
    const result = await new AxeBuilder({ page }).include('.header').include('#settingsOverlay').withRules(['color-contrast']).analyze();
    expect(result.violations).toEqual([]);
  }
});

test('Slack Ochin keeps readable light cards instead of borrowing its dark sidebar', async ({ page }) => {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.locator('#lightPaletteSelect').selectOption('slack-ochin');
  await expect(page.locator('#lightPaletteSelect')).toBeEnabled();
  await page.locator('[data-settings-theme="light"]').click();
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'slack-ochin');
  await page.addStyleTag({ content: '* { transition: none !important; animation: none !important; }' });
  expect(await page.locator('html').evaluate(el => getComputedStyle(el).getPropertyValue('--crit-palette-surface').trim())).toBe('#f6f7f8');
  const result = await new AxeBuilder({ page }).include('.header').include('#settingsOverlay').withRules(['color-contrast']).analyze();
  expect(result.violations).toEqual([]);
  await page.keyboard.press('Escape');
  await switchToDocumentView(page);
  await expect(mdDocument(page)).toHaveCSS('color', 'rgb(0, 35, 57)');
  await loadPage(page);
  await expect(page.locator('html')).toHaveAttribute('data-crit-palette', 'slack-ochin');
});

test('line numbers can be hidden and the setting survives a reload', async ({ page }) => {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.locator('#lineNumbersSelect').selectOption('off');
  await page.keyboard.press('Escape');
  const file = await revealFile(page, 'routes.go');
  await expect(file.locator('pre[data-disable-line-numbers]').first()).toBeVisible();
  await loadPage(page);
  await expect((await revealFile(page, 'routes.go')).locator('pre[data-disable-line-numbers]').first()).toBeVisible();
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await expect(page.locator('#lineNumbersSelect')).toHaveValue('off');
  await page.locator('#lineNumbersSelect').selectOption('on');
  await page.keyboard.press('Escape');
  await expect(file.locator('pre[data-disable-line-numbers]')).toHaveCount(0);
});

test('Monokai comment utility and comment border follow the UI palette', async ({ page }) => {
  await page.getByRole('button', { name: 'Settings', exact: true }).click();
  await page.locator('#darkPaletteSelect').selectOption('monokai');
  await expect(page.locator('#darkPaletteSelect')).toBeEnabled();
  await page.keyboard.press('Escape');
  const file = await revealFile(page, 'routes.go');
  const line = file.locator('[data-column-number="4"]').first();
  await line.hover();
  const utility = file.locator('[data-utility-button]').first();
  await expect(utility).toBeVisible();
  expect(await utility.evaluate(el => {
    const probe = document.createElement('span');
    probe.style.color = getComputedStyle(document.documentElement).getPropertyValue('--crit-palette-accent');
    return getComputedStyle(el).backgroundColor === probe.style.color;
  })).toBe(true);
  await utility.click();
  await page.locator('.comment-form textarea').fill('Theme border check');
  await page.locator('.comment-form .btn-primary').click();
  const card = page.locator('.comment-card').filter({ hasText: 'Theme border check' }).first();
  await expect(card).toBeVisible();
  expect(await card.evaluate(el => {
    const probe = document.createElement('span');
    probe.style.color = getComputedStyle(document.documentElement).getPropertyValue('--crit-palette-accent');
    return getComputedStyle(el).borderTopColor !== probe.style.color;
  })).toBe(true);
});

test('worker creation failure still paints the diff', async ({ page }) => {
  await page.addInitScript(() => {
    window.Worker = class { constructor() { throw new Error('Worker blocked for resilience test'); } } as unknown as typeof Worker;
  });
  await loadPage(page);
  const file = await revealFile(page, 'routes.go');
  await expect(file.locator('[data-line="4"]').first()).toBeVisible();
});

test('documents and file-level forms stay centered, with space above and below the form', async ({ page }) => {
  await page.setViewportSize({ width: 1800, height: 900 });
  await switchToDocumentView(page);
  await expect(mdDocument(page)).toBeVisible();
  const margins = await mdDocument(page).evaluate(el => {
    const host = el.closest('.pierre-document')!.getBoundingClientRect();
    const doc = el.querySelector('.document-wrapper')!.getBoundingClientRect();
    return { left: doc.left - host.left, right: host.right - doc.right };
  });
  expect(margins.left).toBeGreaterThan(0);
  expect(Math.abs(margins.left - margins.right)).toBeLessThan(1);
  const file = await revealFile(page, 'server.go');
  await expect(file.locator('.crit-review-file-header')).toHaveCSS('border-top-left-radius', '6px');
  await file.locator('.file-comment-btn').click();
  const form = file.locator('.pierre-file-level');
  await expect(form).toBeVisible();
  // Reading width, centered in the file-level cell.
  const place = await form.evaluate(el => {
    const r = el.getBoundingClientRect();
    const slot = el.parentElement!.getBoundingClientRect();
    return { width: r.width, left: r.left - slot.left, right: slot.right - r.right };
  });
  expect(place.width).toBeLessThanOrEqual(1040);
  expect(place.left).toBeGreaterThanOrEqual(16);
  expect(Math.abs(place.left - place.right)).toBeLessThan(2);
  // Space above and below the form inside the file-level annotation cell.
  const gaps = await form.evaluate(el => {
    const r = el.getBoundingClientRect();
    const cell = el.parentElement!.assignedSlot!.closest('[data-line-annotation]')!.getBoundingClientRect();
    return { above: r.top - cell.top, below: cell.bottom - r.bottom };
  });
  expect(gaps.above).toBeGreaterThanOrEqual(12);
  expect(gaps.below).toBeGreaterThanOrEqual(12);
  await form.getByRole('button', { name: 'Cancel', exact: true }).click();
});

test('review conversation lines up with the file list', async ({ page }) => {
  const conversation = page.locator('#filesContainer .review-conversation');
  await expect(conversation).toBeVisible();
  const header = page.locator('.crit-review-file-header').first();
  await expect(header).toBeVisible();
  const [a, b] = [await conversation.boundingBox(), await header.boundingBox()];
  expect(Math.abs(a!.x - b!.x)).toBeLessThan(2);
});

test('line comment forms in wide unified diffs stay at reading width', async ({ page }) => {
  await page.setViewportSize({ width: 1800, height: 900 });
  await setDiffStyle(page, 'unified');
  const file = await revealFile(page, 'server.go');
  const form = await openLineComment(page, file, 5);
  const [formBox, fileBox] = [await form.boundingBox(), await file.boundingBox()];
  expect(fileBox!.width).toBeGreaterThan(1200);
  expect(formBox!.width).toBeLessThanOrEqual(1040);
  await form.getByRole('button', { name: 'Cancel', exact: true }).click();
});
