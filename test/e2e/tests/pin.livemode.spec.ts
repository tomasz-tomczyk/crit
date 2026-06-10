import { test, expect } from '@playwright/test';
import {
  clearAllLivePins,
  getIframe,
  openPinComposer,
  NAV_OTHER,
  PIN_TARGET,
} from './livemode-helpers';

test.describe('live-mode pin composer — M11 sustained highlight', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('clicking an element in Pin mode keeps it outlined while composer is open', async ({ page }) => {
    await openPinComposer(page);
    await expect(getIframe(page).locator('.crit-live-pending-highlight')).toHaveCount(1);
  });

  test('Cancel removes the highlight', async ({ page }) => {
    await openPinComposer(page);
    await expect(getIframe(page).locator('.crit-live-pending-highlight')).toHaveCount(1);
    await page.locator('.crit-live-composer-cancel').click();
    await expect(getIframe(page).locator('.crit-live-pending-highlight')).toHaveCount(0);
  });

  test('Escape removes the highlight', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').focus();
    await page.keyboard.press('Escape');
    await expect(getIframe(page).locator('.crit-live-pending-highlight')).toHaveCount(0);
  });

  test('Save removes the highlight after the comment is created', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('A pin comment.');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-live-pending-highlight')).toHaveCount(0);
  });

  test('route change auto-clears the highlight', async ({ page }) => {
    await openPinComposer(page);
    // Switch back to navigate mode so the link click is not suppressed by the agent.
    await page.locator('#liveModeToggle button[data-mode="navigate"]').click();
    await getIframe(page).locator(NAV_OTHER).click();
    await expect(page.locator('#liveRouteName')).toHaveText('/dashboard');
    await expect(getIframe(page).locator('.crit-live-pending-highlight')).toHaveCount(0);
  });
});

test.describe('live-mode composer — M16 keyboard shortcuts', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('Cmd/Ctrl+Enter submits the composer', async ({ page }) => {
    await openPinComposer(page);
    const ta = page.locator('.crit-live-composer-body');
    await ta.fill('Submit via shortcut');
    await ta.press('Meta+Enter');
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
  });

  test('reply composer in panel honours the same shortcuts', async ({ page }) => {
    // Seed a pin, then open its reply composer in the panel.
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('Top-level pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    const reply = page.locator('#commentsPanelBody .crit-live-comment-reply').first();
    await expect(reply).toBeVisible();
    await reply.click();
    const ta = page.locator('#commentsPanelBody textarea').first();
    await expect(ta).toBeVisible();
    await ta.fill('A reply');
    await ta.press('Meta+Enter');
    await expect(page.locator('#commentsPanelBody textarea')).toHaveCount(0);
  });
});

test.describe('live-mode composer — M17 confirm-before-discard on Esc', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('Esc on empty composer cancels immediately', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').focus();
    await page.keyboard.press('Escape');
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
  });

  test('Esc on dirty composer triggers confirm and respects user choice', async ({ page }) => {
    await openPinComposer(page);
    const ta = page.locator('.crit-live-composer-body');
    await ta.fill('partial draft');

    // Decline confirm — composer must remain.
    page.once('dialog', async (d) => { await d.dismiss(); });
    await ta.press('Escape');
    await expect(page.locator('.crit-live-composer')).toBeVisible();

    // Accept confirm — composer closes, highlight cleared.
    page.once('dialog', async (d) => { await d.accept(); });
    await ta.press('Escape');
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-live-pending-highlight')).toHaveCount(0);
  });
});

test.describe('live-mode reply edit / delete — parity with code-review', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  // Seed pin + reply by driving the UI. Uses `button.crit-live-comment-reply`
  // (the action-bar Reply button) and `.crit-live-comment-replies
  // .crit-live-comment-reply` (the rendered reply row) to avoid the dual
  // meaning of `.crit-live-comment-reply` (matches both).
  async function seedPinWithReply(page: import('@playwright/test').Page, replyBody: string) {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('Top-level pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await row.locator('button.crit-live-comment-reply').click();
    const ta = page.locator('.crit-live-reply-textarea').first();
    await ta.fill(replyBody);
    await ta.press('Meta+Enter');
    await expect(page.locator('.crit-live-reply-textarea')).toHaveCount(0);
  }

  test('clicking the reply Edit button opens an inline textarea + Save persists', async ({ page }) => {
    await seedPinWithReply(page, 'Original reply');
    const replyRow = page.locator('#commentsPanelBody .crit-live-comment-replies .crit-live-comment-reply').first();
    await expect(replyRow).toBeVisible();
    // Hover-only chrome — the button is in DOM but visibility may be hover-gated.
    // dispatch click directly to bypass hover gating.
    await replyRow.locator('.crit-live-reply-edit').dispatchEvent('click');
    const editTa = page.locator('textarea.crit-live-reply-edit-textarea');
    await expect(editTa).toBeVisible();
    await editTa.fill('Edited reply body');
    await page.locator('#commentsPanelBody .crit-live-comment-replies .crit-live-comment-reply button', { hasText: 'Save' }).click();
    // After refresh, the new body is rendered.
    await expect(page.locator('#commentsPanelBody .reply-body').filter({ hasText: 'Edited reply body' })).toBeVisible();
  });

  test('clicking the reply Delete button removes the reply', async ({ page }) => {
    await seedPinWithReply(page, 'Doomed reply');
    const repliesContainer = page.locator('#commentsPanelBody .crit-live-comment-replies');
    await expect(repliesContainer.locator('.crit-live-comment-reply')).toHaveCount(1);
    await repliesContainer.locator('.crit-live-reply-delete').first().dispatchEvent('click');
    await expect(repliesContainer.locator('.crit-live-comment-reply')).toHaveCount(0);
  });
});

test.describe('live-mode comments-panel resizer — DOM order', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('resizer sits adjacent to comments panel (right of iframe pane)', async ({ page }) => {
    await openPinComposer(page); // ensures /live loaded + agent ready
    await page.locator('.crit-live-composer-cancel').click();
    // Resizer's next sibling must be the comments panel — otherwise the
    // handle is stranded on the wrong edge of the iframe pane and feels
    // un-draggable.
    const order = await page.evaluate(() => {
      const r = document.getElementById('commentsPanelResizer');
      const next = r && r.nextElementSibling;
      return next ? next.id || next.className : null;
    });
    expect(order).toContain('commentsPanel');
  });
});

// Re-export PIN_TARGET so an unused-import lint never trips this spec.
void PIN_TARGET;
