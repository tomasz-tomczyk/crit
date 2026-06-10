import { test, expect } from '@playwright/test';
import {
  clearAllLivePins,
  openPinComposer,
  waitForAgentReady,
} from './livemode-helpers';

// Phase F infra not online yet. Scenarios are written and parse cleanly so
// `npx playwright test --list --project=live-mode` enumerates them; the
// runner is a Phase F deliverable. Use `test.skip(true, 'phase F runner')`
// rather than `test.fixme` so the per-test reason shows in the trace.

test.describe('live-mode comments panel — M12 toggle (navbar)', () => {
  test('navbar #commentCount toggles the panel open/closed', async ({ page }) => {
    await waitForAgentReady(page);
    const panel = page.locator('#commentsPanel');
    const btn = page.locator('#commentCount');
    await expect(btn).toBeVisible();
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    // Regression for commit 07bd353: a `.crit-mode-live .comments-panel
    // { display: flex !important }` rule used to outweigh
    // `.comments-panel-hidden { display: none }`, so the class would flip
    // but the panel stayed visible. Assert actual visibility, not just
    // the class toggle.
    await expect(panel).toBeVisible();
    await btn.click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
    await expect(panel).toBeHidden();
    await btn.click();
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    await expect(panel).toBeVisible();
  });

  test('persists open/closed across reloads via crit-settings cookie', async ({ page }) => {
    await waitForAgentReady(page);
    await page.locator('#commentCount').click();
    await expect(page.locator('#commentsPanel')).toHaveClass(/comments-panel-hidden/);
    await page.reload();
    await expect(page.locator('#commentsPanel')).toHaveClass(/comments-panel-hidden/);
  });
});

test.describe('live-mode comments panel — M12 count badge', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('count badge reflects total pin count and updates live', async ({ page }) => {
    // Parity with code-review: the panel-header count badge tracks TOTAL
    // comments (resolved + open). The per-state breakdown is shown by the
    // filter pill counts (All / Open / Resolved). See app.js#renderCommentsPanel
    // and live-mode.panel-render.js#updateUnresolvedBadge — both write
    // `String(totalCount)` into #commentsPanelCountBadge.
    await openPinComposer(page);
    const badge = page.locator('#commentsPanelCountBadge');
    await expect(badge).toHaveText('0');
    await page.locator('.crit-live-composer-body').fill('Pin one');
    await page.locator('.crit-live-composer-save').click();
    await expect(badge).toHaveText('1');
    await page.locator('#commentsPanelBody .crit-live-comment-resolve').first().click();
    // Resolving doesn't drop total count — the filter pill's Open/Resolved
    // counts shift, but the header badge stays at the total.
    await expect(badge).toHaveText('1');
    await expect(page.locator('#commentsFilterPill .toggle-btn[data-filter="open"] .filter-count'))
      .toHaveText('0');
    await expect(page.locator('#commentsFilterPill .toggle-btn[data-filter="resolved"] .filter-count'))
      .toHaveText('1');
  });
});

test.describe('live-mode comments panel — M13 resize', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  // Drive pointerdown/pointermove/pointerup directly — the handler in
  // live-mode.js listens for native PointerEvents and headless Chromium's
  // page.mouse.* doesn't always synthesise pointer events that hit the
  // pointerdown listener.
  async function dragResizer(page: import('@playwright/test').Page, dx: number) {
    const handle = page.locator('#commentsPanelResizer');
    const box = await handle.boundingBox();
    if (!box) throw new Error('resize handle not visible');
    const sx = box.x + box.width / 2;
    const sy = box.y + box.height / 2;
    await handle.dispatchEvent('pointerdown', { pointerId: 1, clientX: sx, clientY: sy, button: 0, isPrimary: true });
    await handle.dispatchEvent('pointermove', { pointerId: 1, clientX: sx + dx, clientY: sy, button: 0, isPrimary: true });
    await handle.dispatchEvent('pointerup', { pointerId: 1, clientX: sx + dx, clientY: sy, button: 0, isPrimary: true });
  }

  test('drag handle resizes the panel and persists to crit-settings', async ({ page }) => {
    await waitForAgentReady(page);
    const panel = page.locator('#commentsPanel');
    const before = await panel.evaluate(el => (el as HTMLElement).offsetWidth);
    await dragResizer(page, -150);
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
    await dragResizer(page, -800);
    await expect.poll(
      () => panel.evaluate(el => (el as HTMLElement).offsetWidth),
    ).toBeGreaterThan(600);
  });
});

test.describe('live-mode comments panel — M5 row controls', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('panel rows expose Expand, Edit, Resolve, Reply controls (parity with code review)', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('original body');
    await page.locator('.crit-live-composer-save').click();
    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await expect(row).toBeVisible();
    // Production class names — live pins use crit-live-comment-* / .comment-collapse-btn.
    await expect(row.locator('.comment-collapse-btn')).toHaveCount(1);
    await expect(row.locator('.crit-live-comment-edit')).toHaveCount(1);
    await expect(row.locator('.crit-live-comment-reply')).toHaveCount(1);
    await expect(row.locator('.crit-live-comment-resolve')).toHaveCount(1);
  });

  test('Edit opens inline editor, Save updates body via PUT', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('original body');
    await page.locator('.crit-live-composer-save').click();
    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await row.locator('.crit-live-comment-edit').click();
    const ta = row.locator('.crit-live-edit-textarea');
    await expect(ta).toBeVisible();
    await ta.fill('edited body');
    await ta.press('Meta+Enter');
    await expect(row.locator('.crit-live-edit-composer')).toHaveCount(0);
    await expect(row.locator('.comment-body')).toContainText('edited body');
  });
});

test.describe('live-mode comments panel — M14 filter pill', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('filter pill (All / Open / Resolved) toggles row visibility and updates counts', async ({ page }) => {
    // Seed two pins, resolve one, verify filter behavior.
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('Pin one');
    await page.locator('.crit-live-composer-save').click();
    await openPinComposer(page, '#secondary-btn');
    await page.locator('.crit-live-composer-body').fill('Pin two');
    await page.locator('.crit-live-composer-save').click();

    const rows = page.locator('#commentsPanelBody .crit-live-comment-row');
    await expect(rows).toHaveCount(2);

    // Resolve the first pin.
    await page.locator('#commentsPanelBody .crit-live-comment-resolve').first().click();

    const pill = page.locator('#commentsFilterPill');
    // Open filter — only the unresolved row remains visible.
    await pill.locator('.toggle-btn[data-filter="open"]').click();
    await expect(pill.locator('.toggle-btn[data-filter="open"]')).toHaveClass(/active/);
    await expect(page.locator('#commentsPanelBody .crit-live-comment-row:visible')).toHaveCount(1);

    // Resolved filter — only the resolved row.
    await pill.locator('.toggle-btn[data-filter="resolved"]').click();
    await expect(pill.locator('.toggle-btn[data-filter="resolved"]')).toHaveClass(/active/);
    await expect(page.locator('#commentsPanelBody .crit-live-comment-row:visible')).toHaveCount(1);

    // All — both back.
    await pill.locator('.toggle-btn[data-filter="all"]').click();
    await expect(page.locator('#commentsPanelBody .crit-live-comment-row:visible')).toHaveCount(2);
  });
});

test.describe('live-mode comments panel — M14 body expand toggle', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  // Regression for commit 2aef74c: live-mode.row.js used to pass
  // collapseDefault: false unconditionally, so resolving a pin left the
  // card expanded. Code-review uses collapseDefault: !!c.resolved at every
  // panel mount → buildCommentCard auto-collapses resolved threads.
  test('resolving a pin auto-collapses the card on next render', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('to be resolved');
    await page.locator('.crit-live-composer-save').click();
    // The card element carries BOTH .comment-card and .crit-live-comment-row.
    const card = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await expect(card).toBeVisible();
    // Open thread by default — not collapsed.
    await expect.poll(
      () => card.evaluate((el) => el.classList.contains('collapsed')),
    ).toBe(false);
    // Resolve.
    await page.locator('#commentsPanelBody .crit-live-comment-resolve').first().click();
    // After resolution + re-render, the card defaults to collapsed
    // (collapseDefault: !!c.resolved).
    await expect.poll(
      () => card.evaluate((el) => el.classList.contains('collapsed')),
      { timeout: 5_000 },
    ).toBe(true);
  });

  test('Expand chevron on resolved card toggles .comment-card.collapsed', async ({ page }) => {
    // buildCommentCard auto-collapses resolved threads; clicking the chevron
    // (.comment-collapse-btn) toggles `.collapsed` on the card. Drive the
    // visible behavior using actual production selectors.
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('a long-ish body that is collapsible after resolve');
    await page.locator('.crit-live-composer-save').click();

    const card = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await expect(card).toBeVisible();
    // Resolve to trigger the auto-collapse default for resolved cards.
    await page.locator('#commentsPanelBody .crit-live-comment-resolve').first().click();

    // Wait for resolve to settle — fetch is async and panel re-renders on
    // its `.then`. Asserting on the resolved auto-collapse first removes
    // the race between the resolve PUT and the chevron click below.
    await expect.poll(
      () => card.evaluate((el) => el.classList.contains('collapsed')),
      { timeout: 5_000 },
    ).toBe(true);

    // Now the visible chevron toggle should expand the card.
    const collapseBtn = card.locator('.comment-collapse-btn').first();
    await expect(collapseBtn).toBeVisible();
    await collapseBtn.click();
    await expect.poll(
      () => card.evaluate((el) => el.classList.contains('collapsed')),
    ).toBe(false);
    // And clicking again collapses it back.
    await collapseBtn.click();
    await expect.poll(
      () => card.evaluate((el) => el.classList.contains('collapsed')),
    ).toBe(true);
  });
});

test.describe('live-mode comments panel — M15 panel close button', () => {
  test('panel header close button hides the panel', async ({ page }) => {
    await waitForAgentReady(page);
    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    await page.locator('.comments-panel-close').click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
  });
});

test.describe('live-mode comments panel — M15 reopen via navbar', () => {
  test('reopening via navbar restores prior width (M13 persistence)', async ({ page }) => {
    await waitForAgentReady(page);
    const panel = page.locator('#commentsPanel');
    // Set an explicit width via the persisted setting key so the test isn't
    // sensitive to the resizer-drag implementation.
    await page.evaluate(() => {
      const helpers = (window as any).crit && (window as any).crit.shared;
      if (helpers && helpers.setSetting) helpers.setSetting('live_commentsPanelWidth', 540);
    });
    await page.reload();
    await expect.poll(
      () => panel.evaluate(el => (el as HTMLElement).offsetWidth),
    ).toBeGreaterThan(500);

    // Close + reopen via navbar — width should be preserved.
    await page.locator('.comments-panel-close').click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
    await page.locator('#commentCount').click();
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);
    await expect.poll(
      () => panel.evaluate(el => (el as HTMLElement).offsetWidth),
    ).toBeGreaterThan(500);
  });
});

test.describe('live-mode comments panel — M18 reply composer', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  async function seedPinAndOpenReply(page: import('@playwright/test').Page) {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('Top-level pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await row.locator('button.crit-live-comment-reply').click();
    return row;
  }

  test('Reply on live pin posts to /api/comment/{id}/replies and renders below comment', async ({ page }) => {
    const rowWrap = page.locator('#commentsPanelBody .crit-live-comment-row-wrap').first();
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('Top-level pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await row.locator('button.crit-live-comment-reply').click();
    const composer = rowWrap.locator('.crit-live-reply-composer');
    await expect(composer).toBeVisible();
    await composer.locator('.crit-live-reply-textarea').fill('a reply');
    await composer.locator('.crit-live-reply-textarea').press('Meta+Enter');
    // makeReplyListBuilder writes the body into a shared `.reply-body` node
    // (matches code-review's reply DOM). Row class is the live-prefixed
    // `.crit-live-comment-reply` for back-compat with existing CSS.
    const reply = rowWrap.locator('.crit-live-comment-replies .crit-live-comment-reply').first();
    await expect(reply.locator('.reply-body')).toContainText('a reply');
  });

  test('Esc with text triggers confirm before discarding draft', async ({ page }) => {
    const row = await seedPinAndOpenReply(page);
    const rowWrap = page.locator('#commentsPanelBody .crit-live-comment-row-wrap').first();
    const ta = rowWrap.locator('.crit-live-reply-textarea');
    await expect(ta).toBeVisible();
    await ta.fill('half-written');
    page.once('dialog', async d => { await d.dismiss(); });
    await ta.press('Escape');
    await expect(rowWrap.locator('.crit-live-reply-composer')).toBeVisible();
    page.once('dialog', async d => { await d.accept(); });
    await ta.press('Escape');
    await expect(rowWrap.locator('.crit-live-reply-composer')).toHaveCount(0);
    void row;
  });

  test('Esc on empty composer closes immediately', async ({ page }) => {
    await seedPinAndOpenReply(page);
    const rowWrap = page.locator('#commentsPanelBody .crit-live-comment-row-wrap').first();
    await rowWrap.locator('.crit-live-reply-textarea').press('Escape');
    await expect(rowWrap.locator('.crit-live-reply-composer')).toHaveCount(0);
  });
});
