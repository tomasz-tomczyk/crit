package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/gitlab"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type wireTestProvider struct {
	getID forge.ChangeID
	get   forge.ChangeRequest
	err   error
}

func (*wireTestProvider) Kind() forge.Kind { return forge.GitLab }
func (*wireTestProvider) RequireAuth(context.Context, forge.RepoContext) error {
	return nil
}
func (*wireTestProvider) Detect(context.Context, forge.RepoContext) (forge.ChangeID, error) {
	return forge.ChangeID{}, nil
}
func (p *wireTestProvider) Get(_ context.Context, _ forge.RepoContext, id forge.ChangeID) (forge.ChangeRequest, error) {
	p.getID = id
	return p.get, p.err
}
func (*wireTestProvider) ListOpen(context.Context, forge.RepoContext) ([]forge.ChangeSummary, error) {
	return nil, nil
}
func (*wireTestProvider) Pull(context.Context, forge.PullRequest) (forge.PullResult, error) {
	return forge.PullResult{}, nil
}
func (*wireTestProvider) Push(context.Context, forge.PushRequest) (forge.PushResult, error) {
	return forge.PushResult{}, nil
}
func (*wireTestProvider) FetchFile(context.Context, forge.RepoContext, forge.RepoRef, string, string) ([]byte, error) {
	return nil, nil
}
func (*wireTestProvider) Invalidate(forge.ChangeID) {}

func isolateWireTest(t *testing.T, cwd string) string {
	t.Helper()
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	if cwd == "" {
		cwd = t.TempDir()
	}
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	return homeDir
}

func writeWireConfig(t *testing.T, homeDir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installWireFakeGLab(t *testing.T, exitCode int, apiResponse string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-glab shim is a POSIX shell script; not portable to Windows")
	}
	binDir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "api" ]; then
  printf '%s' "$CRIT_WIRE_GLAB_API"
fi
exit "$CRIT_WIRE_GLAB_EXIT"
`
	if err := os.WriteFile(filepath.Join(binDir, "glab"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CRIT_WIRE_GLAB_API", apiResponse)
	t.Setenv("CRIT_WIRE_GLAB_EXIT", strconv.Itoa(exitCode))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestWire_ResolveServerConfigPassesSessionID(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc, err := session.ResolveServerConfigFn([]string{"--session", "839f3b4cd5d6", "--no-open"})
	if err != nil {
		t.Fatalf("ResolveServerConfigFn: %v", err)
	}
	if sc.SessionID != "839f3b4cd5d6" {
		t.Errorf("SessionID = %q", sc.SessionID)
	}
}

func TestSelectProviderExplicitSelectionOverridesConfig(t *testing.T) {
	homeDir := isolateWireTest(t, "")
	writeWireConfig(t, homeDir, `{"forge":"github","gitlab_url":"https://code.example"}`)

	provider, err := selectProvider(forge.GitLab)
	if err != nil {
		t.Fatal(err)
	}
	gitlabProvider, ok := provider.(gitlab.Provider)
	if !ok || gitlabProvider.Host != "code.example" {
		t.Fatalf("provider = %#v, want GitLab host code.example", provider)
	}
	provider, err = selectProvider(forge.GitHub)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.(github.Provider); !ok {
		t.Fatalf("provider = %#v, want GitHub", provider)
	}
}

func TestSelectProviderAutoDetectsGitLabRemote(t *testing.T) {
	repo := vcs.InitTestRepo(t)
	vcs.GitRun(t, repo, "remote", "add", "origin", "https://gitlab.example/acme/widget.git")
	homeDir := isolateWireTest(t, repo)
	writeWireConfig(t, homeDir, `{"forge":"auto","gitlab_url":"https://gitlab.example"}`)

	provider, err := selectProvider(forge.Auto)
	if err != nil {
		t.Fatal(err)
	}
	gitlabProvider, ok := provider.(gitlab.Provider)
	if !ok || gitlabProvider.Host != "gitlab.example" {
		t.Fatalf("provider = %#v, want GitLab host gitlab.example", provider)
	}
}

func TestSelectProviderUsesGlabForUnknownSelfHostedRemote(t *testing.T) {
	repo := vcs.InitTestRepo(t)
	vcs.GitRun(t, repo, "remote", "add", "origin", "git@code.example:acme/widget.git")
	homeDir := isolateWireTest(t, repo)
	writeWireConfig(t, homeDir, `{"gitlab_url":"https://code.example"}`)
	installWireFakeGLab(t, 0, "")

	provider, err := selectProvider(forge.Auto)
	if err != nil {
		t.Fatal(err)
	}
	if provider.Kind() != forge.GitLab {
		t.Fatalf("provider kind = %q, want gitlab", provider.Kind())
	}
}

func TestSelectProviderRejectsInvalidConfig(t *testing.T) {
	homeDir := isolateWireTest(t, "")
	writeWireConfig(t, homeDir, `{"forge":"bitbucket"}`)
	if _, err := selectProvider(forge.Auto); err == nil || !strings.Contains(err.Error(), "invalid forge") {
		t.Fatalf("selectProvider error = %v", err)
	}
}

func TestGlabOwnsHost(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if glabOwnsHost("code.example") {
			t.Fatal("missing glab owns host")
		}
	})
	t.Run("authenticated", func(t *testing.T) {
		installWireFakeGLab(t, 0, "")
		if !glabOwnsHost("code.example") {
			t.Fatal("authenticated glab did not own host")
		}
	})
	t.Run("unauthenticated", func(t *testing.T) {
		installWireFakeGLab(t, 1, "")
		if glabOwnsHost("code.example") {
			t.Fatal("unauthenticated glab owns host")
		}
	})
}

func TestFetchChangeUsesNormalizedChangeID(t *testing.T) {
	wantErr := errors.New("provider failure")
	provider := &wireTestProvider{get: forge.ChangeRequest{Title: "Feature"}, err: wantErr}
	change, err := fetchChange(provider, 27)
	if change.Title != "Feature" || !errors.Is(err, wantErr) {
		t.Fatalf("fetchChange = (%+v, %v)", change, err)
	}
	if provider.getID != (forge.ChangeID{Number: 27}) {
		t.Fatalf("change ID = %+v", provider.getID)
	}
}

func TestListOpenGitLabChangesUsesConfiguredProvider(t *testing.T) {
	homeDir := isolateWireTest(t, "")
	writeWireConfig(t, homeDir, `{"gitlab_url":"https://gitlab.example"}`)
	installWireFakeGLab(t, 0, `[{"iid":3,"title":"MR","sha":"head"}]`)

	changes, err := listOpenGitLabChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Number != 3 || changes[0].ID.Host != "gitlab.example" {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestListOpenGitLabChangesRejectsInvalidConfiguredURL(t *testing.T) {
	homeDir := isolateWireTest(t, "")
	writeWireConfig(t, homeDir, `{"gitlab_url":"not-a-url"}`)
	if _, err := listOpenGitLabChanges(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid gitlab_url") {
		t.Fatalf("listOpenGitLabChanges error = %v", err)
	}
}

func TestWiredForgeDetectionFallsBackToGitHubOnConfigError(t *testing.T) {
	homeDir := isolateWireTest(t, "")
	writeWireConfig(t, homeDir, `{"forge":"invalid"}`)
	if got := server.DetectForgeKindFn(); got != forge.GitHub {
		t.Fatalf("detected forge = %q, want github fallback", got)
	}
}

func TestWirePRResolveHooks_PreservesURLProject(t *testing.T) {
	prevFetch, prevStacked := focus.FetchPRHook, focus.IsStackedPRHook
	t.Cleanup(func() { focus.SetPRResolveHooks(prevFetch, prevStacked) })

	restore := github.SwapFetchPRByNumberForTest(func(n int) (*github.PRInfo, error) {
		if n == 99 {
			return nil, errors.New("fetch boom")
		}
		return &github.PRInfo{
			URL:               "https://github.com/myorg/repo-b/pull/1",
			Number:            n,
			Title:             "Cross-repo",
			BaseRefOid:        "base",
			HeadRefOid:        "head",
			BaseRefName:       "main",
			HeadRefName:       "feat",
			HeadRepoURL:       "https://github.com/fork/repo-b.git",
			IsCrossRepository: true,
		}, nil
	})
	t.Cleanup(restore)

	wirePRResolveHooks()

	info, err := focus.FetchPRHook("https://github.com/myorg/repo-b/pull/1")
	if err != nil {
		t.Fatal(err)
	}
	if info.BaseRepoProject != "myorg/repo-b" {
		t.Errorf("BaseRepoProject = %q", info.BaseRepoProject)
	}
	if info.HeadRepoProject != "fork/repo-b" {
		t.Errorf("HeadRepoProject = %q", info.HeadRepoProject)
	}
	if info.HeadRepoHost != "github.com" {
		t.Errorf("HeadRepoHost = %q", info.HeadRepoHost)
	}

	bare, err := focus.FetchPRHook("7")
	if err != nil {
		t.Fatal(err)
	}
	if bare.BaseRepoProject != "" {
		t.Errorf("bare --pr BaseRepoProject = %q want empty", bare.BaseRepoProject)
	}

	if _, err := focus.FetchPRHook("not-a-pr"); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := focus.FetchPRHook("99"); err == nil || !strings.Contains(err.Error(), "fetch boom") {
		t.Fatalf("fetch error = %v", err)
	}
	if focus.IsStackedPRHook == nil || focus.IsStackedPRHook(info, nil) {
		t.Fatal("IsStackedPRHook(nil vcs) should be false")
	}
}

func TestWireMRResolveHooks_InvalidSpecAndStackedGuard(t *testing.T) {
	prevFetch, prevStacked := focus.FetchMRHook, focus.IsStackedMRHook
	t.Cleanup(func() { focus.SetMRResolveHooks(prevFetch, prevStacked) })

	wireMRResolveHooks()

	if _, err := focus.FetchMRHook("not-an-mr"); err == nil {
		t.Fatal("expected parse error")
	}
	if focus.IsStackedMRHook == nil {
		t.Fatal("IsStackedMRHook not wired")
	}
	if focus.IsStackedMRHook(focus.ChangeResolveInfo{BaseRefName: "feature"}, nil) {
		t.Fatal("nil vcs should not be stacked")
	}
}
