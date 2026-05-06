import { test, expect } from '@playwright/test';
import {
  clearAllDesignPins,
  enterPinMode,
  getIframe,
  openPinComposer,
  openPinComposerNoNav,
  seedDesignPin,
  setIframeRoute,
  waitForAgentReady,
} from './designmode-helpers';

// Marker rendering, MutationObserver reposition, mutation-budget catch-up,
// keyboard activation, and drift-tray + re-anchor flow.
//
// Selectors are aligned with what frontend/design-mode.js + crit-agent.js
// actually emit: markers live inside the iframe at #crit-marker-root, drift
// tray rows render in .crit-design-drifted-tray, drift-tray host element is
// .crit-design-drifted-tray-host. The original Phase F skeletons targeted
// speculative selectors (#shift-down-btn, iframe.crit-design-iframe); this
// spec binds to actual DOM and uses helpers.

test.describe('design-mode markers — rendering on current pathname', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('renders one marker per pin on current pathname', async ({ page }) => {
    // Pin two distinct elements on the same route, then assert both markers
    // are emitted. The chrome filters set-pins by current pathname before
    // posting to the agent (frontend/design-mode-pin-filter.js).
    await openPinComposer(page, '#primary-btn');
    await page.locator('.crit-design-composer-body').fill('Pin A');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    await openPinComposer(page, '#secondary-btn');
    await page.locator('.crit-design-composer-body').fill('Pin B');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(2);
    await expect(getIframe(page).locator('#crit-marker-root')).toBeAttached();
  });
});

test.describe('design-mode markers — MutationObserver reposition', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('marker positions update when DOM mutates above the target', async ({ page }) => {
    await waitForAgentReady(page);
    await setIframeRoute(page, '/shift-mutator');
    await expect(getIframe(page).locator('#sm-title')).toBeVisible();
    await openPinComposerNoNav(page, '#sm-target');
    await page.locator('.crit-design-composer-body').fill('shifted target');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    const marker = getIframe(page).locator('.crit-design-marker').first();
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
    await page.locator('.crit-design-composer-body').fill('mass mutation');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    const marker = getIframe(page).locator('.crit-design-marker').first();
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
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);
  });

  test('attribute-only changes do NOT trigger pin-resolution-result', async ({ page }) => {
    await waitForAgentReady(page);
    await setIframeRoute(page, '/shift-mutator');
    await expect(getIframe(page).locator('#sm-title')).toBeVisible();
    await openPinComposerNoNav(page, '#sm-target');
    await page.locator('.crit-design-composer-body').fill('class thrash');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    // Reset chrome message log AFTER initial set-pins resolution chatter has
    // settled, then thrash attributes. The MutationObserver subscribes to
    // childList + subtree only (attributes:false in crit-agent.js), so no
    // pin-resolution-result should be posted in response.
    await expect(getIframe(page).locator('.crit-design-marker')).toHaveCount(1);
    // Wait briefly for any pending resolution to drain into the message log,
    // then clear the log.
    await page.evaluate(() => {
      (window as unknown as { __critDesignMessages?: unknown[] }).__critDesignMessages = [];
    });
    await getIframe(page).locator('body').evaluate(() => {
      const t = document.getElementById('sm-target');
      if (!t) return;
      for (let i = 0; i < 50; i++) {
        t.classList.toggle('thrash-' + (i % 3));
      }
    });
    // Give the batcher a frame to drain; assert the log stays empty of resolves.
    await page.waitForTimeout(150); // allow rAF + listeners; no state to wait on
    const resolveCount = await page.evaluate(() => {
      const log = (window as unknown as { __critDesignMessages?: { type: string }[] })
        .__critDesignMessages || [];
      return log.filter((m) => m.type === 'pin-resolution-result').length;
    });
    expect(resolveCount).toBe(0);
  });
});

test.describe('design-mode markers — keyboard + click activation', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('clicking a marker posts pin-clicked and highlights its row', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('clickable pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);
    // Exit pin mode so document-level click capture stops swallowing marker clicks.
    await page.locator('#designModeToggle button[data-mode="navigate"]').click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('navigate');

    await page.evaluate(() => {
      (window as unknown as { __critDesignMessages?: unknown[] }).__critDesignMessages = [];
    });
    // Capture pin_id of the only marker so we can verify the round-trip.
    const pinId = await getIframe(page).locator('.crit-design-marker').first()
      .getAttribute('data-pin-id');
    expect(pinId).toBeTruthy();
    await getIframe(page).locator('.crit-design-marker').first().click();
    // pin-clicked round-trips to the chrome with the right pin_id.
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critDesignMessages?: { type: string; pin_id?: string }[] })
          .__critDesignMessages || [];
        return log.find((m) => m.type === 'pin-clicked')?.pin_id ?? null;
      }),
    ).toBe(pinId);
    // Chrome's handlePinClicked sets state.openPin to the matching pin and
    // serializes a #pin= deep-link via history.replaceState. Both are
    // observable post-click and don't depend on the 1500ms transient
    // .crit-design-thread-highlight class which can clear before the test
    // observes it.
    await expect.poll(
      () => page.evaluate(() => {
        return (window as unknown as { crit?: { design?: { openPin?: { id?: string } } } })
          .crit?.design?.openPin?.id ?? null;
      }),
    ).toBe(pinId);
  });

  test('Enter on a focused marker activates same as click', async ({ page }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('keyboard-activatable');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-composer')).toHaveCount(0);

    // Exit pin mode so document-level keydown capture stops suppressing Enter.
    await page.locator('#designModeToggle button[data-mode="navigate"]').click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { mode?: string } })
          .__critAgentState?.mode;
      }),
    ).toBe('navigate');

    await page.evaluate(() => {
      (window as unknown as { __critDesignMessages?: unknown[] }).__critDesignMessages = [];
    });
    const marker = getIframe(page).locator('.crit-design-marker').first();
    await marker.focus();
    await marker.press('Enter');
    await expect.poll(
      () => page.evaluate(() => {
        const log = (window as unknown as { __critDesignMessages?: { type: string }[] })
          .__critDesignMessages || [];
        return log.some((m) => m.type === 'pin-clicked');
      }),
    ).toBe(true);
  });
});

test.describe('design-mode markers — drift tray', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('drifted-recoverable shows in tray with re-anchor button', async ({ page, request }) => {
    // Selector misses but role+name+landmark resolve to #primary-btn.
    await seedDesignPin(request, 'recoverable pin', {
      pathname: '/',
      css_selector: '#nope',
      tag_chain: ['BUTTON'],
      accessible_name: 'Primary',
      role: 'button',
      landmark: 'main',
      outer_html: '<button>Primary</button>',
    });
    await waitForAgentReady(page);
    const tray = page.locator('.crit-design-drifted-tray');
    await expect(tray).toBeAttached();
    await expect(tray.locator('.crit-design-drifted-badge--recoverable')).toHaveCount(1);
    await expect(tray.locator('.crit-design-reanchor-btn')).toHaveCount(1);
  });

  test('drifted (lost) shows badge but no re-anchor button', async ({ page, request }) => {
    // No fallback fields → resolvePin returns 'drifted'.
    await seedDesignPin(request, 'lost pin', {
      pathname: '/',
      css_selector: '#completely-missing',
      tag_chain: ['DIV'],
      // intentionally no role/name/landmark
    });
    await waitForAgentReady(page);
    const tray = page.locator('.crit-design-drifted-tray');
    await expect(tray).toBeAttached();
    await expect(tray.locator('.crit-design-drifted-badge--lost')).toHaveCount(1);
    await expect(tray.locator('.crit-design-reanchor-btn')).toHaveCount(0);
  });

  test('clicking re-anchor → next iframe click updates pin and tray clears', async ({ page, request }) => {
    const { id } = await seedDesignPin(request, 'recoverable for re-anchor', {
      pathname: '/',
      css_selector: '#nope',
      tag_chain: ['BUTTON'],
      accessible_name: 'Primary',
      role: 'button',
      landmark: 'main',
    });
    void id;
    await waitForAgentReady(page);
    // Enter pin mode FIRST. crit-agent.js's onClickCapture short-circuits when
    // state.mode !== 'pin', so the re-anchor click only registers when the
    // user is already in pin mode. (See onEnterReanchor's intent comment vs.
    // the actual early-return in onClickCapture — production gap.)
    await enterPinMode(page);
    const tray = page.locator('.crit-design-drifted-tray');
    await expect(tray).toBeAttached();
    await tray.locator('.crit-design-reanchor-btn').first().click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { reanchor?: { armed?: boolean } } })
          .__critAgentState?.reanchor?.armed === true;
      }),
    ).toBe(true);
    await getIframe(page).locator('#secondary-btn').click();
    await expect.poll(
      () => page.locator('.crit-design-drifted-tray .crit-design-drifted-row').count(),
      { timeout: 10_000 },
    ).toBe(0);
  });

  test('re-anchor flow updates pin via PUT', async ({ page, request }) => {
    const { id } = await seedDesignPin(request, 'recoverable for PUT', {
      pathname: '/',
      css_selector: '#nope',
      tag_chain: ['BUTTON'],
      accessible_name: 'Primary',
      role: 'button',
      landmark: 'main',
    });
    await waitForAgentReady(page);
    await expect(page.locator('.crit-design-drifted-tray')).toBeAttached();

    // Enter pin mode so the re-anchor click is captured by the agent (see the
    // mode-gating note on the previous test).
    await enterPinMode(page);
    const putPromise = page.waitForResponse((r) =>
      r.url().includes(`/api/comment/${id}`) && r.request().method() === 'PUT',
    );
    await page.locator('.crit-design-reanchor-btn').first().click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { reanchor?: { armed?: boolean } } })
          .__critAgentState?.reanchor?.armed === true;
      }),
    ).toBe(true);
    await getIframe(page).locator('#secondary-btn').click();
    const resp = await putPromise;
    expect(resp.ok()).toBeTruthy();
  });

  test('end-to-end three-status walkthrough: recoverable + lost coexist; recoverable can re-anchor', async ({ page, request }) => {
    // One recoverable + one lost on the same path.
    await seedDesignPin(request, 'recoverable A', {
      pathname: '/',
      css_selector: '#nope-a',
      tag_chain: ['BUTTON'],
      accessible_name: 'Primary',
      role: 'button',
      landmark: 'main',
    });
    await seedDesignPin(request, 'lost B', {
      pathname: '/',
      css_selector: '#nope-b',
      tag_chain: ['SPAN'],
    });
    await waitForAgentReady(page);
    const tray = page.locator('.crit-design-drifted-tray');
    await expect(tray.locator('.crit-design-drifted-badge--recoverable')).toHaveCount(1);
    await expect(tray.locator('.crit-design-drifted-badge--lost')).toHaveCount(1);

    // Enter pin mode so the re-anchor click is captured (see drift-tray
    // tests above for the mode-gating note).
    await enterPinMode(page);
    // Re-anchor the recoverable one.
    await tray.locator('.crit-design-reanchor-btn').first().click();
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => {
        return (window as unknown as { __critAgentState?: { reanchor?: { armed?: boolean } } })
          .__critAgentState?.reanchor?.armed === true;
      }),
    ).toBe(true);
    await getIframe(page).locator('#secondary-btn').click();
    // Recoverable disappears; lost remains (no re-anchor was attempted).
    await expect.poll(
      () => tray.locator('.crit-design-drifted-badge--recoverable').count(),
      { timeout: 10_000 },
    ).toBe(0);
    await expect(tray.locator('.crit-design-drifted-badge--lost')).toHaveCount(1);
  });
});

// Re-export to placate unused-import lint when only some imports are used.
void enterPinMode;
