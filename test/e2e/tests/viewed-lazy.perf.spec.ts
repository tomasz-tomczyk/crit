import { test, expect, type Page } from '@playwright/test';
import * as fs from 'fs';
import { createHash } from 'crypto';
import { clearAllComments, loadPage, roundComplete, sessionCwd } from './helpers';

// Files past the eager load limit (25) have no content hash until they load.
// A viewed mark on one must still go stale when the file changes on disk, and
// the page must notice on load without expanding (loading) the file.
// file99.go sorts after the first 25 files of the 301-file perf fixture.
const LAZY_FILE = 'file99.go';

function treeFile(page: Page, filePath: string) {
  return page.locator(`.tree-file[data-tree-path="${filePath}"]`);
}

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test('viewed mark on a lazy file resets when its content changes', async ({ page, request }) => {
  const fixtureDir = await sessionCwd(request);
  const filePath = `${fixtureDir}/${LAZY_FILE}`;
  const original = fs.readFileSync(filePath, 'utf8');

  const diffLoads: string[] = [];
  page.on('request', (r) => {
    const url = new URL(r.url());
    if (url.pathname === '/api/file/diff' && url.searchParams.get('path') === LAZY_FILE) diffLoads.push(LAZY_FILE);
  });

  try {
    // Seed the mark through the API, as a previous page load (or daemon) would
    // have saved it, so the file never loads and stays lazy on the server.
    const hash = 'sha256:' + createHash('sha256').update(original).digest('hex');
    const put = await request.put('/api/viewed', {
      data: { files: [{ path: LAZY_FILE, viewed: true, auto: false, file_hash: hash, head_sha: '' }] },
    });
    expect(put.ok()).toBeTruthy();

    // The mark holds while the content is unchanged.
    await loadPage(page);
    await expect(treeFile(page, LAZY_FILE)).toHaveClass(/viewed/);

    fs.writeFileSync(filePath, original + '\n// changed after it was viewed\n');
    await roundComplete(request);

    // The page must drop the stale mark from the file list alone, without
    // loading (expanding) the file.
    diffLoads.length = 0;
    await page.reload();
    await loadPage(page);
    await expect(treeFile(page, LAZY_FILE)).not.toHaveClass(/viewed/);
    expect(diffLoads).toHaveLength(0);
  } finally {
    fs.writeFileSync(filePath, original);
    await roundComplete(request);
  }
});
