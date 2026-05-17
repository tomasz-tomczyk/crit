import { test, expect } from '@playwright/test';
import { clearAllLivePins, getIframe } from './livemode-helpers';

test.describe('websocket — proxy upgrade (Scenario 14)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('WS opened from iframe origin echoes through proxy', async ({ page }) => {
    await page.goto('/live');
    await expect(getIframe(page).locator('#title')).toBeVisible();

    const result = await getIframe(page).locator('body').evaluate(async () => {
      return new Promise<{ ok: boolean; received?: string; error?: string }>((resolve) => {
        try {
          const ws = new WebSocket(`ws://${location.host}/ws`);
          ws.addEventListener('open', () => ws.send('ping'));
          ws.addEventListener('message', (ev) => {
            ws.close();
            resolve({ ok: true, received: String(ev.data) });
          });
          ws.addEventListener('error', () => resolve({ ok: false, error: 'ws error' }));
          // Hard timeout inside one-shot evaluate (not a state-poll).
          setTimeout(() => resolve({ ok: false, error: 'timeout' }), 5000);
        } catch (e) {
          resolve({ ok: false, error: String(e) });
        }
      });
    });

    expect(result.ok).toBe(true);
    expect(result.received).toBe('ping');
  });
});
