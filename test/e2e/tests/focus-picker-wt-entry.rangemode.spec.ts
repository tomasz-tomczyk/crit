import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments } from './helpers';

// In working-tree focus the stack chip (and its embedded ✕ exit button)
// must be hidden. The "Resume PR" pill is the inverse — it shows here
// when the session has a stashed last range focus.
//
// Entry into range mode from WT (when no last range exists) is a
// deliberate gap: the CLI is the only entry point (`crit --pr <N>` or
// `crit --range A..B`). See printHelp().

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
  await request.post('/api/focus', { data: { kind: 'working_tree' } });
});

test('stack chip is hidden in working-tree focus', async ({ page }) => {
  await loadPage(page);
  await expect(page.locator('#stackChip')).toBeHidden();
});

test('stack chip exit ✕ is hidden in working-tree focus', async ({ page }) => {
  await loadPage(page);
  await expect(page.locator('#stackChipExit')).toBeHidden();
});
