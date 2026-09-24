import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, treeFiles } from './helpers';

// ============================================================
// No-Git Mode — Git-absence invariants
//
// These tests verify the two things unique to the no-git fixture:
// 1. The session API correctly reports files mode with no branch
// 2. The page loads and renders file sections without git
//
// Broader file-mode behavior is covered by *.filemode.spec.ts on its own
// git-backed fixture. Keep one core review lifecycle here to prove that
// storage and commenting also work when no repository exists at all.
// ============================================================

test.describe('No-Git Mode — Git-absence invariants', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('session API reports files mode with no branch', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();
    expect(session.mode).toBe('files');
    expect(session.branch).toBeFalsy();
  });

  test('page loads and file sections appear', async ({ page }) => {
    await loadPage(page);
    await expect(treeFiles(page)).not.toHaveCount(0);
    await expect(page.locator('#filesContainer .file-section').first()).toBeVisible();
  });

  test('creates and reloads a line comment without a repository', async ({ page, request }) => {
    await loadPage(page);
    const section = await mdSection(page);
    const firstBlock = section.locator('.line-block').first();
    await firstBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = section.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('No-git persisted comment');
    await textarea.press('Control+Enter');
    await expect(section.locator('.comment-form')).toHaveCount(0);
    await expect(
      section.locator('.comment-card', { hasText: 'No-git persisted comment' }),
    ).toHaveCount(1);

    const response = await request.get('/api/file/comments?path=plan.md');
    await expect(response).toBeOK();
    const comments = await response.json() as Array<{
      body: string;
      start_line: number;
      end_line: number;
    }>;
    expect(comments).toEqual([
      expect.objectContaining({
        body: 'No-git persisted comment',
        start_line: 1,
        end_line: 1,
      }),
    ]);

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    const persisted = (await mdSection(page)).locator('.comment-card', { hasText: 'No-git persisted comment' });
    await expect(persisted).toHaveCount(1);
  });

  // The stack breadcrumb and working-tree pill are VCS-aware controls. In
  // no-git mode those concepts don't exist, so both must stay hidden.
  // Regression for: "picker visible in file mode where it has no meaning."
  test('stack breadcrumb is hidden in no-git mode', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#stackBreadcrumb')).toBeHidden();
  });

  test('stack chip ✕ exit is hidden in no-git mode', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#stackChipExit')).toBeHidden();
  });
});
