import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  const files = session.files || [];

  for (const f of files) {
    const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await commentsRes.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
}

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

// Switch plan.md to document view (defaults to diff in git mode)
async function switchToDocumentView(page: Page) {
  const section = mdSection(page);
  await expect(section).toBeVisible();
  const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  await expect(section.locator('.document-wrapper')).toBeVisible();
}

test.describe('Draft Autosave', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    // Clear any existing drafts
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

    // Wait for debounced save (500ms + buffer)
    await page.waitForTimeout(700);

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

    // Wait for debounced save
    await page.waitForTimeout(700);

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

    // Wait for draft to save
    await page.waitForTimeout(700);

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
    await page.waitForTimeout(700);

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
    await page.waitForTimeout(700);

    // Press Escape
    await textarea.press('Escape');

    // Draft should be cleared
    const draftCount = await page.evaluate(() => {
      return Object.keys(localStorage).filter(k => k.startsWith('crit-draft-')).length;
    });
    expect(draftCount).toBe(0);
  });

  test('stale drafts (>24h) are discarded on load', async ({ page }) => {
    // Manually set a stale draft
    await page.goto('/');
    await page.evaluate(() => {
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
