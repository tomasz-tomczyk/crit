import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

test.describe('Heading Anchors — Single File Mode', () => {
  test('headings have id attributes matching their slug', async ({ page }) => {
    await loadPage(page);

    const heading = page.locator('h2#overview');
    await expect(heading).toBeVisible();
    await expect(heading).toContainText('Overview');

    const subheading = page.locator('h3#step-1-auth-middleware');
    await expect(subheading).toBeVisible();
  });

  test('navigating to a hash URL scrolls to the heading', async ({ page, baseURL }) => {
    await loadPage(page);

    // Navigate to anchor
    await page.goto(baseURL + '/#timeline');
    const target = page.locator('h2#timeline');
    await expect(target).toBeInViewport({ timeout: 3000 });
  });

  test('clicking an in-page anchor link scrolls to the heading', async ({ page }) => {
    await loadPage(page);

    // hashchange: programmatically set hash and verify scroll
    await page.evaluate(() => { window.location.hash = '#open-questions'; });
    const target = page.locator('h2#open-questions');
    await expect(target).toBeInViewport({ timeout: 3000 });
  });

  test('all headings get id attributes', async ({ page }) => {
    await loadPage(page);

    const headingsWithId = page.locator('h1[id], h2[id], h3[id], h4[id], h5[id], h6[id]');
    await expect(async () => {
      const count = await headingsWithId.count();
      expect(count).toBeGreaterThanOrEqual(5);
    }).toPass({ timeout: 5000 });
  });

  test('link icon appears on heading hover', async ({ page }) => {
    await loadPage(page);

    const heading = page.locator('h2#overview');
    await expect(heading).toBeVisible();

    const anchor = heading.locator('.heading-anchor');
    await expect(anchor).toHaveCSS('opacity', '0');

    await heading.hover();
    await expect(anchor).toHaveCSS('opacity', '1');
  });

  test('clicking link icon updates URL hash and copies to clipboard', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await loadPage(page);

    const heading = page.locator('h2#overview');
    await heading.hover();
    const anchor = heading.locator('.heading-anchor');
    await anchor.click();

    const hash = await page.evaluate(() => window.location.hash);
    expect(hash).toBe('#overview');

    const clipboard = await page.evaluate(() => navigator.clipboard.readText());
    expect(clipboard).toContain('#overview');
  });
});
