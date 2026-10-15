package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestInstallIntegration_OMO(t *testing.T) {
	for _, global := range []bool{false, true} {
		name := "project"
		if global {
			name = "global"
		}
		t.Run(name, func(t *testing.T) {
			// Given an isolated home, project and PATH (no real opencode probe).
			isolateAgentPATH(t)
			home := t.TempDir()
			project := t.TempDir()
			testutil.SetHome(t, home)
			cwd := project
			skillsDir := filepath.Join(project, ".omo", "skills")
			if global {
				cwd = home
				skillsDir = filepath.Join(home, ".omo", "agent", "skills")
			}
			t.Chdir(cwd)

			// When the OMO integration is installed.
			if err := installIntegration("omo", false); err != nil {
				t.Fatal(err)
			}

			// Then all embedded skills reach OMO's discovery paths.
			for _, skill := range []string{"crit", "crit-cli", "crit-story"} {
				got, err := os.ReadFile(filepath.Join(skillsDir, skill, "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
				want, err := integrationsFS.ReadFile("integrations/omo/skills/" + skill + "/SKILL.md")
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s: installed content differs from embedded skill", skill)
				}
			}
			statuses := detectInstalledIntegrations(project, home)
			if len(statuses) != 1 || statuses[0].Agent != "omo" || statuses[0].Status != "current" {
				t.Fatalf("installed OMO integration not detected as current: %+v", statuses)
			}
		})
	}
}

func TestInstallIntegration_OMOExistingSkill(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "skip", true: "force"}[force], func(t *testing.T) {
			// Given an existing user-edited skill.
			project := t.TempDir()
			testutil.SetHome(t, t.TempDir())
			t.Chdir(project)
			dest := filepath.Join(".omo", "skills", "crit", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			want := []byte("user-edited skill")
			if err := os.WriteFile(dest, want, 0o644); err != nil {
				t.Fatal(err)
			}
			if force {
				var err error
				want, err = integrationsFS.ReadFile("integrations/omo/skills/crit/SKILL.md")
				if err != nil {
					t.Fatal(err)
				}
			}

			// When installing with the selected overwrite policy.
			if err := installIntegration("omo", force); err != nil {
				t.Fatal(err)
			}

			// Then only --force replaces the existing content.
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("unexpected installed content with force=%t", force)
			}
		})
	}
}

func TestDetectPresentAgents_OMOConfigDir(t *testing.T) {
	// Given an OMO home without CLI binaries on PATH.
	home := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, ".omo"), 0o755); err != nil {
		t.Fatal(err)
	}

	// When detecting present agents.
	got := detectPresentAgents(home)

	// Then OMO is available for installation.
	if !slices.Equal(got, []string{"omo"}) {
		t.Fatalf("got %v, want [omo]", got)
	}
	if !slices.Contains(availableIntegrations(), "omo") {
		t.Fatal("omo missing from available integrations")
	}
}
