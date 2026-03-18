import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// ============================================================
// No-Git Mode — Git-absence invariants
//
// These tests verify the two things unique to the no-git fixture:
// 1. The session API correctly reports files mode with no branch
// 2. The page loads and renders file sections without git
//
// All other file-mode behaviors (no branch header, no diff toggle,
// document view defaults, etc.) are already covered by *.filemode.spec.ts
// tests which also run against this fixture.
// ============================================================

test.describe('No-Git Mode — Git-absence invariants', () => {
  test('session API reports files mode with no branch', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();
    expect(session.mode).toBe('files');
    expect(session.branch).toBeFalsy();
  });

  test('page loads and file sections appear', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('.file-section')).not.toHaveCount(0);
  });
});
