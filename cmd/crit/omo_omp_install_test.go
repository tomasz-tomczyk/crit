package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// ompStyleAgents share one install layout: skills under .<agent>/skills in
// the project and .<agent>/agent/skills in the home directory.
var ompStyleAgents = []string{"omo", "omp"}

func TestInstallIntegration_OMOAndOMP(t *testing.T) {
	for _, agent := range ompStyleAgents {
		for _, global := range []bool{false, true} {
			name := agent + "/project"
			if global {
				name = agent + "/global"
			}
			t.Run(name, func(t *testing.T) {
				testInstallOMPStyle(t, agent, global)
			})
		}
	}
}

func testInstallOMPStyle(t *testing.T, agent string, global bool) {
	// Given an isolated home with the agent's config dir, and an empty PATH
	// so no real CLI is probed.
	isolateAgentPATH(t)
	home := t.TempDir()
	project := t.TempDir()
	testutil.SetHome(t, home)
	if err := os.Mkdir(filepath.Join(home, "."+agent), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := project
	skillsDir := filepath.Join(project, "."+agent, "skills")
	location := locationProject
	if global {
		cwd = home
		skillsDir = filepath.Join(home, "."+agent, "agent", "skills")
		location = locationHome
	}
	t.Chdir(cwd)

	// When the integration is installed, it stops being reported missing.
	if got := checkMissingIntegrations(project, home); !slices.Equal(got, []string{agent}) {
		t.Fatalf("uninstalled %s integration: got %v, want [%s]", agent, got, agent)
	}
	if err := installIntegration(agent, false); err != nil {
		t.Fatal(err)
	}
	if got := checkMissingIntegrations(project, home); len(got) != 0 {
		t.Fatalf("installed integration still missing: %v", got)
	}

	// Then every embedded skill reaches the agent's discovery path unchanged.
	for _, skill := range []string{"crit", "crit-cli", "crit-story"} {
		assertSkillMatchesEmbedded(t, agent, skill, filepath.Join(skillsDir, skill, "SKILL.md"))
	}
	statuses := detectInstalledIntegrations(project, home)
	if len(statuses) != 1 || statuses[0].Agent != agent || statuses[0].Status != "current" || statuses[0].Location != location {
		t.Fatalf("installed %s integration not current at %s: %+v", agent, location, statuses)
	}

	// A user-edited skill survives reinstall and is reported stale.
	dest := filepath.Join(skillsDir, "crit", "SKILL.md")
	const custom = "user-edited skill\n"
	if err := os.WriteFile(dest, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installIntegration(agent, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Fatalf("reinstall overwrote user-edited skill: %q", got)
	}
	stale := checkInstalledIntegrations(project, home)
	if len(stale) != 1 || stale[0].agent != agent || stale[0].dest != dest || stale[0].location != location {
		t.Fatalf("modified %s skill not detected as stale: %+v", agent, stale)
	}

	// --force restores the embedded content.
	if err := installIntegration(agent, true); err != nil {
		t.Fatal(err)
	}
	assertSkillMatchesEmbedded(t, agent, "crit", dest)
	if stale := checkInstalledIntegrations(project, home); len(stale) != 0 {
		t.Fatalf("forced reinstall left stale skills: %+v", stale)
	}
}

func assertSkillMatchesEmbedded(t *testing.T, agent, skill, dest string) {
	t.Helper()
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("skill not installed at %s discovery path %s: %v", agent, dest, err)
	}
	want, err := integrationsFS.ReadFile("integrations/" + agent + "/skills/" + skill + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s/%s: installed content differs from embedded skill", agent, skill)
	}
}

func TestDetectPresentAgents_OMOAndOMPConfigDir(t *testing.T) {
	for _, agent := range ompStyleAgents {
		t.Run(agent, func(t *testing.T) {
			// Given only the agent's home config dir, with no CLI binaries on PATH.
			home := t.TempDir()
			t.Setenv("PATH", t.TempDir())
			if err := os.MkdirAll(filepath.Join(home, "."+agent), 0o755); err != nil {
				t.Fatal(err)
			}

			// When detecting present agents, it is available for installation.
			if got := detectPresentAgents(home); !slices.Equal(got, []string{agent}) {
				t.Fatalf("got %v, want [%s]", got, agent)
			}
			if !slices.Contains(availableIntegrations(), agent) {
				t.Fatalf("%s missing from available integrations", agent)
			}
		})
	}
}
