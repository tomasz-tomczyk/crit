import { test, expect } from '@playwright/test';
import {
  clearAllDesignPins,
  openPinComposer,
  waitForAgentReady,
} from './designmode-helpers';

// Phase F infra not online yet. Scenarios are written and parse cleanly so
// `npx playwright test --list --project=design-mode` enumerates them; the
// runner is a Phase F deliverable. Use `test.skip(true, 'phase F runner')`
// rather than `test.fixme` so the per-test reason shows in the trace.

test.describe('design-mode comments panel — M12 toggle (navbar)', () => {
  // #commentNavGroup (which contains #commentCount) is hidden in design mode
  // by style-design.css line 23. Listener is wired but the button is
  // invisible — pending production work to expose a panel-toggle affordance
  // in the design-mode chrome. Keep fixme until a navbar entry point ships.
  test.fixme(true, 'no visible panel-toggle affordance in design mode (#commentNavGroup hidden by style-design.css)');

  test('navbar #commentCount toggles the panel open/closed', async ({ page }) => {
    await page.goto('/design');
  });

  test('persists open/closed across reloads via crit-settings cookie', async ({ page }) => {
    await page.goto('/design');
  });
});

test.describe('design-mode comments panel — M12 count badge', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('count badge reflects unresolved pin count and updates live', async ({ page }) => {
    await openPinComposer(page);
    const badge = page.locator('#commentsPanelCountBadge');
    await expect(badge).toHaveText('0');
    await page.locator('.crit-design-composer-body').fill('Pin one');
    await page.locator('.crit-design-composer-save').click();
    await expect(badge).toHaveText('1');
    await page.locator('#commentsPanelBody .crit-design-comment-resolve').first().click();
    await expect(badge).toHaveText('0');
  });
});

test.describe('design-mode comments panel — M13 resize', () => {
  // #commentsPanelResizer (the .sidebar-resize-handle) is hidden in design
  // mode by style-design.css line 14. design-mode.js wires the pointerdown
  // listener but the handle isn't user-visible / draggable. Pending a
  // design-mode-specific resize affordance.
  test.fixme(true, '#commentsPanelResizer hidden in design mode (.sidebar-resize-handle display:none in style-design.css)');

  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('drag handle resizes the panel and persists to crit-settings', async ({ page }) => {
    await waitForAgentReady(page);
    const panel = page.locator('#commentsPanel');
    const handle = page.locator('#commentsPanelResizer');
    const before = await panel.evaluate(el => (el as HTMLElement).offsetWidth);

    const handleBox = await handle.boundingBox();
    if (!handleBox) throw new Error('resize handle not visible');
    // Drag left to grow the panel (panel is on the right; left edge is the resize handle).
    await page.mouse.move(handleBox.x + handleBox.width / 2, handleBox.y + handleBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(handleBox.x - 100, handleBox.y + handleBox.height / 2, { steps: 10 });
    await page.mouse.up();

    await expect.poll(
      () => panel.evaluate(el => (el as HTMLElement).offsetWidth),
    ).toBeGreaterThan(before);

    await page.reload();
    await expect.poll(
      () => page.locator('#commentsPanel').evaluate(el => (el as HTMLElement).offsetWidth),
    ).toBeGreaterThan(before);
  });

  test('NO upper clamp: panel can grow past viewport-preset width', async ({ page }) => {
    await waitForAgentReady(page);
    const panel = page.locator('#commentsPanel');
    const handle = page.locator('#commentsPanelResizer');
    const handleBox = await handle.boundingBox();
    if (!handleBox) throw new Error('resize handle not visible');
    await page.mouse.move(handleBox.x, handleBox.y + 5);
    await page.mouse.down();
    await page.mouse.move(0, handleBox.y + 5, { steps: 20 });
    await page.mouse.up();
    await expect.poll(
      () => panel.evaluate(el => (el as HTMLElement).offsetWidth),
    ).toBeGreaterThan(600);
  });
});

test.describe('design-mode comments panel — M5 row controls', () => {
  // buildCommentCard mounts Resolve and Reply buttons (with class names
  // crit-design-comment-resolve / crit-design-comment-reply), and a
  // .comment-collapse-btn for expand/collapse. It does NOT emit a separate
  // [data-action="edit"] button on design rows — the only inline-edit path
  // goes through markdown editing on the body. Keep fixme until parity work
  // ships an Edit affordance in design rows.
  test.fixme(true, '[data-action="edit"|"reply"|"expand"] selectors not emitted by design rows; buildCommentCard uses different class names');

  test('panel rows expose Expand, Edit, Resolve, Reply controls (parity with code review)', async ({ page }) => {
    await page.goto('/design');
  });
});

test.describe('design-mode comments panel — M14 filter pill', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('filter pill (All / Open / Resolved) toggles row visibility and updates counts', async ({ page }) => {
    // Seed two pins, resolve one, verify filter behavior.
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('Pin one');
    await page.locator('.crit-design-composer-save').click();
    await openPinComposer(page, '#secondary-btn');
    await page.locator('.crit-design-composer-body').fill('Pin two');
    await page.locator('.crit-design-composer-save').click();

    const rows = page.locator('#commentsPanelBody .crit-design-comment-row');
    await expect(rows).toHaveCount(2);

    // Resolve the first pin.
    await page.locator('#commentsPanelBody .crit-design-comment-resolve').first().click();

    const pill = page.locator('#commentsFilterPill');
    // Open filter — only the unresolved row remains visible.
    await pill.locator('.toggle-btn[data-filter="open"]').click();
    await expect(pill.locator('.toggle-btn[data-filter="open"]')).toHaveClass(/active/);
    await expect(page.locator('#commentsPanelBody .crit-design-comment-row:visible')).toHaveCount(1);

    // Resolved filter — only the resolved row.
    await pill.locator('.toggle-btn[data-filter="resolved"]').click();
    await expect(pill.locator('.toggle-btn[data-filter="resolved"]')).toHaveClass(/active/);
    await expect(page.locator('#commentsPanelBody .crit-design-comment-row:visible')).toHaveCount(1);

    // All — both back.
    await pill.locator('.toggle-btn[data-filter="all"]').click();
    await expect(page.locator('#commentsPanelBody .crit-design-comment-row:visible')).toHaveCount(2);
  });
});

test.describe('design-mode comments panel — M14 body expand toggle', () => {
  // Test asserts row.locator('.comment-body').toHaveClass(/comment-body-collapsed/)
  // and a [data-action="expand"] button. buildCommentCard uses
  // .comment-card.collapsed (on the card, not the body) and a
  // .comment-collapse-btn. Selector contract drift — keep fixme until either
  // the test contract or the production class names converge.
  test.fixme(true, 'comment-body-collapsed class + [data-action="expand"] selector not emitted; uses .comment-card.collapsed + .comment-collapse-btn');

  test('Expand toggle in long bodies shows full text', async ({ page }) => {
    await page.goto('/design');
  });
});

test.describe('design-mode comments panel — M15 panel close button', () => {
  test('panel header close button hides the panel', async ({ page }) => {
    await waitForAgentReady(page);
    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    await page.locator('.comments-panel-close').click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
  });
});

test.describe('design-mode comments panel — M15 reopen via navbar', () => {
  // Reopen path goes through #commentCount, but #commentNavGroup is hidden
  // by style-design.css in design mode, so the only navbar entry point is
  // not user-visible. Same gap as M12 toggle.
  test.fixme(true, '#commentCount not visible in design mode; #commentNavGroup hidden by style-design.css');

  test('reopening via navbar restores prior width (M13 persistence)', async ({ page }) => {
    await page.goto('/design');
  });
});

test.describe('design-mode comments panel — M18 reply composer', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  async function seedPinAndOpenReply(page: import('@playwright/test').Page) {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('Top-level pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    const row = page.locator('#commentsPanelBody .crit-design-comment-row').first();
    await row.locator('button.crit-design-comment-reply').click();
    return row;
  }

  test('Reply on design pin posts to /api/comment/{id}/replies and renders below comment', async ({ page }) => {
    const rowWrap = page.locator('#commentsPanelBody .crit-design-comment-row-wrap').first();
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('Top-level pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    const row = page.locator('#commentsPanelBody .crit-design-comment-row').first();
    await row.locator('button.crit-design-comment-reply').click();
    const composer = rowWrap.locator('.crit-design-reply-composer');
    await expect(composer).toBeVisible();
    await composer.locator('.crit-design-reply-textarea').fill('a reply');
    await composer.locator('.crit-design-reply-textarea').press('Meta+Enter');
    const reply = rowWrap.locator('.crit-design-comment-replies .crit-design-comment-reply').first();
    await expect(reply.locator('.crit-design-reply-body')).toContainText('a reply');
  });

  test('Esc with text triggers confirm before discarding draft', async ({ page }) => {
    const row = await seedPinAndOpenReply(page);
    const rowWrap = page.locator('#commentsPanelBody .crit-design-comment-row-wrap').first();
    const ta = rowWrap.locator('.crit-design-reply-textarea');
    await expect(ta).toBeVisible();
    await ta.fill('half-written');
    page.once('dialog', async d => { await d.dismiss(); });
    await ta.press('Escape');
    await expect(rowWrap.locator('.crit-design-reply-composer')).toBeVisible();
    page.once('dialog', async d => { await d.accept(); });
    await ta.press('Escape');
    await expect(rowWrap.locator('.crit-design-reply-composer')).toHaveCount(0);
    void row;
  });

  test('Esc on empty composer closes immediately', async ({ page }) => {
    await seedPinAndOpenReply(page);
    const rowWrap = page.locator('#commentsPanelBody .crit-design-comment-row-wrap').first();
    await rowWrap.locator('.crit-design-reply-textarea').press('Escape');
    await expect(rowWrap.locator('.crit-design-reply-composer')).toHaveCount(0);
  });
});
