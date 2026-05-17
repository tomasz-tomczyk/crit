// Reproduction for the user-reported P0 regression: in pin mode, clicking
// any element on the proxied page does NOT open the comment composer.
//
// This test exists to capture the bug in CI shape and pin down where the
// click flow breaks. It also captures chrome + iframe console errors so
// the diagnostic is in the test artefact, not in a developer's local
// browser.
import { test, expect, type ConsoleMessage } from '@playwright/test';
import {
  clearAllLivePins,
  enterPinMode,
  getIframe,
  waitForAgentReady,
  PIN_TARGET,
} from './livemode-helpers';

test.describe('live-mode pin click — P0 regression repro', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('clicking a pin target opens the composer', async ({ page }) => {
    const consoleMessages: { source: string; type: string; text: string }[] = [];
    const pageErrors: string[] = [];

    // Chrome-side console + uncaught errors.
    page.on('console', (msg: ConsoleMessage) => {
      consoleMessages.push({ source: 'chrome', type: msg.type(), text: msg.text() });
    });
    page.on('pageerror', (err) => {
      pageErrors.push(`chrome: ${err.message}`);
    });
    // Iframe (agent) console + uncaught errors. Playwright fires `frame*`
    // events on Page for every frame; we filter to the live iframe by
    // matching its name once it loads.
    page.on('console', (msg: ConsoleMessage) => {
      const url = msg.location().url || '';
      if (url && !url.includes(page.url())) {
        // Best-effort: anything not from the chrome page is treated as iframe.
        consoleMessages.push({ source: 'iframe?', type: msg.type(), text: `[${url}] ${msg.text()}` });
      }
    });

    await waitForAgentReady(page);

    // Pre-click sanity: __critAgentState should expose mode, attached
    // listeners flag, etc. If the agent didn't bind, we want to see that.
    await enterPinMode(page);

    const stateBeforeClick = await getIframe(page).locator('body').evaluate(() => {
      const w = window as unknown as { __critAgentState?: Record<string, unknown> };
      return w.__critAgentState ?? null;
    });

    // Reset the chrome's message log so we only see what fires from the click.
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });

    const target = getIframe(page).locator(PIN_TARGET);
    await target.scrollIntoViewIfNeeded();
    await target.click();

    // Give the chrome a moment to receive the postMessage and render.
    // We poll the message log rather than sleep.
    const selectionMsg = await page.evaluate(async () => {
      // Wait up to 3s for *any* selection event.
      const log = () => (window as unknown as {
        __critLiveMessages?: { type: string; dom_anchor?: unknown }[]
      }).__critLiveMessages || [];
      const start = Date.now();
      while (Date.now() - start < 3000) {
        const m = log().find((x) => x.type === 'selection');
        if (m) return m;
        await new Promise((r) => setTimeout(r, 50));
      }
      return null;
    });

    // Snapshot all messages received by the chrome for diagnostics.
    const allMessages = await page.evaluate(() => {
      return (window as unknown as { __critLiveMessages?: { type: string }[] })
        .__critLiveMessages || [];
    });

    const composerExists = await page.locator('.crit-live-composer').count();

    // ---------- Diagnostic dump (printed to stderr; survives Playwright list reporter) ----------
    /* eslint-disable no-console */
    console.log('[DIAG] state-before-click:', JSON.stringify(stateBeforeClick));
    console.log('[DIAG] selection-msg:', JSON.stringify(selectionMsg));
    console.log('[DIAG] all-messages:', JSON.stringify(allMessages.map((m) => m.type)));
    console.log('[DIAG] composer-count:', composerExists);
    console.log('[DIAG] console-messages:');
    for (const m of consoleMessages.slice(-30)) {
      console.log(`  [${m.source}/${m.type}] ${m.text}`);
    }
    console.log('[DIAG] page-errors:', pageErrors.join('\n') || '(none)');
    /* eslint-enable no-console */

    // The user-visible contract: pin click opens composer.
    await expect(page.locator('.crit-live-composer')).toBeVisible({ timeout: 5_000 });
  });
});
