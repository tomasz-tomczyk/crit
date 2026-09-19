// Regression coverage for #958: opening a live pin from its panel card must
// use the same deferred activation path as a #pin= deep-link. In particular,
// cross-route activation must wait for the new iframe agent before asking it
// to scroll to and highlight the anchored element.
import { test, expect, type Page } from '@playwright/test';
import {
  clearAllLivePins,
  getIframe,
  seedLivePin,
  waitForAgentReady,
} from './livemode-helpers';

async function makeRootTargetRequireScroll(page: Page): Promise<void> {
  const iframe = getIframe(page);
  await iframe.locator('#primary-btn').evaluate((target) => {
    const spacer = document.createElement('div');
    spacer.id = '__crit_pin_card_scroll_spacer';
    spacer.style.height = '1600px';
    target.parentElement?.insertBefore(spacer, target);
    window.scrollTo(0, 0);
  });
  await expect.poll(
    () => iframe.locator('#primary-btn').evaluate((target) => {
      return target.getBoundingClientRect().top > window.innerHeight;
    }),
  ).toBe(true);
}

test.describe('live-mode panel pin-card activation', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('clicking a pin card on the current route scrolls to and highlights its target', async ({ page, request }) => {
    const pin = await seedLivePin(request, 'Root button pin', {
      pathname: '/',
      css_selector: '#primary-btn',
      tag_chain: ['BUTTON'],
      accessible_name: 'Primary',
      role: 'button',
      outer_html: '<button id="primary-btn">Primary</button>',
    });
    await waitForAgentReady(page);
    await makeRootTargetRequireScroll(page);

    const card = page.locator(`.comment-card[data-id="${pin.id}"]`);
    await expect(card).toBeVisible();
    await card.click();

    const target = getIframe(page).locator('#primary-btn');
    await expect(target).toHaveClass(/crit-live-pending-highlight/);
    await expect.poll(
      () => target.evaluate((element) => {
        const rect = element.getBoundingClientRect();
        return rect.top >= 0 && rect.bottom <= window.innerHeight;
      }),
    ).toBe(true);
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => window.scrollY),
    ).toBeGreaterThan(0);
    await expect(target).not.toHaveClass(/crit-live-pending-highlight/, { timeout: 3_000 });
  });

  test('clicking a pin card on another route waits for load, then highlights its target', async ({ page, request }) => {
    const pin = await seedLivePin(request, 'Dashboard bottom action pin', {
      pathname: '/dashboard',
      css_selector: '#dash-bottom-action',
      tag_chain: ['BUTTON'],
      accessible_name: 'Bottom action',
      role: 'button',
      outer_html: '<button id="dash-bottom-action">Bottom action</button>',
    });
    await waitForAgentReady(page);

    const card = page.locator(`.comment-card[data-id="${pin.id}"]`);
    await expect(card).toBeVisible();
    await card.click();

    await expect(page.locator('#liveRouteName')).toHaveText('/dashboard');
    const target = getIframe(page).locator('#dash-bottom-action');
    await expect(target).toHaveClass(/crit-live-pending-highlight/);
    await expect.poll(
      () => target.evaluate((element) => {
        const rect = element.getBoundingClientRect();
        return rect.top >= 0 && rect.bottom <= window.innerHeight;
      }),
    ).toBe(true);
    await expect.poll(
      () => getIframe(page).locator('body').evaluate(() => window.scrollY),
    ).toBeGreaterThan(0);
    await expect(page).toHaveURL(new RegExp(`#pin=${pin.id}$`));
  });

  test('repeated clicks while another route loads keep pin focus pending', async ({ page, request }) => {
    const pin = await seedLivePin(request, 'Repeated dashboard pin', {
      pathname: '/dashboard',
      css_selector: '#dash-title',
      tag_chain: ['H1'],
      accessible_name: 'Dashboard',
      role: 'heading',
      outer_html: '<h1 id="dash-title">Dashboard</h1>',
    });
    await waitForAgentReady(page);

    const card = page.locator(`.comment-card[data-id="${pin.id}"]`);
    await card.dblclick();

    await expect(page.locator('#liveRouteName')).toHaveText('/dashboard');
    await expect(getIframe(page).locator('#dash-title')).toHaveClass(/crit-live-pending-highlight/);
  });

  test('pressing Enter on a pin card activates it like a click', async ({ page, request }) => {
    const pin = await seedLivePin(request, 'Keyboard dashboard pin', {
      pathname: '/dashboard',
      css_selector: '#dash-title',
      tag_chain: ['H1'],
      accessible_name: 'Dashboard',
      role: 'heading',
      outer_html: '<h1 id="dash-title">Dashboard</h1>',
    });
    await waitForAgentReady(page);

    const card = page.locator(`.comment-card[data-id="${pin.id}"]`);
    await expect(card).toHaveAttribute('tabindex', '0');
    await card.focus();
    await expect(card).toBeFocused();
    await card.press('Enter');

    await expect(page.locator('#liveRouteName')).toHaveText('/dashboard');
    await expect(getIframe(page).locator('#dash-title')).toHaveClass(/crit-live-pending-highlight/);
  });
});
