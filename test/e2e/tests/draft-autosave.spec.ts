import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, openDocLineComment } from './helpers';

// Every test gets a fresh browser context, so localStorage starts empty.

// Number of crit-draft-* entries in localStorage.
function draftCount(page: Page): Promise<number> {
  return page.evaluate(() => Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length);
}

test.describe('Draft Autosave', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('typing saves a draft that is restored on reload with a toast', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const textarea = await openDocLineComment(page, doc);
    await textarea.fill('Saved draft for reload');

    // The debounced save lands in localStorage.
    await expect.poll(() => draftCount(page)).toBe(1);
    const draft = await page.evaluate(() => {
      const key = Object.keys(localStorage).find(k => k.startsWith('crit-draft-'));
      return key ? JSON.parse(localStorage.getItem(key)!) : null;
    });
    expect(draft).not.toBeNull();
    expect(draft.body).toBe('Saved draft for reload');
    expect(draft.startLine).toBeGreaterThan(0);
    expect(draft.savedAt).toBeGreaterThan(0);

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // The comment form is open on plan.md with the draft text.
    // CodeView only mounts files near the viewport, so bring plan.md in.
    const restoredSection = await mdSection(page);
    const restoredTextarea = restoredSection.locator('.comment-form textarea');
    await expect(restoredTextarea).toBeVisible();
    await expect(restoredTextarea).toHaveValue('Saved draft for reload');
    await expect(page.locator('.mini-toast')).toBeAttached();
  });

  test('cancel, Escape and submit each clear the draft', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);

    // Cancel
    let textarea = await openDocLineComment(page, doc);
    await textarea.fill('Will be cancelled');
    await expect.poll(() => draftCount(page)).toBe(1);
    await doc.locator('.comment-form .btn-sm:not(.btn-primary)').filter({ hasText: 'Cancel' }).click();
    await expect(doc.locator('.comment-form')).toHaveCount(0);
    await expect.poll(() => draftCount(page)).toBe(0);

    // Escape (a non-empty draft prompts for confirmation)
    textarea = await openDocLineComment(page, doc);
    await textarea.fill('Will be escaped');
    await expect.poll(() => draftCount(page)).toBe(1);
    page.once('dialog', (dialog) => void dialog.accept());
    await textarea.press('Escape');
    await expect(doc.locator('.comment-form')).toHaveCount(0);
    await expect.poll(() => draftCount(page)).toBe(0);

    // Submit
    textarea = await openDocLineComment(page, doc);
    await textarea.fill('Will be submitted');
    await expect.poll(() => draftCount(page)).toBe(1);
    await doc.locator('.comment-form .btn-primary').click();
    await expect(doc.locator('.comment-card')).toBeVisible();
    await expect.poll(() => draftCount(page)).toBe(0);
  });

  test('stale drafts (>24h) are discarded on load while fresh ones are restored', async ({ page }) => {
    // Seed both before the page loads. The fresh draft is the proof that the
    // restore pass ran: once its form is open, a stale one would be too.
    await page.addInitScript(() => {
      // Different lines, so a wrongly restored stale draft adds a second form.
      const base = { filePath: 'plan.md', editingId: null, side: '' };
      localStorage.setItem('crit-draft-stale', JSON.stringify({
        ...base, startLine: 3, endLine: 3, afterBlockIndex: 1, body: 'Old stale draft',
        savedAt: Date.now() - (25 * 60 * 60 * 1000), // 25 hours ago
      }));
      localStorage.setItem('crit-draft-fresh', JSON.stringify({
        ...base, startLine: 1, endLine: 1, afterBlockIndex: 0, body: 'Fresh draft', savedAt: Date.now(),
      }));
    });

    await loadPage(page);
    const section = await mdSection(page);
    await expect(section.locator('.comment-form textarea')).toHaveValue('Fresh draft');
    await expect(page.locator('.comment-form')).toHaveCount(1);
    await expect.poll(() => page.evaluate(() => localStorage.getItem('crit-draft-stale'))).toBeNull();
  });
});
