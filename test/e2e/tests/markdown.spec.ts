import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView } from './helpers';

// Read-only checks on plan.md's rendered document, all from one load.
test.describe('Markdown Rendering — plan.md', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('document view renders headings, tables, code, lists, task items, blockquotes, nested bullets and gutters', async ({ page }) => {
    await loadPage(page);
    const section = await switchToDocumentView(page);

    // renders h1 and h2 headings

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

    // renders tables with th and td elements

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

    // renders code blocks with syntax highlighting

    // Code lines should be visible (per-line rendering of code blocks)
    const codeLines = section.locator('.line-block:has(> .line-content.code-line)');
    await expect(codeLines.first()).toBeVisible();

    // The Go block renders one commentable block per source line
    const funcLine = codeLines.filter({ hasText: 'func authMiddleware' });
    await expect(funcLine).toHaveCount(1);
    await expect(codeLines.filter({ hasText: 'return func(w http.ResponseWriter' })).toHaveCount(1);

    // Syntax highlighting: Shiki token spans carry light/dark token colors
    const tokens = funcLine.locator('code.crit-code span[style*="--diffs-token"]');
    await expect(tokens.first()).toBeVisible();
    // `func` keyword is its own colored token, distinct from the plain text
    await expect(tokens.filter({ hasText: /^func$/ }).first()).toBeVisible();

    // renders ordered lists

    // Ordered list elements
    const olElements = section.locator('ol');
    await expect(olElements.first()).toBeVisible();

    // Verify list items within the ordered list
    const liElements = section.locator('ol li');
    await expect(liElements).not.toHaveCount(0);

    // Check that specific ordered list content is present
    await expect(section.locator('ol li', { hasText: 'Add auth middleware' })).toBeVisible();
    await expect(section.locator('ol li', { hasText: 'Write integration tests' })).toBeVisible();

    // renders task list items with checked and unchecked markers

    // markdown-it renders task list items as <li> with literal [ ] and [x] text
    // At least one unchecked item: "[ ] Create migration..."
    const uncheckedItems = section.locator('li', { hasText: /^\[ \]/ });
    await expect(uncheckedItems.first()).toBeVisible();

    // At least one checked item: "[x] Define key format..."
    const checkedItems = section.locator('li', { hasText: /^\[x\]/ });
    await expect(checkedItems.first()).toBeVisible();

    // renders blockquotes

    // Blockquote element should be visible
    const blockquotes = section.locator('blockquote');
    await expect(blockquotes.first()).toBeVisible();

    // Verify blockquote content from plan.md
    await expect(section.locator('blockquote', { hasText: 'rate-limit' })).toBeVisible();

    // nested bullet items are split into individually-commentable line blocks

    // Each nested bullet from the "Nested Tasks" fixture should produce its own
    // .line-block with a unique data-start-line, so users can comment on each
    // nested item independently. Match by visible text rather than line number,
    // because plan.md line offsets shift with fixture edits.
    const items = [
      'Top alpha',
      'Nested alpha-one',
      'Nested alpha-two',
      'Top beta',
      'Nested beta-one',
      'Deep beta-one-a',
      'Nested beta-two',
      'Top gamma',
    ];

    const startLines = new Set<string>();
    for (const text of items) {
      // Find the .line-block that directly contains this item's bullet text.
      // Use a tight locator: a line-block whose visible text contains the item label.
      const block = section.locator('.line-block', { hasText: text }).first();
      await expect(block).toBeVisible();
      const startLine = await block.getAttribute('data-start-line');
      expect(startLine, `block for "${text}" must expose data-start-line`).not.toBeNull();
      startLines.add(startLine!);
    }

    // Each item lives in its own block — distinct data-start-line values.
    expect(startLines.size).toBe(items.length);

    // The nested wrapper class is present, confirming semantic nesting was preserved.
    await expect(section.locator('.line-block ul.crit-list-wrapper').first()).toBeVisible();

    // line gutters exist in DOM with visible line numbers

    // Line gutters exist in the DOM (needed for comment interaction)
    const lineGutters = section.locator('.line-gutter');
    await expect(lineGutters.first()).toBeVisible();

    // Line numbers are present and visible in document view
    const lineNums = section.locator('.line-gutter .line-num');
    await expect(lineNums.first()).toBeVisible();

    // Line numbers carry valid data attributes for commenting
    const firstLineNumText = await lineNums.first().textContent();
    const firstNum = parseInt(firstLineNumText?.trim() || '0', 10);
    expect(firstNum).toBeGreaterThan(0);
  });
});
