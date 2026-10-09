import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, revealFile, fileHeader, diffLine } from './helpers';
import { ensureRangeFocus } from './range-helpers';

const MMD = 'flow.mmd';

test.beforeEach(async ({ request }) => {
  await ensureRangeFocus(request);
  await clearAllComments(request);
});

test('a .mmd file opens on Source in git mode and loads mermaid.min.js only for Diagram', async ({ page }) => {
  const requested: string[] = [];
  page.on('request', req => { if (req.url().includes('mermaid.min.js')) requested.push(req.url()); });
  await loadPage(page);

  const item = await revealFile(page, MMD);
  const toggle = fileHeader(page, MMD).locator('.file-header-toggle');
  await expect(toggle.locator('.toggle-btn')).toHaveText(['Diagram', 'Source']);
  await expect(toggle.locator('[data-mode="diff"]')).toHaveAttribute('aria-pressed', 'true');
  await expect(diffLine(item, 1)).toContainText('flowchart TD');
  await expect(item.locator('.crit-render')).toHaveCount(0);
  expect(requested).toHaveLength(0);

  await toggle.locator('[data-mode="rendered"]').click();
  const target = item.locator('.crit-render[data-crit-renderer="mermaid"][data-crit-kind="file"]');
  await expect(target).toHaveAttribute('data-crit-state', 'rendered');
  await expect(target.locator('.crit-render-output svg')).toBeVisible();
  expect(requested).toHaveLength(1);

  await toggle.locator('[data-mode="diff"]').click();
  await expect(diffLine(item, 1)).toContainText('flowchart TD');
});
