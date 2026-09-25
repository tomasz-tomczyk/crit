import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { addComment, clearAllComments, fileHeader, loadPage, revealFile } from './helpers';

// Bring skill.md into view, switch it to Document view and return its
// rendered document (the .pierre-document annotation inside its Pierre item).
async function switchSkillToDocumentView(page: Page) {
  await revealFile(page, 'skill.md');
  await fileHeader(page, 'skill.md').locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
  const section = page.locator('[id="file-section-skill.md"].pierre-document');
  await expect(section.locator('.document-wrapper')).toBeVisible();
  return section;
}

test.describe('Markdown frontmatter — skill.md', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('renders YAML frontmatter as commentable highlighted code', async ({ page, request }) => {
    await loadPage(page);
    const section = await switchSkillToDocumentView(page);

    await expect(section.locator('h1', { hasText: 'this would become an h1' })).toHaveCount(0);
    await expect(section.locator('.line-block[data-start-line="1"] .fence-marker', { hasText: '---' })).toBeVisible();
    await expect(section.locator('.line-block[data-start-line="8"] .fence-marker', { hasText: '---' })).toBeVisible();
    await expect(section.locator('.line-block[data-start-line="2"] code.crit-code')).toContainText('name: demo-skill');
    await expect(section.locator('.line-block[data-start-line="2"] code.crit-code span[style*="--diffs-token"]').first()).toBeVisible();
    // Line 4 is a YAML `# comment` — must not become a heading.
    await expect(section.locator('.line-block[data-start-line="4"]')).toContainText('this would become an h1');
    await expect(section.locator('.line-block[data-start-line="4"] h1, .line-block[data-start-line="4"] h2')).toHaveCount(0);

    for (let line = 1; line <= 10; line++) {
      await expect(section.locator(`.line-block[data-start-line="${line}"]`)).toHaveCount(1);
    }

    const frontmatterComment = await addComment(request, 'skill.md', 2, 'Frontmatter comment');
    const bodyComment = await addComment(request, 'skill.md', 10, 'Body comment');

    await loadPage(page);
    const reloadedSection = await switchSkillToDocumentView(page);
    const frontmatterCard = reloadedSection.locator(`.comment-card[data-comment-id="${frontmatterComment.id}"]`);
    const bodyCard = reloadedSection.locator(`.comment-card[data-comment-id="${bodyComment.id}"]`);
    await expect(frontmatterCard).toBeVisible();
    await expect(bodyCard).toBeVisible();
    await expect(frontmatterCard).toContainText('Frontmatter comment');
    await expect(bodyCard).toContainText('Body comment');
    // Anchored to the frontmatter / body lines (has-comment on the line-block).
    await expect(reloadedSection.locator('.line-block[data-start-line="2"].has-comment')).toBeVisible();
    await expect(reloadedSection.locator('.line-block[data-start-line="10"].has-comment')).toBeVisible();
    await expect(reloadedSection.locator('.line-block[data-start-line="2"]')).toContainText('name: demo-skill');
    await expect(reloadedSection.locator('.line-block[data-start-line="10"]')).toContainText('Skill Body');

    await reloadedSection.screenshot({ path: 'test-results/markdown-frontmatter-skill.png' });
  });
});
