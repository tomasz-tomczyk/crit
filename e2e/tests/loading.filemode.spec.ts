import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

test.describe('File Mode — Page Loading', () => {
  test('page loads without errors, file sections appear', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('.file-section')).not.toHaveCount(0);
  });

  test('no branch name shown in header (file mode)', async ({ page }) => {
    await loadPage(page);
    const branchContext = page.locator('#branchContext');
    await expect(branchContext).toBeHidden();
  });

  test('document title includes file names (no branch)', async ({ page }) => {
    await loadPage(page);
    // In file mode, title lists the file names instead of a branch
    await expect(page).toHaveTitle(/Crit — .*plan\.md/);
  });

  test('diff mode toggle is hidden in file mode', async ({ page }) => {
    await loadPage(page);
    const diffToggle = page.locator('#diffModeToggle');
    await expect(diffToggle).toBeHidden();
  });

  test('all three files are listed', async ({ page }) => {
    await loadPage(page);
    // In file mode: plan.md, server.go, handler.js (3 files)
    const sections = page.locator('.file-section');
    await expect(sections).toHaveCount(3);
  });

  test('file headers do not show status badge in file mode', async ({ page }) => {
    await loadPage(page);
    // In file mode, status badges are hidden (they only make sense in git mode)
    const badges = page.locator('.file-header-badge');
    await expect(badges).toHaveCount(0);
  });
});

test.describe('File Mode — Markdown Rendering', () => {
  test('markdown file renders in document view by default', async ({ page }) => {
    await loadPage(page);
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
    // In file mode, markdown should default to document view (no diff available)
    const docWrapper = mdSection.locator('.document-wrapper');
    await expect(docWrapper).toBeVisible();
  });

  test('markdown renders headings and content', async ({ page }) => {
    await loadPage(page);
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
    await expect(mdSection.locator('h1', { hasText: 'Authentication Plan' })).toBeVisible();
    await expect(mdSection.locator('h2', { hasText: 'Overview' })).toBeVisible();
  });

  test('markdown blocks have visible line gutters in DOM', async ({ page }) => {
    await loadPage(page);
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const gutters = mdSection.locator('.line-gutter');
    const count = await gutters.count();
    expect(count).toBeGreaterThan(0);
    // Gutters are visible in document view with line numbers
    await expect(gutters.first()).toBeVisible();
  });
});

test.describe('File Mode — Scope Cookie Resilience', () => {
  test('renders files even when scope cookie is set to non-all value', async ({ page }) => {
    // Simulate a user who previously used git mode with a non-"all" scope.
    // The scope cookie persists across sessions; file mode must ignore it.
    await page.context().addCookies([
      { name: 'crit-diff-scope', value: 'staged', domain: 'localhost', path: '/' },
    ]);
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    const sections = page.locator('.file-section');
    await expect(sections).toHaveCount(3);
    // Verify markdown content actually rendered (not just empty sections)
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const docWrapper = mdSection.locator('.document-wrapper');
    await expect(docWrapper).toBeVisible();
    const lineBlocks = mdSection.locator('.line-block');
    const count = await lineBlocks.count();
    expect(count).toBeGreaterThan(0);
  });

  test('renders files even when scope cookie is set to branch', async ({ page }) => {
    await page.context().addCookies([
      { name: 'crit-diff-scope', value: 'branch', domain: 'localhost', path: '/' },
    ]);
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    const sections = page.locator('.file-section');
    await expect(sections).toHaveCount(3);
  });
});

test.describe('File Mode — Code Files', () => {
  test('code files render in document view with line blocks', async ({ page }) => {
    await loadPage(page);
    const jsSection = page.locator('.file-section').filter({ hasText: 'handler.js' });
    const docWrapper = jsSection.locator('.document-wrapper');
    await expect(docWrapper).toBeVisible();
    const lineBlocks = jsSection.locator('.line-block');
    const count = await lineBlocks.count();
    expect(count).toBeGreaterThan(0);
  });

  test('code files have syntax-highlighted content', async ({ page }) => {
    await loadPage(page);
    const jsSection = page.locator('.file-section').filter({ hasText: 'handler.js' });
    // Code lines should contain hljs-highlighted code
    const codeLine = jsSection.locator('.line-content.code-line').first();
    await expect(codeLine).toBeVisible();
    await expect(codeLine.locator('code.hljs')).toBeVisible();
  });

  test('code files have commentable line gutters', async ({ page }) => {
    await loadPage(page);
    const jsSection = page.locator('.file-section').filter({ hasText: 'handler.js' });
    const lineBlock = jsSection.locator('.line-block').first();
    await lineBlock.hover();
    const commentGutter = jsSection.locator('.line-comment-gutter').first();
    await expect(commentGutter).toBeVisible();
  });

  test('go file renders with line numbers', async ({ page }) => {
    await loadPage(page);
    const goSection = page.locator('.file-section').filter({ hasText: 'server.go' });
    const docWrapper = goSection.locator('.document-wrapper');
    await expect(docWrapper).toBeVisible();
    // Check line numbers exist
    const lineNums = goSection.locator('.line-num');
    const count = await lineNums.count();
    expect(count).toBeGreaterThan(0);
  });

  test('markdown file has commentable line gutters in file mode', async ({ page }) => {
    await loadPage(page);
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
    // Hover over a line block to check the + button appears
    const lineBlock = mdSection.locator('.line-block').first();
    await lineBlock.hover();
    const commentGutter = mdSection.locator('.line-comment-gutter').first();
    await expect(commentGutter).toBeVisible();
  });
});
