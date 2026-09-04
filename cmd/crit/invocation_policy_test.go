package main

import (
	"strings"
	"testing"
)

func readIntegrationForPolicyTest(t *testing.T, path string) string {
	t.Helper()
	data, err := integrationsFS.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInteractiveSkillsRequireExplicitCritWording(t *testing.T) {
	const sharedDescription = "Review code changes, a plan, a live page (running dev server), or a local HTML file with Crit inline comments and structured human feedback. Use only when the user explicitly invokes /crit or directly asks to use Crit; a generic review request does not count."
	paths := []string{
		"integrations/claude-code/skills/crit/SKILL.md",
		"integrations/cursor/skills/crit/SKILL.md",
		"integrations/github-copilot/skills/crit/SKILL.md",
		"integrations/grok/skills/crit/SKILL.md",
		"integrations/ampcode/skills/crit/SKILL.md",
		"integrations/pi/skills/crit/SKILL.md",
		"integrations/qwen/skills/crit/SKILL.md",
		"integrations/codex/skills/crit/SKILL.md",
		"integrations/codex/plugin/crit/skills/crit/SKILL.md",
		"integrations/hermes/skills/crit/SKILL.md",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			content := readIntegrationForPolicyTest(t, path)
			if strings.Contains(content, "disable-model-invocation: true") {
				t.Fatalf("%s still uses hard disable-model-invocation; prefer explicit-Crit wording", path)
			}
			if strings.Contains(content, "allow_implicit_invocation: false") {
				t.Fatalf("%s still uses hard allow_implicit_invocation:false; prefer explicit-Crit wording", path)
			}
			if !strings.Contains(content, sharedDescription) {
				t.Fatalf("%s lacks the shared explicit-Crit skill description", path)
			}
		})
	}

	// Aider uses conventions, not a skill frontmatter description.
	aider := readIntegrationForPolicyTest(t, "integrations/aider/CONVENTIONS.md")
	if !strings.Contains(aider, "explicitly") || !strings.Contains(aider, "generic") {
		t.Fatal("integrations/aider/CONVENTIONS.md lacks an explicit-Crit-only wording gate")
	}
}

func TestCodexNoLongerShipsImplicitInvocationPolicyFile(t *testing.T) {
	paths := []string{
		"integrations/codex/skills/crit/agents/openai.yaml",
		"integrations/codex/plugin/crit/skills/crit/agents/openai.yaml",
	}
	for _, path := range paths {
		if _, err := integrationsFS.ReadFile(path); err == nil {
			t.Fatalf("%s should have been removed; use skill wording instead of policy YAML", path)
		}
	}
}

func TestCritCLIStaysModelDiscoverableWithoutStartingInteractiveCrit(t *testing.T) {
	paths := []string{
		"integrations/claude-code/skills/crit-cli/SKILL.md",
		"integrations/cline/skills/crit-cli/SKILL.md",
		"integrations/codex/skills/crit-cli/SKILL.md",
		"integrations/cursor/skills/crit-cli/SKILL.md",
		"integrations/gemini/skills/crit-cli/SKILL.md",
		"integrations/github-copilot/skills/crit-cli/SKILL.md",
		"integrations/grok/skills/crit-cli/SKILL.md",
		"integrations/ampcode/skills/crit-cli/SKILL.md",
		"integrations/hermes/skills/crit-cli/SKILL.md",
		"integrations/opencode/skills/crit-cli/SKILL.md",
		"integrations/pi/skills/crit-cli/SKILL.md",
		"integrations/qwen/skills/crit-cli/SKILL.md",
		"integrations/windsurf/skills/crit-cli/SKILL.md",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			content := readIntegrationForPolicyTest(t, path)
			if !strings.Contains(content, "name: crit-cli") {
				t.Fatalf("%s is not a crit-cli skill", path)
			}
			if strings.Contains(content, "disable-model-invocation: true") {
				t.Fatalf("%s must remain model-discoverable", path)
			}
		})
	}
}

func TestCritCLIDescriptionsSeparateInteractiveWorkflow(t *testing.T) {
	cases := map[string]string{
		"integrations/cline/skills/crit-cli/SKILL.md":    "`/crit.md` workflow",
		"integrations/hermes/skills/crit-cli/SKILL.md":   "`crit` skill",
		"integrations/opencode/skills/crit-cli/SKILL.md": "`/crit` command",
		"integrations/windsurf/skills/crit-cli/SKILL.md": "`/crit` workflow",
	}
	for path, interactiveSurface := range cases {
		content := readIntegrationForPolicyTest(t, path)
		if !strings.Contains(content, "Not for invoking an interactive review loop") {
			t.Fatalf("%s does not distinguish the CLI reference from interactive Crit", path)
		}
		if !strings.Contains(content, interactiveSurface) {
			t.Fatalf("%s does not name its interactive surface %q", path, interactiveSurface)
		}
		if strings.Contains(content, `or "review"`) {
			t.Fatalf("%s treats a generic review request as explicit Crit invocation", path)
		}
	}
}

func TestManualWorkflowsReplaceAlwaysOnRules(t *testing.T) {
	cases := []struct {
		tool string
		dest string
	}{
		{tool: "cline", dest: ".clinerules/workflows/crit.md"},
		{tool: "windsurf", dest: ".windsurf/workflows/crit.md"},
	}
	for _, tc := range cases {
		files := integrationMap[tc.tool]
		if len(files) < 2 {
			t.Fatalf("%s should install a manual workflow and crit-cli skill", tc.tool)
		}
		if files[0].dest != tc.dest {
			t.Fatalf("%s interactive destination = %q, want %q", tc.tool, files[0].dest, tc.dest)
		}
		if !strings.Contains(files[1].dest, "crit-cli/SKILL.md") {
			t.Fatalf("%s second integration is not crit-cli: %q", tc.tool, files[1].dest)
		}
	}
}

func TestCritStorySkillsAreWordingGated(t *testing.T) {
	paths := []string{
		"integrations/claude-code/skills/crit-story/SKILL.md",
		"integrations/cursor/skills/crit-story/SKILL.md",
		"integrations/github-copilot/skills/crit-story/SKILL.md",
		"integrations/codex/skills/crit-story/SKILL.md",
		"integrations/codex/plugin/crit/skills/crit-story/SKILL.md",
		"integrations/pi/skills/crit-story/SKILL.md",
		"integrations/qwen/skills/crit-story/SKILL.md",
		"integrations/hermes/skills/crit-story/SKILL.md",
		"integrations/grok/skills/crit-story/SKILL.md",
		"integrations/ampcode/skills/crit-story/SKILL.md",
		"integrations/cline/skills/crit-story/SKILL.md",
		"integrations/windsurf/skills/crit-story/SKILL.md",
		"integrations/opencode/skills/crit-story/SKILL.md",
		"integrations/gemini/skills/crit-story/SKILL.md",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			content := readIntegrationForPolicyTest(t, path)
			if !strings.Contains(content, "name: crit-story") {
				t.Fatalf("%s is not a crit-story skill", path)
			}
			if !strings.Contains(content, "Do not infer") {
				t.Fatalf("%s lacks wording-gate language", path)
			}
			if strings.Contains(content, "disable-model-invocation: true") {
				t.Fatalf("%s uses hard disable; prefer wording gate", path)
			}
		})
	}
}

func TestPlanExitHooksRemainEnabled(t *testing.T) {
	cases := map[string]string{
		"integrations/claude-code/hooks/hooks.json":       "ExitPlanMode",
		"integrations/codex/plugin/crit/hooks/hooks.json": "crit plan-hook --mode codex",
		"integrations/gemini/hooks/settings-snippet.json": "exit_plan_mode",
	}
	for path, marker := range cases {
		if content := readIntegrationForPolicyTest(t, path); !strings.Contains(content, marker) {
			t.Fatalf("%s no longer contains plan-exit marker %q", path, marker)
		}
	}
}
