import { test, expect } from '@playwright/test';
import { addComment, clearAllComments, loadPage } from './helpers';

function skillSection(page: Parameters<typeof loadPage>[0]) {
  return page.locator('.file-section').filter({ hasText: 'skill.md' });
}

async function switchSkillToDocumentView(page: Parameters<typeof loadPage>[0]) {
  const section = skillSection(page);
  await expect(section).toBeVisible();
  await section.locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
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
    await expect(section.locator('.line-block[data-start-line="7"] .fence-marker', { hasText: '---' })).toBeVisible();
    await expect(section.locator('.line-block[data-start-line="2"] .code-line, .line-block[data-start-line="2"] code.hljs')).toContainText('name: demo-skill');
    await expect(section.locator('.line-block[data-start-line="2"] [class^="hljs-"]').first()).toBeVisible();

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
    await expect(reloadedSection.locator('.line-block[data-start-line="2"] + .comment-block').filter({ has: frontmatterCard })).toHaveCount(1);
    await expect(reloadedSection.locator('.line-block[data-start-line="10"] + .comment-block').filter({ has: bodyCard })).toHaveCount(1);
    await expect(reloadedSection.locator('.line-block[data-start-line="2"]')).toContainText('name: demo-skill');
    await expect(reloadedSection.locator('.line-block[data-start-line="10"]')).toContainText('Skill Body');

    await reloadedSection.screenshot({ path: 'test-results/markdown-frontmatter-skill.png' });
  });
});
