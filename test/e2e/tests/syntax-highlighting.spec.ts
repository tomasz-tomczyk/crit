import { test, expect, type Locator } from '@playwright/test';
import {
  loadPage, goSection, jsSection, revealFile, diffLine, showLine,
  clickWhenHittable, setDiffStyle,
} from './helpers';

// ============================================================
// Syntax Highlighting in Diff Views
//
// Diffs are highlighted by Shiki inside Pierre: each token is a span whose
// inline style carries --diffs-token-light / --diffs-token-dark. A line is
// "highlighted" when its tokens carry more than one distinct colour — a
// plain-text fallback renders a single colour (or no token spans at all).
// ============================================================

const TOKEN = 'span[style*="--diffs-token-light"]';

// Distinct light/dark token colours in a line, plus the distinct computed
// text colours actually painted for them.
async function tokenColours(line: Locator) {
  return line.evaluate((el, sel) => {
    const spans = Array.from(el.querySelectorAll(sel)) as HTMLElement[];
    const light = new Set(spans.map(s => s.style.getPropertyValue('--diffs-token-light').trim()));
    const dark = new Set(spans.map(s => s.style.getPropertyValue('--diffs-token-dark').trim()));
    const painted = new Set(spans.filter(s => (s.textContent || '').trim()).map(s => getComputedStyle(s).color));
    return { tokens: spans.length, light: light.size, dark: dark.size, painted: painted.size };
  }, TOKEN);
}

async function expectHighlighted(line: Locator, minColours = 2) {
  await expect(line).toBeVisible();
  await expect.poll(async () => {
    const c = await tokenColours(line);
    return Math.min(c.light, c.dark, c.painted);
  }, { message: 'distinct token colours in line' }).toBeGreaterThanOrEqual(minColours);
}

// A token span with this exact text.
function token(line: Locator, text: string): Locator {
  return line.locator(TOKEN).filter({ hasText: new RegExp(`^\\s*${text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s*$`) }).first();
}

async function colourOf(t: Locator) {
  return t.evaluate(el => (el as HTMLElement).style.getPropertyValue('--diffs-token-light').trim());
}

test.describe('Syntax Highlighting — Split Mode', () => {
  test('Go file has syntax-highlighted code in split diff', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    // new 24: func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    const line = diffLine(item, 24);
    await showLine(page, line);
    await expect(line).toHaveAttribute('data-line-type', 'change-addition');
    await expectHighlighted(line, 3);
    // The keyword is coloured differently from the function name.
    expect(await colourOf(token(line, 'func'))).not.toBe(await colourOf(token(line, 'authMiddleware')));
  });

  test('JavaScript file has syntax-highlighted code in split diff', async ({ page }) => {
    await loadPage(page);
    const item = await jsSection(page);

    // line 2: export function handleNotification(req, res) {
    const line = diffLine(item, 2);
    await expect(line).toHaveAttribute('data-line-type', 'change-addition');
    await expectHighlighted(line, 3);
    expect(await colourOf(token(line, 'function'))).not.toBe(await colourOf(token(line, 'handleNotification')));
  });

  test('old side (deletion) lines also have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    // old 23: fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
    const line = diffLine(item, 23, 'old');
    await showLine(page, line);
    await expect(line).toHaveAttribute('data-line-type', 'change-deletion');
    await expectHighlighted(line);
  });

  test('context lines have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    // new 12 (context): func respondJSON(w http.ResponseWriter, status int, body string) {
    const line = diffLine(item, 12);
    await showLine(page, line);
    await expect(line).toHaveAttribute('data-line-type', /^context/);
    await expectHighlighted(line, 3);
    await expectHighlighted(diffLine(item, 9, 'old'), 3);
  });
});

test.describe('Syntax Highlighting — language detection', () => {
  test('Gherkin .feature file gets syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const item = await revealFile(page, 'login.feature');

    // line 1: Feature: User login — the keyword is tokenized apart from the title.
    const line = diffLine(item, 1);
    await expect(line).toHaveAttribute('data-line-type', 'change-addition');
    await expectHighlighted(line);
    expect(await colourOf(token(line, 'Feature'))).not.toBe(await colourOf(token(line, 'User login')));
  });
});

test.describe('Syntax Highlighting — Unified Mode', () => {
  test('Go file has syntax-highlighted code in unified diff', async ({ page }) => {
    await loadPage(page);
    await setDiffStyle(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();

    const line = diffLine(item, 24);
    await showLine(page, line);
    await expect(line).toHaveAttribute('data-line-type', 'change-addition');
    await expectHighlighted(line, 3);
  });

  test('deletion lines in unified mode have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    await setDiffStyle(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();

    const line = diffLine(item, 23, 'old');
    await showLine(page, line);
    await expect(line).toHaveAttribute('data-line-type', 'change-deletion');
    await expectHighlighted(line);
  });
});

test.describe('Syntax Highlighting — Expanded Context', () => {
  test('expanded context lines get syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const item = await revealFile(page, 'routes.go');

    // routes.go hides new 15..51; expand the 20 lines after the first hunk.
    await expect(diffLine(item, 18)).toHaveCount(0);
    await clickWhenHittable(page, item.locator('[data-separator] [data-expand-button][data-expand-up]').filter({ visible: true }).first());

    // new 18: func handlePosts(w http.ResponseWriter, r *http.Request) {
    const line = diffLine(item, 18);
    await showLine(page, line);
    await expect(line).toHaveAttribute('data-line-type', 'context-expanded');
    await expectHighlighted(line, 3);
  });
});
