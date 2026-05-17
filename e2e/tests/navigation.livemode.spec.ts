import { test, expect } from '@playwright/test';
import { clearAllLivePins, getIframe, setIframeRoute } from './livemode-helpers';

test.describe('navigation — link, pushState, redirects (Scenarios 10–13)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('link click in iframe lands on /dashboard', async ({ page }) => {
    await page.goto('/live');
    await expect(getIframe(page).locator('#dash-link')).toBeVisible();
    await getIframe(page).locator('#dash-link').click();
    await expect(getIframe(page).locator('#dash-title')).toBeVisible();
    // Chrome breadcrumb updates via route announcer.
    await expect(page.locator('#liveRouteName')).toHaveText('/dashboard');
  });

  test('pushState on /spa updates breadcrumb to /spa/section', async ({ page }) => {
    await page.goto('/live');
    await setIframeRoute(page, '/spa');
    await expect(getIframe(page).locator('#spa-title')).toBeVisible();
    await getIframe(page).locator('#push-btn').click();
    await expect(page.locator('#liveRouteName')).toHaveText('/spa/section');
  });

  test('same-origin redirect lands on /dashboard via proxy', async ({ page }) => {
    await page.goto('/live');
    await setIframeRoute(page, '/redirect-same');
    // Iframe content lands on /dashboard via proxy's same-origin redirect rewrite.
    await expect(getIframe(page).locator('#dash-title')).toBeVisible();
  });

  test.fixme('chrome breadcrumb reflects post-redirect path', async () => {
    // FIXME: chrome route announcer doesn't update #liveRouteName when the
    // iframe navigates via a server-side redirect from a programmatically-set
    // src. Likely a Phase B announcer wiring gap, not a Phase F infra issue.
  });

  test.fixme('cross-origin redirect surfaces an "open in real browser" affordance', async () => {
    // FIXME: shell.livemode.spec already has fixme'd coverage; the chrome
    // banner for cross-origin redirects is not yet wired (Phase B/E
    // incomplete). When [data-crit-live-cross-origin-banner] ships, replace
    // with assertion against #/redirect-cross.
  });
});
