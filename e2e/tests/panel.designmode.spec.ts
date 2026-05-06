import { test, expect } from '@playwright/test';

// Phase F infra not online yet. Scenarios are written and parse cleanly so
// `npx playwright test --list --project=design-mode` enumerates them; the
// runner is a Phase F deliverable. Use `test.skip(true, 'phase F runner')`
// rather than `test.fixme` so the per-test reason shows in the trace.

test.describe('design-mode comments panel — M12 toggle + count badge', () => {
  test.skip(true, 'phase F runner');

  test('navbar #commentCount toggles the panel open/closed', async ({ page }) => {
    await page.goto('/design');
    const panel = page.locator('#commentsPanel');
    const toggle = page.locator('#commentCount');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    await toggle.click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
    await toggle.click();
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
  });

  test('persists open/closed across reloads via crit-settings cookie', async ({ page }) => {
    await page.goto('/design');
    const panel = page.locator('#commentsPanel');
    await page.locator('#commentCount').click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
    await page.reload();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
  });

  test('count badge reflects unresolved pin count and updates live', async ({ page }) => {
    await page.goto('/design');
    const badge = page.locator('#commentsPanelCountBadge');
    await expect(badge).toHaveText('0');
    // Add a pin via the agent flow (placeholder — actual interaction TBD in
    // Phase F when the test fixture site is wired up).
    // expect(badge).toHaveText('1') after a pin is created.
    // expect(badge).toHaveText('0') after the pin is resolved.
  });
});

test.describe('design-mode comments panel — M13 resize', () => {
  test.skip(true, 'phase F runner');

  test('drag handle resizes the panel and persists to crit-settings', async ({ page }) => {
    await page.goto('/design');
    const panel = page.locator('#commentsPanel');
    const handle = page.locator('#commentsPanelResizer');
    const before = await panel.evaluate(el => (el as HTMLElement).offsetWidth);

    const handleBox = await handle.boundingBox();
    if (!handleBox) throw new Error('resize handle not visible');
    await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y + handleBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(handleBox.x - 100, handleBox.y + handleBox.height / 2);
    await page.mouse.up();

    const after = await panel.evaluate(el => (el as HTMLElement).offsetWidth);
    expect(after).toBeGreaterThan(before);

    await page.reload();
    const restored = await page.locator('#commentsPanel').evaluate(el => (el as HTMLElement).offsetWidth);
    expect(restored).toBeGreaterThan(before);
  });

  test('NO upper clamp: panel can grow past viewport-preset width', async ({ page }) => {
    await page.goto('/design');
    const panel = page.locator('#commentsPanel');
    const handle = page.locator('#commentsPanelResizer');
    const handleBox = await handle.boundingBox();
    if (!handleBox) throw new Error('resize handle not visible');
    await page.mouse.move(handleBox.x, handleBox.y + 5);
    await page.mouse.down();
    await page.mouse.move(0, handleBox.y + 5);
    await page.mouse.up();
    const w = await panel.evaluate(el => (el as HTMLElement).offsetWidth);
    expect(w).toBeGreaterThan(600);
  });
});

test.describe('design-mode comments panel — M5 row controls', () => {
  test.skip(true, 'phase F runner');

  test('panel rows expose Expand, Edit, Resolve, Reply controls (parity with code review)', async ({ page }) => {
    await page.goto('/design');
    // Add a pin first (placeholder fixture flow).
    const row = page.locator('#commentsPanelBody .comment-card').first();
    await expect(row.locator('.comment-actions .crit-design-comment-resolve')).toBeVisible();
    // Edit + Reply + Expand affordances should be present once buildCommentCard
    // is reused in design rows (deferred — see follow-up plan).
    await expect(row.locator('[data-action="edit"]')).toBeVisible();
    await expect(row.locator('[data-action="reply"]')).toBeVisible();
    await expect(row.locator('[data-action="expand"]')).toBeVisible();
  });
});

test.describe('design-mode comments panel — M14 filter pill + body expand', () => {
  test.skip(true, 'phase F runner');

  test('filter pill (All / Open / Resolved) toggles row visibility and updates counts', async ({ page }) => {
    await page.goto('/design');
    const pill = page.locator('#commentsFilterPill');
    await pill.locator('[data-filter="open"]').click();
    await expect(pill.locator('[data-filter="open"]')).toHaveClass(/active/);
    // After resolving a row, switching to Open should hide it.
    // After switching to Resolved, the same row should appear.
  });

  test('Expand toggle in long bodies shows full text', async ({ page }) => {
    await page.goto('/design');
    const row = page.locator('#commentsPanelBody .comment-card').first();
    const body = row.locator('.comment-body');
    await expect(body).toHaveClass(/comment-body-collapsed/);
    await row.locator('[data-action="expand"]').click();
    await expect(body).not.toHaveClass(/comment-body-collapsed/);
  });
});

test.describe('design-mode comments panel — M15 panel close button', () => {
  test.skip(true, 'phase F runner');

  test('panel header close button hides the panel', async ({ page }) => {
    await page.goto('/design');
    const panel = page.locator('#commentsPanel');
    await page.locator('.comments-panel-close').click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
  });

  test('reopening via navbar restores prior width (M13 persistence)', async ({ page }) => {
    await page.goto('/design');
    // Resize first (covered in M13). Close then reopen. Width should match.
    const panel = page.locator('#commentsPanel');
    const before = await panel.evaluate(el => (el as HTMLElement).offsetWidth);
    await page.locator('.comments-panel-close').click();
    await page.locator('#commentCount').click();
    const after = await panel.evaluate(el => (el as HTMLElement).offsetWidth);
    expect(after).toBe(before);
  });
});
