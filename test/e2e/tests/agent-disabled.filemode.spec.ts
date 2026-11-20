import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

// The file-mode fixture has no agent_cmd, so none of the agent controls show
// and Ctrl+Shift+Enter is a plain Comment or Reply.
test.describe('Without an agent', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('no Send now, and Ctrl+Shift+Enter only saves', async ({ page }) => {
    let agentRequests = 0;
    page.on('request', (req) => {
      if (req.url().includes('/api/agent/request')) agentRequests++;
    });
    await loadPage(page);
    const section = await mdSection(page);

    await section.locator('.line-block').first().hover();
    await section.locator('.line-comment-gutter').first().click();
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Saved without an agent');
    await expect(page.locator('.comment-form .btn-agent')).toHaveCount(0);
    await textarea.press('Control+Shift+Enter');

    const card = section.locator('.comment-card', { hasText: 'Saved without an agent' });
    await expect(card).toBeVisible();

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Reply without an agent');
    await expect(card.locator('.reply-form .btn-agent')).toHaveCount(0);
    await card.locator('.reply-textarea').press('Control+Shift+Enter');

    await expect(card.locator('.comment-reply')).toHaveCount(1);
    await expect(card.locator('.live-thread-badge')).toHaveCount(0);
    await page.waitForTimeout(300);
    expect(agentRequests).toBe(0);
  });

  test('the shortcuts list leaves out Send now', async ({ page }) => {
    await loadPage(page);
    await page.locator('#settingsToggle').click();
    await page.locator('#tab-shortcuts').click();
    await expect(page.locator('#shortcutsPane')).toContainText('Comment');
    await expect(page.locator('#shortcutsPane')).not.toContainText('send to the agent');
  });
});
