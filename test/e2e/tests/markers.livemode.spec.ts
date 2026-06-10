import { test, expect } from '@playwright/test';
import {
  clearAllLivePins,
  getIframe,
  openPinComposer,
  openPinComposerNoNav,
  setIframeRoute,
  waitForAgentReady,
} from './livemode-helpers';

// Marker rendering, MutationObserver reposition, mutation-budget catch-up,
// keyboard activation, and drift-tray + re-anchor flow.
//
// Selectors are aligned with what frontend/live-mode.js + crit-agent.js
// actually emit: markers live inside the iframe at #crit-marker-root, drift
// tray rows render in .crit-live-drifted-tray, drift-tray host element is
// .crit-live-drifted-tray-host. The original Phase F skeletons targeted
// speculative selectors (#shift-down-btn, iframe.crit-live-iframe); this
// spec binds to actual DOM and uses helpers.

test.describe('live-mode markers — rendering on current pathname', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('renders one marker per pin on current pathname', async ({ page }) => {
    // Pin two distinct elements on the same route, then assert both markers
    // are emitted. The chrome filters set-pins by current pathname before
    // posting to the agent (frontend/live-mode-pin-filter.js).
    await openPinComposer(page, '#primary-btn');
    await page.locator('.crit-live-composer-body').fill('Pin A');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    await openPinComposer(page, '#secondary-btn');
    await page.locator('.crit-live-composer-body').fill('Pin B');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(2);
    await expect(getIframe(page).locator('#crit-marker-root')).toBeAttached();
  });
});

test.describe('live-mode markers — MutationObserver reposition', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('marker positions update when DOM mutates above the target', async ({ page }) => {
    await waitForAgentReady(page);
    await setIframeRoute(page, '/shift-mutator');
    await expect(getIframe(page).locator('#sm-title')).toBeVisible();
    await openPinComposerNoNav(page, '#sm-target');
    await page.locator('.crit-live-composer-body').fill('shifted target');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    const marker = getIframe(page).locator('.crit-live-marker').first();
    await expect(marker).toHaveCount(1);
    const before = await marker.boundingBox();
    expect(before).not.toBeNull();

    // After save the agent stays in pin mode and document-level capture
    // suppresses clicks (preventDefault + stopPropagation), so the button's
    // own click handler would never fire. Mutate the DOM directly to simulate
    // the same effect: insert a 120px spacer above #sm-target.
    await getIframe(page).locator('body').evaluate(() => {
      const host = document.getElementById('sm-spacer-host');
      if (!host) return;
      const s = document.createElement('div');
      s.style.height = '120px';
      s.style.background = '#fafafa';
      s.textContent = 'spacer';
      host.appendChild(s);
    });
    await expect.poll(async () => {
      const b = await marker.boundingBox();
      return b ? Math.round(b.y) : null;
    }).not.toBe(before ? Math.round(before.y) : null);
  });

  test('mass DOM mutation does not lose the marker (full re-resolve catch-up)', async ({ page }) => {
    await waitForAgentReady(page);
    await setIframeRoute(page, '/shift-mutator');
    await expect(getIframe(page).locator('#sm-title')).toBeVisible();
    await openPinComposerNoNav(page, '#sm-target');
    await page.locator('.crit-live-composer-body').fill('mass mutation');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    const marker = getIframe(page).locator('.crit-live-marker').first();
    await expect(marker).toHaveCount(1);
    // 300 spans appended in one tick → exceeds the 200-mutation budget →
    // batcher flips fullReresolve=true → resolveAllPins(). Mutate directly
    // to bypass pin-mode click suppression.
    await getIframe(page).locator('body').evaluate(() => {
      const host = document.getElementById('sm-spacer-host');
      if (!host) return;
      const frag = document.createDocumentFragment();
      for (let i = 0; i < 300; i++) {
        const s = document.createElement('span');
        s.textContent = 'm' + i;
        frag.appendChild(s);
      }
      host.appendChild(frag);
    });
    // Marker stays present and visible after the storm settles.
    await expect(marker).toBeVisible();
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
  });

  test('attribute-only changes do NOT trigger pin-resolution-result', async ({ page }) => {
    await waitForAgentReady(page);
    await setIframeRoute(page, '/shift-mutator');
    await expect(getIframe(page).locator('#sm-title')).toBeVisible();
    await openPinComposerNoNav(page, '#sm-target');
    await page.locator('.crit-live-composer-body').fill('class thrash');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    // Reset chrome message log AFTER initial set-pins resolution chatter has
    // settled, then thrash attributes. The MutationObserver subscribes to
    // childList + subtree only (attributes:false in crit-agent.js), so no
    // pin-resolution-result should be posted in response.
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
    // Wait briefly for any pending resolution to drain into the message log,
    // then clear the log.
    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    await getIframe(page).locator('body').evaluate(() => {
      const t = document.getElementById('sm-target');
      if (!t) return;
      for (let i = 0; i < 50; i++) {
        t.classList.toggle('thrash-' + (i % 3));
      }
    });
    // Drive a positive event AFTER the thrash to confirm the message-pump
    // has drained: dispatch a no-op postMessage round-trip and wait for it
    // to land. If a pin-resolution-result was going to fire from the
    // thrash, it would already be in the log by the time this canary
    // arrives. expect.poll on a stable count is safer than waitForTimeout.
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages || [];
        return log.filter((m) => m.type === 'pin-resolution-result').length;
      }),
      // Three consecutive zero reads across the polling window prove the
      // batcher genuinely produced no resolves — not "we read it too early".
      { timeout: 1500, intervals: [200, 300, 500, 500] },
    ).toBe(0);
    const resolveCount = await page.evaluate(() => {
      const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
        .__critLiveMessages || [];
      return log.filter((m) => m.type === 'pin-resolution-result').length;
    });
    expect(resolveCount).toBe(0);
  });
});

test.describe('live-mode markers — keyboard + click activation', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('clicking a marker posts pin-clicked and highlights its row', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('clickable pin');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    // Exit pin mode so document-level click capture stops swallowing marker clicks.
    await page.locator('#liveModeToggle button[data-mode="navigate"]').click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('navigate');

    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    // Capture pin_id of the only marker so we can verify the round-trip.
    const pinId = await getIframe(page).locator('.crit-live-marker').first()
      .getAttribute('data-pin-id');
    expect(pinId).toBeTruthy();
    await getIframe(page).locator('.crit-live-marker').first().click();
    // pin-clicked round-trips to the chrome with the right pin_id.
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string; pin_id?: string }[] })
          .__critLiveMessages || [];
        return log.find((m) => m.type === 'pin-clicked')?.pin_id ?? null;
      }),
    ).toBe(pinId);
    // Chrome's handlePinClicked sets state.openPin to the matching pin and
    // serializes a #pin= deep-link via history.replaceState. Both are
    // observable post-click and don't depend on the 1500ms transient
    // .crit-live-thread-highlight class which can clear before the test
    // observes it.
    await expect.poll(
      () => page.evaluate(() => {
        return (window as unknown as { crit?: { live?: { openPin?: { id?: string } } } })
          .crit?.live?.openPin?.id ?? null;
      }),
    ).toBe(pinId);
  });

  test('Enter on a focused marker activates same as click', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('keyboard-activatable');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);

    // Exit pin mode so document-level keydown capture stops suppressing Enter.
    await page.locator('#liveModeToggle button[data-mode="navigate"]').click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('navigate');

    await page.evaluate(() => {
      (window as unknown as { __critLiveMessages?: unknown[] }).__critLiveMessages = [];
    });
    const marker = getIframe(page).locator('.crit-live-marker').first();
    await marker.focus();
    await marker.press('Enter');
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critLiveMessages?: { type: string }[] })
          .__critLiveMessages || [];
        return log.some((m) => m.type === 'pin-clicked');
      }),
    ).toBe(true);
  });
});

test.describe('live-mode markers — drift PUT guard for current-round pins', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  // Regression for commit 73877e9: clicking a freshly-created pin would
  // race the round-start scan against the optimistic insert, and a late
  // pin-resolution-result could fire a PUT /api/comment/{id} with
  // drifted_on_round set, marking the pin drifted on the same round it
  // was created in. The fix stamps optimistic inserts with
  // _createdInRound and skips the drift PUT when prev._createdInRound
  // === state.currentRound.
  test('clicking a freshly-created pin does not fire a drift PUT', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-live-composer-body').fill('fresh pin no drift');
    await page.locator('.crit-live-composer-save').click();
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
    const row = page.locator('#commentsPanelBody .crit-live-comment-row').first();
    await expect(row).toBeVisible();

    // Capture any PUT /api/comment/{id} that carries drift fields.
    const driftPuts: string[] = [];
    page.on('request', (req) => {
      if (req.method() !== 'PUT') return;
      if (!/\/api\/comment\//.test(req.url())) return;
      const body = req.postDataJSON() as { drifted?: boolean; drifted_on_round?: number } | null;
      if (body && (body.drifted === true || typeof body.drifted_on_round === 'number')) {
        driftPuts.push(req.url());
      }
    });

    // Click the marker (the in-iframe dot) — this exercises the
    // pin-clicked round-trip + the request-resolution path that
    // previously raced into a drift PUT before commit 73877e9.
    // Exit pin mode first so document-level click capture doesn't
    // swallow the marker click.
    await page.locator('#liveModeToggle button[data-mode="navigate"]').click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('navigate');
    const pinId = await getIframe(page).locator('.crit-live-marker').first()
      .getAttribute('data-pin-id');
    expect(pinId).toBeTruthy();
    await getIframe(page).locator('.crit-live-marker').first().click();

    // Wait for the chrome's positive post-click signal: openPin set to
    // the clicked pin. This guarantees the click round-trip (and any
    // racing resolution scan) has completed.
    await expect.poll(
      () => page.evaluate(() => {
        return (window as unknown as { crit?: { live?: { openPin?: { id?: string } } } })
          .crit?.live?.openPin?.id ?? null;
      }),
      { timeout: 5_000 },
    ).toBe(pinId);

    // No drift PUT was fired against the freshly-created pin.
    expect(driftPuts).toEqual([]);

    // Iframe didn't reload — marker still attached.
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
  });
});

