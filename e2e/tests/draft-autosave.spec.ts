import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView } from './helpers';

test.describe('Draft Autosave', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    // Navigate once to clear any existing drafts from localStorage
    await page.goto('/');
    await page.evaluate(() => {
      const keys = Object.keys(localStorage).filter(k => k.startsWith('crit-draft-'));
      keys.forEach(k => localStorage.removeItem(k));
    });
  });

  test('typing in comment form saves draft to localStorage', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
    const section = mdSection(page);

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Draft comment text');

    // Poll for debounced save to localStorage
    await expect(async () => {
      const keys = await page.evaluate(() =>
        Object.keys(localStorage).filter(k => k.startsWith('crit-draft-'))
      );
      expect(keys.length).toBeGreaterThan(0);
    }).toPass({ timeout: 3000 });

    // Check localStorage
    const draft = await page.evaluate(() => {
      const keys = Object.keys(localStorage).filter(k => k.startsWith('crit-draft-'));
      if (keys.length === 0) return null;
      return JSON.parse(localStorage.getItem(keys[0])!);
    });

    expect(draft).not.toBeNull();
    expect(draft.body).toBe('Draft comment text');
    expect(draft.startLine).toBeGreaterThan(0);
    expect(draft.savedAt).toBeGreaterThan(0);
  });

  test('draft is restored on page reload with toast notification', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
    const section = mdSection(page);

    // Open comment form and type
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Saved draft for reload');

    // Poll for debounced save to localStorage
    await expect(async () => {
      const keys = await page.evaluate(() =>
        Object.keys(localStorage).filter(k => k.startsWith('crit-draft-'))
      );
      expect(keys.length).toBeGreaterThan(0);
    }).toPass({ timeout: 3000 });

    // Reload the page
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // The comment form should be open with the draft text
    const restoredTextarea = page.locator('.comment-form textarea');
    await expect(restoredTextarea).toBeVisible({ timeout: 3000 });
    await expect(restoredTextarea).toHaveValue('Saved draft for reload');

    // Mini-toast should appear
    const toast = page.locator('.mini-toast');
    await expect(toast).toBeAttached();
  });

  test('submitting comment clears the draft', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
    const section = mdSection(page);

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Will be submitted');

    // Poll for draft to save
    await expect(async () => {
      const keys = await page.evaluate(() =>
        Object.keys(localStorage).filter(k => k.startsWith('crit-draft-'))
      );
      expect(keys.length).toBeGreaterThan(0);
    }).toPass({ timeout: 3000 });

    // Verify draft exists
    let draftCount = await page.evaluate(() => {
      return Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length;
    });
    expect(draftCount).toBe(1);

    // Submit the comment
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Draft should be cleared
    draftCount = await page.evaluate(() => {
      return Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length;
    });
    expect(draftCount).toBe(0);
  });

  test('cancelling comment clears the draft', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
    const section = mdSection(page);

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Will be cancelled');
    // Poll for debounced save to localStorage
    await expect(async () => {
      const keys = await page.evaluate(() =>
        Object.keys(localStorage).filter(k => k.startsWith('crit-draft-'))
      );
      expect(keys.length).toBeGreaterThan(0);
    }).toPass({ timeout: 3000 });

    // Cancel the form
    await page.locator('.comment-form .btn-sm:not(.btn-primary)').filter({ hasText: 'Cancel' }).click();

    // Draft should be cleared
    const draftCount = await page.evaluate(() => {
      return Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length;
    });
    expect(draftCount).toBe(0);
  });

  test('pressing Escape clears the draft', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
    const section = mdSection(page);

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Will be escaped');
    // Poll for debounced save to localStorage
    await expect(async () => {
      const keys = await page.evaluate(() =>
        Object.keys(localStorage).filter(k => k.startsWith('crit-draft-'))
      );
      expect(keys.length).toBeGreaterThan(0);
    }).toPass({ timeout: 3000 });

    // Press Escape
    await textarea.press('Escape');

    // Draft should be cleared
    const draftCount = await page.evaluate(() => {
      return Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length;
    });
    expect(draftCount).toBe(0);
  });

  test('stale drafts (>24h) are discarded on load', async ({ page }) => {
    // Seed a stale draft before the page loads via addInitScript
    await page.addInitScript(() => {
      localStorage.setItem('crit-draft-plan.md', JSON.stringify({
        filePath: 'plan.md',
        startLine: 1,
        endLine: 1,
        afterBlockIndex: 0,
        editingId: null,
        side: '',
        body: 'Old stale draft',
        savedAt: Date.now() - (25 * 60 * 60 * 1000) // 25 hours ago
      }));
    });

    await loadPage(page);

    // Comment form should NOT be open (stale draft discarded)
    await expect(page.locator('.comment-form')).toHaveCount(0);

    // Draft should be removed from localStorage
    const draftCount = await page.evaluate(() => {
      return Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length;
    });
    expect(draftCount).toBe(0);
  });
});
