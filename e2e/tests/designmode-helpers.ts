// Sibling helpers for the design-mode Playwright project.
// Does NOT import from helpers.ts — file-mode helper assumptions don't carry.
//
// Selector contract used here is the *actual* DOM shipped by Phase B/C/D/E
// (frontend/design-mode.js), not the speculative selectors in the original
// Phase F plan. If/when the chrome adopts data-attribute selectors, switch
// these to match.
import { expect, type Page, type APIRequestContext, type FrameLocator } from '@playwright/test';

/**
 * Bulk-delete every comment (design pins are comments). Same endpoint as
 * file-mode helpers' clearAllComments.
 */
export async function clearAllDesignPins(request: APIRequestContext): Promise<void> {
  const resp = await request.delete('/api/comments');
  expect(resp.ok()).toBeTruthy();
}

/**
 * Navigate to /design and wait for the iframe to be present and the agent
 * to post agent-ready (via __critDesignMessages, shipped by Phase B chrome).
 *
 * Note: a concurrent bug-fix round is addressing "browser doesn't auto-open
 * /design" — this helper navigates explicitly so it's unaffected.
 */
export async function waitForAgentReady(page: Page): Promise<void> {
  await page.goto('/design');
  await expect(page.locator('#critDesignIframe')).toBeVisible({ timeout: 15_000 });
  await expect.poll(
    () => page.evaluate(() => {
      const log = (window as unknown as { __critDesignMessages?: { type: string }[] })
        .__critDesignMessages;
      return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
    }),
    { timeout: 15_000 },
  ).toBe(true);
}

/**
 * Toggle Pin mode via the toolbar. Pin button currently uses native
 * `disabled` attr in Phase B — Phase C's enable-on-agent-ready is what
 * makes this clickable. Tests should call waitForAgentReady() first.
 */
export async function enterPinMode(page: Page): Promise<void> {
  const pinBtn = page.locator('#designModeToggle button[data-mode="pin"]');
  await expect(pinBtn).toBeVisible();
  await expect(pinBtn).toBeEnabled();
  await pinBtn.click();
  await expect(pinBtn).toHaveClass(/active/);
}

/** Inverse of enterPinMode. */
export async function exitPinMode(page: Page): Promise<void> {
  const navBtn = page.locator('#designModeToggle button[data-mode="navigate"]');
  await navBtn.click();
  await expect(navBtn).toHaveClass(/active/);
}

/** Convenience: returns the iframe FrameLocator the chrome embeds. */
export function getIframe(page: Page): FrameLocator {
  return page.frameLocator('#critDesignIframe');
}

/**
 * Click an element inside the upstream iframe.
 */
export async function clickInIframe(
  page: Page,
  selector: string,
  button: 'left' | 'right' = 'left',
): Promise<void> {
  const target = getIframe(page).locator(selector);
  await target.scrollIntoViewIfNeeded();
  await expect(target).toBeVisible();
  await target.click({ button });
}

/**
 * Returns the count of rendered marker dots inside the iframe. Use only
 * inside `expect.poll(...)` — never as a bare snapshot.
 */
export async function getMarkerCount(page: Page): Promise<number> {
  return getIframe(page).locator('.crit-design-marker').count();
}

/**
 * Issue a fetch from the iframe origin (proxy port) targeting the API
 * port. Used by the security spec to verify CORS blocks unauthed writes.
 */
export async function forgeUnauthedWrite(
  page: Page,
  body: Record<string, unknown>,
  apiOrigin: string,
): Promise<{ ok: boolean; status?: number; error?: string }> {
  return getIframe(page).locator('body').evaluate(async (_body, args) => {
    try {
      const r = await fetch(`${args.apiOrigin}/api/file/comments?path=/`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(args.body),
      });
      return { ok: r.ok, status: r.status };
    } catch (e) {
      return { ok: false, error: String(e) };
    }
  }, { apiOrigin, body });
}

/** Click a viewport preset button. */
export async function setViewportPreset(
  page: Page,
  preset: 'mobile' | 'tablet' | 'desktop' | 'fit',
): Promise<void> {
  await page.locator(`button[data-viewport="${preset}"]`).click();
  await expect(page.locator(`button[data-viewport="${preset}"]`)).toHaveClass(/active/);
}

/**
 * Set the iframe's route by replacing the path component of its current
 * src. Uses URL ctor to avoid regex pitfalls.
 */
export async function setIframeRoute(page: Page, path: string): Promise<void> {
  await page.evaluate((p) => {
    const ifr = document.getElementById('critDesignIframe') as HTMLIFrameElement | null;
    if (!ifr) throw new Error('#critDesignIframe not found');
    ifr.src = new URL(p, ifr.src).toString();
  }, path);
  // Wait for navigation to settle.
  await expect.poll(
    () => page.evaluate((p) => {
      const ifr = document.getElementById('critDesignIframe') as HTMLIFrameElement | null;
      return ifr?.src.endsWith(p) ?? false;
    }, path),
    { timeout: 5_000 },
  ).toBe(true);
}

/**
 * Navigate the iframe by clicking a link inside it; wait for the chrome's
 * route name to update.
 */
export async function navigateInIframe(
  page: Page,
  selector: string,
  expectedRoute: string,
): Promise<void> {
  await getIframe(page).locator(selector).click();
  await expect(page.locator('#designRouteName')).toHaveText(expectedRoute, { timeout: 10_000 });
}

/**
 * Default pin-target selector inside the upstream fixture root page (`/`).
 * Buttons in the fixture's main: #primary-btn, #secondary-btn, .card.
 */
export const PIN_TARGET = '#primary-btn';

/** Default cross-route navigation selector inside the upstream fixture root. */
export const NAV_OTHER = '#dash-link';

/**
 * Open the pin composer by entering Pin mode and clicking the given iframe
 * selector. Waits for the agent-ready handshake first.
 */
export async function openPinComposer(
  page: Page,
  selector: string = PIN_TARGET,
): Promise<void> {
  await waitForAgentReady(page);
  await openPinComposerNoNav(page, selector);
}

/**
 * Seed a design pin directly via the API. Used to bypass the iframe-mediated
 * pin flow when the test needs a pre-existing pin (e.g. drift-tray scenarios
 * that depend on resolution results against a specific DOM state).
 *
 * Pass css_selector / tag_chain / role+name+landmark to control the
 * resolution outcome:
 *   - a selector that matches and a tag_chain that verifies → resolved
 *   - a selector that misses but role+name+landmark match an element → drifted-recoverable
 *   - everything misses → drifted (lost)
 */
export async function seedDesignPin(
  request: APIRequestContext,
  body: string,
  anchor: {
    pathname: string;
    css_selector: string;
    tag_chain: string[];
    accessible_name?: string;
    role?: string;
    landmark?: string;
    outer_html?: string;
    viewport_width?: number;
    viewport_height?: number;
  },
): Promise<{ id: string }> {
  const fullAnchor = {
    accessible_name: '',
    role: '',
    landmark: '',
    outer_html: '',
    viewport_width: 1280,
    viewport_height: 800,
    ...anchor,
  };
  const resp = await request.post(
    `/api/file/comments?path=${encodeURIComponent(anchor.pathname)}`,
    {
      data: {
        start_line: 0,
        end_line: 0,
        body,
        dom_anchor: fullAnchor,
      },
    },
  );
  if (!resp.ok()) {
    throw new Error(`seedDesignPin failed: ${resp.status()} ${await resp.text()}`);
  }
  return resp.json() as Promise<{ id: string }>;
}

/**
 * Like openPinComposer but does not navigate. Use when the page is already
 * at /design and you only want to switch to Pin mode and click a target —
 * for example after setIframeRoute() to a non-default route.
 */
export async function openPinComposerNoNav(
  page: Page,
  selector: string = PIN_TARGET,
): Promise<void> {
  await waitAgentReadyAfterRoute(page);
  await enterPinMode(page);
  const target = getIframe(page).locator(selector);
  await target.scrollIntoViewIfNeeded();
  await target.click();
  await expect(page.locator('.crit-design-composer')).toBeVisible();
}

/**
 * Wait for an `agent-ready` to appear in the chrome's message log. Used after
 * setIframeRoute() to a new path, where a fresh agent boots and re-handshakes.
 *
 * Callers that need to disambiguate the new handshake from a stale one should
 * clear `__critDesignMessages` BEFORE setIframeRoute() — see usages in
 * agent.designmode.spec.ts. This helper simply polls for any agent-ready.
 */
export async function waitAgentReadyAfterRoute(page: Page): Promise<void> {
  await expect.poll(
    () => page.evaluate(() => {
      const log = (window as unknown as { __critDesignMessages?: { type: string }[] })
        .__critDesignMessages;
      return Array.isArray(log) && log.some((e) => e.type === 'agent-ready');
    }),
    { timeout: 15_000 },
  ).toBe(true);
}

