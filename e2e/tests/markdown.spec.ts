import { test, expect, type Page } from '@playwright/test';

// Helper: navigate and wait for page load
async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// Helper: scope all selectors to the plan.md file section
function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

// Helper: switch plan.md to document view (in git mode, markdown files default to diff view)
async function switchToDocumentView(page: Page) {
  const section = mdSection(page);
  await expect(section).toBeVisible();

  // Click the "Document" toggle button within the plan.md section header
  const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();

  // Wait for the document wrapper to appear (indicates document view is rendered)
  await expect(section.locator('.document-wrapper')).toBeVisible();
}

test.describe('Markdown Rendering — plan.md', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('renders h1 and h2 headings', async ({ page }) => {
    const section = mdSection(page);

    // h1: "Authentication Plan"
    const h1 = section.locator('h1', { hasText: 'Authentication Plan' });
    await expect(h1).toBeVisible();

    // h2 elements — there should be several
    const h2s = section.locator('h2');
    await expect(h2s).not.toHaveCount(0);

    // Verify specific h2 headings exist
    await expect(section.locator('h2', { hasText: 'Overview' })).toBeVisible();
    await expect(section.locator('h2', { hasText: 'Design Decisions' })).toBeVisible();
    await expect(section.locator('h2', { hasText: 'Implementation Steps' })).toBeVisible();
    await expect(section.locator('h2', { hasText: 'Open Questions' })).toBeVisible();
    await expect(section.locator('h2', { hasText: 'Timeline' })).toBeVisible();
  });

  test('renders tables with th and td elements', async ({ page }) => {
    const section = mdSection(page);

    // Table elements should be present
    const tables = section.locator('table');
    await expect(tables.first()).toBeVisible();

    // Table headers
    const thElements = section.locator('th');
    await expect(thElements).not.toHaveCount(0);

    // Verify specific header columns
    await expect(section.locator('th', { hasText: 'Decision' })).toBeVisible();
    await expect(section.locator('th', { hasText: 'Options' })).toBeVisible();
    await expect(section.locator('th', { hasText: 'Chosen' })).toBeVisible();
    await expect(section.locator('th', { hasText: 'Rationale' })).toBeVisible();

    // Table data cells
    const tdElements = section.locator('td');
    await expect(tdElements).not.toHaveCount(0);

    // Verify specific table content (use exact match to avoid ambiguity)
    await expect(section.getByRole('cell', { name: 'API keys', exact: true })).toBeVisible();
  });

  test('renders code blocks with syntax highlighting', async ({ page }) => {
    const section = mdSection(page);

    // Code lines should be visible (per-line rendering of code blocks)
    const codeLines = section.locator('.line-content.code-line');
    await expect(codeLines.first()).toBeVisible();

    // There should be multiple code lines (the Go code block has ~10 lines)
    const count = await codeLines.count();
    expect(count).toBeGreaterThanOrEqual(5);

    // Syntax highlighting: hljs-* spans should be present within code elements
    const hljsSpans = section.locator('.line-content.code-line [class^="hljs-"]');
    await expect(hljsSpans.first()).toBeVisible();
  });

  test('renders ordered lists', async ({ page }) => {
    const section = mdSection(page);

    // Ordered list elements
    const olElements = section.locator('ol');
    await expect(olElements.first()).toBeVisible();

    // Verify list items within the ordered list
    const liElements = section.locator('ol li');
    await expect(liElements).not.toHaveCount(0);

    // Check that specific ordered list content is present
    await expect(section.locator('ol li', { hasText: 'Add auth middleware' })).toBeVisible();
    await expect(section.locator('ol li', { hasText: 'Write integration tests' })).toBeVisible();
  });

  test('renders task list items with checked and unchecked markers', async ({ page }) => {
    const section = mdSection(page);

    // markdown-it renders task list items as <li> with literal [ ] and [x] text
    // At least one unchecked item: "[ ] Create migration..."
    const uncheckedItems = section.locator('li', { hasText: /^\[ \]/ });
    const uncheckedCount = await uncheckedItems.count();
    expect(uncheckedCount).toBeGreaterThanOrEqual(1);
    await expect(uncheckedItems.first()).toBeVisible();

    // At least one checked item: "[x] Define key format..."
    const checkedItems = section.locator('li', { hasText: /^\[x\]/ });
    const checkedCount = await checkedItems.count();
    expect(checkedCount).toBeGreaterThanOrEqual(1);
    await expect(checkedItems.first()).toBeVisible();
  });

  test('renders blockquotes', async ({ page }) => {
    const section = mdSection(page);

    // Blockquote element should be visible
    const blockquotes = section.locator('blockquote');
    await expect(blockquotes.first()).toBeVisible();

    // Verify blockquote content from plan.md
    await expect(section.locator('blockquote', { hasText: 'rate-limit' })).toBeVisible();
  });

  test('line gutters exist in DOM but line numbers are visually hidden', async ({ page }) => {
    const section = mdSection(page);

    // Line gutters exist in the DOM (needed for comment interaction)
    const lineGutters = section.locator('.line-gutter');
    const gutterCount = await lineGutters.count();
    expect(gutterCount).toBeGreaterThan(0);

    // Line numbers are present but visually hidden in document view
    const lineNums = section.locator('.line-gutter .line-num');
    const numCount = await lineNums.count();
    expect(numCount).toBeGreaterThan(0);
    await expect(lineNums.first()).not.toBeVisible();

    // Line numbers still carry valid data attributes for commenting
    const firstLineNumText = await lineNums.first().textContent();
    const firstNum = parseInt(firstLineNumText?.trim() || '0', 10);
    expect(firstNum).toBeGreaterThan(0);
  });
});
