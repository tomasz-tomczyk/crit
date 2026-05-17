import { test, expect } from '@playwright/test';
import {
  clearAllLivePins,
  enterPinMode,
  getIframe,
  openPinComposer,
  setIframeRoute,
  waitForAgentReady,
} from './livemode-helpers';

// Tests for the in-iframe crit-agent.js — boot signal, origin guard, mode flip,
// hover overlay, click capture/suppression, selection emission, screenshot
// fallback, focus-state, ancestor menu, and the agent-error toast surface.
//
// Selectors reflect the actual production DOM (frontend/crit-agent.js +
// frontend/live-mode.js). The original Phase F skeletons targeted a wrapper
// (iframe.crit-live-iframe-frame) instead of the iframe itself
// (#critLiveIframe); we use the helpers' getIframe() throughout.

test.describe('live-mode agent — boot + handshake', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('agent posts agent-ready on boot', async ({ page }) => {
    await waitForAgentReady(page);
    // Agent-ready in the chrome's message log; chrome unlocks the Pin button.
    await expect(page.locator('#liveModeToggle button[data-mode="pin"]')).toBeEnabled();
  });

  test('agent rejects inbound messages from a foreign origin', async ({ page }) => {
    // crit-agent.js validates ev.origin === expectedApiOrigin and ev.source
    // === window.parent. We can't actually post from a foreign origin in a
    // single-page test, but we can post from the iframe's own window — that
    // fails the source check (parent !== self) and must be ignored.
    await waitForAgentReady(page);
    const iframe = getIframe(page);
    // Capture mode before, post a foreign-source set-mode, verify mode unchanged.
    await iframe.locator('body').evaluate(() => {
      // Posting to self bypasses the parent-source guard and should be dropped.
      window.postMessage({ type: 'set-mode', value: 'pin' }, '*');
    });
    // Allow a microtask flush; mode must remain 'navigate'.
    await expect.poll(
      () => iframe.locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode ?? 'unknown';
      }),
    ).toBe('navigate');
  });

  test('agent posts to the verified API origin, not "*"', async ({ page }) => {
    // The chrome lives at one origin (#critLiveIframe parent); the proxied
    // upstream lives at a different one. The agent reads the API origin from
    // its own <script src=...> URL. We assert that __critAgentState reflects
    // an expectedApiOrigin equal to the chrome's origin (not "*", not null).
    await waitForAgentReady(page);
    const apiOrigin = await getIframe(page).locator('body').evaluate(() => {
      return (window as unknown as { __critAgentState?: { expectedApiOrigin?: string } })
        .__critAgentState?.expectedApiOrigin ?? null;
    });
    const chromeOrigin = new URL(page.url()).origin;
    expect(apiOrigin).toBe(chromeOrigin);
  });

  test('agent flips internal mode on set-mode pin', async ({ page }) => {
    await waitForAgentReady(page);
    await enterPinMode(page);
    // Chrome's toolbar click postMessages set-mode to the iframe; the agent
    // updates its own state. Verify the *agent* (not just the chrome) saw it.
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('pin');
  });
});

test.describe('live-mode agent — pin mode hover + click', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('hover paints outline overlay in pin mode', async ({ page }) => {
    await waitForAgentReady(page);
    await enterPinMode(page);
    // Move pointer onto a stable element; the agent must show #crit-agent-overlay.
    const target = getIframe(page).locator('#primary-btn');
    await target.scrollIntoViewIfNeeded();
    await target.hover();
    const overlay = getIframe(page).locator('#crit-agent-overlay');
    await expect(overlay).toBeAttached();
    await expect.poll(
      () => overlay.evaluate((el) => (el as HTMLElement).style.display !== 'none'),
    ).toBe(true);
  });

  test('click in pin mode posts selection with dom_anchor', async ({ page }) => {
    await waitForAgentReady(page);
    // Reset chrome message log so we can find the freshly-posted selection.
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await enterPinMode(page);
    await getIframe(page).locator('#primary-btn').click();
    // The chrome logs every inbound postMessage from the agent.
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string; dom_anchor?: { pathname?: string } }[] })
          .__critLiveMessages || [];
        return log.find((m) => m.type === 'selection') || null;
      }),
      { timeout: 10_000 },
    ).not.toBeNull();
    const sel = await page.evaluate(() => {
      const log = (window as unknown as { __critLiveMessages?: { type: string; dom_anchor?: { pathname?: string; css_selector?: string; tag_chain?: string[] } }[] })
        .__critLiveMessages || [];
      return log.find((m) => m.type === 'selection') || null;
    });
    expect(sel?.dom_anchor?.pathname).toBe('/');
    expect(typeof sel?.dom_anchor?.css_selector).toBe('string');
    expect(Array.isArray(sel?.dom_anchor?.tag_chain)).toBe(true);
  });
});

test.describe('live-mode agent — pin-mode interaction suppression', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('pointerdown on a draggable element is preventDefault-ed in pin mode', async ({ page }) => {
    await waitForAgentReady(page);
    // Reset the message log BEFORE navigating so the agent-ready check below
    // observes the new iframe's handshake, not a stale one from /.
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await setIframeRoute(page, '/widgets');
    await expect(getIframe(page).locator('#widgets-title')).toBeVisible();
    // Re-handshake against the new document's agent.
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    await enterPinMode(page);
    // Wait for the agent inside /widgets to actually flip to pin mode (the
    // chrome's set-mode postMessage is async).
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('pin');
    // Dispatch a synthetic, cancelable mousedown on the draggable. The agent's
    // capture-phase suppressInPinMode handler must call preventDefault, so the
    // event reports defaultPrevented === true after dispatch. This mirrors
    // production behaviour without relying on browser-internal drag heuristics.
    const defaultPrevented = await getIframe(page).locator('body').evaluate(() => {
      const target = document.getElementById('widgets-draggable');
      if (!target) return false;
      const ev = new MouseEvent('mousedown', { bubbles: true, cancelable: true, button: 0 });
      target.dispatchEvent(ev);
      return ev.defaultPrevented;
    });
    expect(defaultPrevented).toBe(true);
  });

  test('Enter on a focused button does NOT activate it in pin mode', async ({ page }) => {
    await waitForAgentReady(page);
    // Clear chrome message log so the agent-ready poll below observes the
    // /widgets agent's handshake, not a stale one from /.
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await setIframeRoute(page, '/widgets');
    await expect(getIframe(page).locator('#widgets-title')).toBeVisible();
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    await enterPinMode(page);
    // Wait for the agent (not just the chrome's button class) to flip to pin
    // mode — set-mode postMessage is async and suppressKeyboardActivation
    // gates on the agent's own state.mode.
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('pin');
    await getIframe(page).locator('body').evaluate(() => {
      (window as unknown as { __widgetsBtnActivations?: number }).__widgetsBtnActivations = 0;
    });
    const btn = getIframe(page).locator('#widgets-btn');
    await btn.focus();
    await btn.press('Enter');
    const activations = await getIframe(page).locator('body').evaluate(() => {
      return (window as unknown as { __widgetsBtnActivations?: number }).__widgetsBtnActivations ?? -1;
    });
    expect(activations).toBe(0);
  });

  test('typing into an <input> still works in pin mode (suppression carve-out)', async ({ page }) => {
    await waitForAgentReady(page);
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await setIframeRoute(page, '/widgets');
    await expect(getIframe(page).locator('#widgets-title')).toBeVisible();
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    await enterPinMode(page);
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('pin');
    const input = getIframe(page).locator('#widgets-input');
    await input.focus();
    // Type via keyboard so suppressInPinMode's input/textarea carve-out is exercised.
    await page.keyboard.type('hello', { delay: 10 });
    await expect(input).toHaveValue('hello');
  });
});

test.describe('live-mode agent — selection round-trip', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('selection event opens the composer with anchor chip', async ({ page }) => {
    await openPinComposer(page);
    await expect(page.locator('.crit-live-composer')).toBeVisible();
    // Composer's chip carries the accessible name or a derived label, not raw outerHTML.
    const chip = page.locator('.crit-live-composer-chip');
    await expect(chip).toBeVisible();
    await expect(chip).toContainText(/Primary/);
  });

  test('save composer POSTs /api/file/comments with dom_anchor and prepends row', async ({ page }) => {
    await openPinComposer(page);
    const postPromise = page.waitForResponse((r) =>
      r.url().includes('/api/file/comments') && r.request().method() === 'POST',
    );
    await page.locator('.crit-live-composer-body').fill('Pin saved via agent test');
    await page.locator('.crit-live-composer-save').click();
    const resp = await postPromise;
    expect(resp.ok()).toBeTruthy();
    // Body must include the dom_anchor that the agent built.
    const sent = resp.request().postDataJSON() as { dom_anchor?: { pathname?: string }; body?: string };
    expect(sent.dom_anchor?.pathname).toBe('/');
    expect(sent.body).toBe('Pin saved via agent test');
    // Composer closes; row appears in the panel.
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    await expect(page.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(1);
  });

  test('save error shows inline error and does not close composer', async () => {
    test.fixme(true, 'Inline .crit-live-composer-error surface requires forcing a non-ok save response (route mocking against the live-mode dispatch path). Unit test on composer covers the error-display contract.');
  });

  test('cancel composer keeps agent in Pin mode for rapid pinning', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-cancel').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    // Agent still in pin mode (so the next click pins again without re-toggling).
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('pin');
  });
});

test.describe('live-mode agent — right-click ancestor menu', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('right-click in pin mode posts request-ancestor-menu with options', async ({ page }) => {
    await waitForAgentReady(page);
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await enterPinMode(page);
    // The card has multiple ancestors (li.card → ul.card-list → main → body),
    // so the menu's options array must be non-empty.
    const card = getIframe(page).locator('.card').first();
    await card.scrollIntoViewIfNeeded();
    await card.click({ button: 'right' });
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string; options?: unknown[] }[] })
          .__critLiveMessages || [];
        const menu = log.find((m) => m.type === 'request-ancestor-menu');
        return menu ? (menu.options?.length ?? 0) : 0;
      }),
      { timeout: 5_000 },
    ).toBeGreaterThan(0);
  });
});

test.describe('live-mode agent — focus-state protocol', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('agent posts focus-state {in_input:true} on focusin into INPUT and false on focusout', async ({ page }) => {
    await waitForAgentReady(page);
    await setIframeRoute(page, '/widgets');
    await expect(getIframe(page).locator('#widgets-title')).toBeVisible();
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    // Reset log to make the in/out sequence easy to find.
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    const input = getIframe(page).locator('#widgets-input');
    await input.focus();
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string; in_input?: boolean }[] })
          .__critLiveMessages || [];
        return log.find((m) => m.type === 'focus-state' && m.in_input === true) || null;
      }),
    ).not.toBeNull();
    await input.blur();
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string; in_input?: boolean }[] })
          .__critLiveMessages || [];
        return log.find((m) => m.type === 'focus-state' && m.in_input === false) || null;
      }),
    ).not.toBeNull();
  });
});

test.describe('live-mode agent — shadow DOM error surface', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('clicking inside shadow DOM emits agent-error and does not pin', async ({ page }) => {
    await waitForAgentReady(page);
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await setIframeRoute(page, '/widgets');
    await expect(getIframe(page).locator('#widgets-title')).toBeVisible();
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    await enterPinMode(page);
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('pin');
    // Clear the message log so the agent-error we observe is the new one.
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    // Dispatch a click that originates from the inner shadow button. The
    // event's composedPath() includes the shadow target, which the agent uses
    // to detect shadow-DOM membership (elementFromPoint retargets to the host).
    await getIframe(page).locator('body').evaluate(() => {
      const host = document.getElementById('shadow-host') as HTMLElement | null;
      const sr = host?.shadowRoot;
      const btn = sr?.getElementById('shadow-btn') as HTMLElement | null;
      if (!btn) throw new Error('shadow-btn not found');
      const rect = btn.getBoundingClientRect();
      btn.dispatchEvent(new MouseEvent('click', {
        bubbles: true, cancelable: true, composed: true, button: 0,
        clientX: rect.left + rect.width / 2,
        clientY: rect.top + rect.height / 2,
      }));
    });
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string; kind?: string }[] })
          .__critLiveMessages || [];
        return log.find((m) => m.type === 'agent-error' && m.kind === 'shadow-dom') || null;
      }),
      { timeout: 5_000 },
    ).not.toBeNull();
    // No selection event was emitted.
    const sel = await page.evaluate(() => {
      const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
        .__critLiveMessages || [];
      return log.find((m) => m.type === 'selection') || null;
    });
    expect(sel).toBeNull();
  });

  test('shadow-DOM agent-error surfaces a toast in chrome', async ({ page }) => {
    await waitForAgentReady(page);
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await setIframeRoute(page, '/widgets');
    await expect(getIframe(page).locator('#widgets-title')).toBeVisible();
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages;
        return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
      }),
      { timeout: 15_000 },
    ).toBe(true);
    await enterPinMode(page);
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('pin');
    await getIframe(page).locator('body').evaluate(() => {
      const host = document.getElementById('shadow-host') as HTMLElement | null;
      const sr = host?.shadowRoot;
      const btn = sr?.getElementById('shadow-btn') as HTMLElement | null;
      if (!btn) throw new Error('shadow-btn not found');
      const rect = btn.getBoundingClientRect();
      btn.dispatchEvent(new MouseEvent('click', {
        bubbles: true, cancelable: true, composed: true, button: 0,
        clientX: rect.left + rect.width / 2,
        clientY: rect.top + rect.height / 2,
      }));
    });
    // Chrome's handleAgentError pipes agent-error through showToast, which
    // since aaa0600 lives in crit.shared and renders into .mini-toast-host
    // with .mini-toast nodes (unified across code-review + live-mode).
    const toast = page.locator('.mini-toast');
    await expect(toast.first()).toBeVisible({ timeout: 5_000 });
    await expect(toast.first()).toContainText(/shadow-dom/);
  });
});

test.describe('live-mode agent — end-to-end pin flow', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('end-to-end: pin → composer → save → row appears in panel', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('End-to-end pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    await expect(page.locator('#commentsPanelBody .crit-live-comment-row')).toHaveCount(1);
    // And the marker was rendered inside the iframe.
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
  });
});

// Drift tray UI was removed in c40534e. The agent's reanchor capture pathway
// still exists internally (used when a future re-anchor entrypoint ships) but
// is no longer reachable via a UI surface in live mode, so the user-facing
// E2E test for it has been removed alongside the tray. crit-agent.js's
// reanchor state machine remains covered at the unit level in
// frontend/__tests__/live-mode-reanchor-click.test.js.
