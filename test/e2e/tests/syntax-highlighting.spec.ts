import { test, expect, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, jsSection, revealFile, diffLine, showLine,
  clickWhenHittable, waitHighlighted,
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

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test.describe('Syntax Highlighting — Split Mode', () => {
  test('Go additions, deletions and context lines are syntax-highlighted', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    // Top to bottom: showLine only scrolls down to reach virtualized rows.
    // new 12 (context): func respondJSON(w http.ResponseWriter, status int, body string) {
    const context = diffLine(item, 12);
    await showLine(page, context);
    await expect(context).toHaveAttribute('data-line-type', /^context/);
    await expectHighlighted(context, 3);
    await expectHighlighted(diffLine(item, 9, 'old'), 3);

    // old 23: fmt.Fprintf(w, "Hello, %s!", r.URL.Path[1:])
    const old = diffLine(item, 23, 'old');
    await showLine(page, old);
    await expect(old).toHaveAttribute('data-line-type', 'change-deletion');
    await expectHighlighted(old);

    // new 24: func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    const line = diffLine(item, 24);
    await showLine(page, line);
    await expect(line).toHaveAttribute('data-line-type', 'change-addition');
    await expectHighlighted(line, 3);
    // The keyword is coloured differently from the function name.
    expect(await colourOf(token(line, 'func'))).not.toBe(await colourOf(token(line, 'authMiddleware')));
  });

  test('JavaScript, Gherkin and separator-expanded lines are syntax-highlighted', async ({ page }) => {
    await loadPage(page);

    // handler.js line 2: export function handleNotification(req, res) {
    const js = await jsSection(page);
    const jsLine = diffLine(js, 2);
    await expect(jsLine).toHaveAttribute('data-line-type', 'change-addition');
    await expectHighlighted(jsLine, 3);
    expect(await colourOf(token(jsLine, 'function'))).not.toBe(await colourOf(token(jsLine, 'handleNotification')));

    // login.feature line 1: Feature: User login — the keyword is tokenized
    // apart from the title (language detection for .feature).
    const feature = await revealFile(page, 'login.feature');
    const featureLine = diffLine(feature, 1);
    await expect(featureLine).toHaveAttribute('data-line-type', 'change-addition');
    await expectHighlighted(featureLine);
    expect(await colourOf(token(featureLine, 'Feature'))).not.toBe(await colourOf(token(featureLine, 'User login')));

    // routes.go hides new 15..51; expand the 20 lines after the first hunk.
    // Pierre re-renders the item, separators included, once it is
    // highlighted, and a click during that swap is lost: wait for the
    // highlighted render (line 14 sits above the gap), then click once.
    const routes = await revealFile(page, 'routes.go');
    await expect(diffLine(routes, 18)).toHaveCount(0);
    await waitHighlighted(routes, 14);
    await clickWhenHittable(page, routes.locator('[data-separator] [data-expand-button][data-expand-up]').filter({ visible: true }).first());
    await expect(diffLine(routes, 18)).toBeAttached();

    // new 18: func handlePosts(w http.ResponseWriter, r *http.Request) {
    const expanded = diffLine(routes, 18);
    await showLine(page, expanded);
    await expect(expanded).toHaveAttribute('data-line-type', 'context-expanded');
    await expectHighlighted(expanded, 3);
  });
});
