import { test, expect } from '@playwright/test';
import {
  clearAllDesignPins,
  getIframe,
  openPinComposer,
  NAV_OTHER,
  PIN_TARGET,
} from './designmode-helpers';

test.describe('design-mode pin composer — M11 sustained highlight', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('clicking an element in Pin mode keeps it outlined while composer is open', async ({ page }) => {
    await openPinComposer(page);
    await expect(getIframe(page).locator('.crit-design-pending-highlight')).toHaveCount(1);
  });

  test('Cancel removes the highlight', async ({ page }) => {
    await openPinComposer(page);
    await expect(getIframe(page).locator('.crit-design-pending-highlight')).toHaveCount(1);
    await page.locator('.crit-design-composer-cancel').click();
    await expect(getIframe(page).locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('Escape removes the highlight', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').focus();
    await page.keyboard.press('Escape');
    await expect(getIframe(page).locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('Save removes the highlight after the comment is created', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('A pin comment.');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-design-pending-highlight')).toHaveCount(0);
  });

  test('route change auto-clears the highlight', async ({ page }) => {
    await openPinComposer(page);
    // Switch back to navigate mode so the link click is not suppressed by the agent.
    await page.locator('#designModeToggle button[data-mode="navigate"]').click();
    await getIframe(page).locator(NAV_OTHER).click();
    await expect(page.locator('#designRouteName')).toHaveText('/dashboard');
    await expect(getIframe(page).locator('.crit-design-pending-highlight')).toHaveCount(0);
  });
});

test.describe('design-mode composer — M16 keyboard shortcuts', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('Cmd/Ctrl+Enter submits the composer', async ({ page }) => {
    await openPinComposer(page);
    const ta = page.locator('.crit-design-composer-body');
    await ta.fill('Submit via shortcut');
    await ta.press('Meta+Enter');
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
  });

  test('reply composer in panel honours the same shortcuts', async ({ page }) => {
    // Seed a pin, then open its reply composer in the panel.
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('Top-level pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    const reply = page.locator('#commentsPanelBody .crit-design-comment-reply').first();
    await expect(reply).toBeVisible();
    await reply.click();
    const ta = page.locator('#commentsPanelBody textarea').first();
    await expect(ta).toBeVisible();
    await ta.fill('A reply');
    await ta.press('Meta+Enter');
    await expect(page.locator('#commentsPanelBody textarea')).toHaveCount(0);
  });
});

test.describe('design-mode composer — M17 confirm-before-discard on Esc', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('Esc on empty composer cancels immediately', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').focus();
    await page.keyboard.press('Escape');
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
  });

  test('Esc on dirty composer triggers confirm and respects user choice', async ({ page }) => {
    await openPinComposer(page);
    const ta = page.locator('.crit-design-composer-body');
    await ta.fill('partial draft');

    // Decline confirm — composer must remain.
    page.once('dialog', async (d) => { await d.dismiss(); });
    await ta.press('Escape');
    await expect(page.locator('.crit-design-composer')).toBeVisible();

    // Accept confirm — composer closes, highlight cleared.
    page.once('dialog', async (d) => { await d.accept(); });
    await ta.press('Escape');
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    await expect(getIframe(page).locator('.crit-design-pending-highlight')).toHaveCount(0);
  });
});

// Re-export PIN_TARGET so an unused-import lint never trips this spec.
void PIN_TARGET;
