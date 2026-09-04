package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// Provider exposes the existing GitHub implementation through the neutral
// forge boundary. The established gh-backed workflows remain the source of
// truth; this type only translates normalized metadata.
type Provider struct{}

func (Provider) Kind() forge.Kind { return forge.GitHub }

func (Provider) RequireAuth(_ context.Context, _ forge.RepoContext) error { return RequireGH() }

func (Provider) Detect(_ context.Context, _ forge.RepoContext) (forge.ChangeID, error) {
	n, err := DetectPR(0)
	return forge.ChangeID{Number: n}, err
}

func (Provider) Get(_ context.Context, _ forge.RepoContext, id forge.ChangeID) (forge.ChangeRequest, error) {
	info, err := FetchPR(id)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	if info == nil {
		return forge.ChangeRequest{}, fmt.Errorf("PR #%d not found", id.Number)
	}
	baseProject := id.Project
	if baseProject == "" {
		baseProject = githubProject(info.URL)
	}
	baseHost := id.Host
	if baseHost == "" {
		if u, err := url.Parse(info.URL); err == nil {
			baseHost = normalizeGitHubHost(u.Host)
		}
	}
	return forge.ChangeRequest{
		ID: id, URL: info.URL, Title: info.Title, Body: info.Body, State: info.State,
		Draft: info.IsDraft, BaseRefName: info.BaseRefName, HeadRefName: info.HeadRefName,
		BaseSHA: info.BaseRefOid, HeadSHA: info.HeadRefOid,
		BaseRepo:        forge.RepoRef{Project: baseProject, Host: baseHost},
		HeadRepo:        forge.RepoRef{Project: githubProject(info.HeadRepoURL), CloneURL: info.HeadRepoURL},
		CrossRepository: info.IsCrossRepository, Additions: info.Additions,
		Deletions: info.Deletions, ChangedFiles: info.ChangedFiles,
		Author: info.AuthorLogin, CreatedAt: info.CreatedAt,
	}, nil
}

func (Provider) ListOpen(ctx context.Context, _ forge.RepoContext) ([]forge.ChangeSummary, error) {
	prs, err := fetchOpenPRsCtx(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]forge.ChangeSummary, 0, len(prs))
	for _, pr := range prs {
		out = append(out, forge.ChangeSummary{
			ID: forge.ChangeID{Number: pr.Number}, Number: pr.Number, Title: pr.Title,
			URL: pr.URL, HeadRefName: pr.HeadRefName, HeadSHA: pr.HeadRefOid,
			BaseRefName: pr.BaseRefName, Draft: pr.IsDraft, Provider: forge.GitHub,
		})
	}
	return out, nil
}

func (Provider) Pull(_ context.Context, request forge.PullRequest) (forge.PullResult, error) {
	args, err := githubPullArgs(request)
	if err != nil {
		return forge.PullResult{}, err
	}
	return forge.PullResult{}, RunPull(args)
}

func (Provider) Push(_ context.Context, request forge.PushRequest) (forge.PushResult, error) {
	args, err := githubPushArgs(request)
	if err != nil {
		return forge.PushResult{}, err
	}
	return forge.PushResult{}, RunPush(args)
}

func githubPullArgs(request forge.PullRequest) ([]string, error) {
	if request.Args != nil {
		return request.Args, nil
	}
	args := []string{}
	if request.SessionID != "" {
		args = append(args, "--session", request.SessionID)
	}
	if request.OutputDir != "" {
		args = append(args, "--output", request.OutputDir)
	}
	if request.ChangeSpec != "" {
		// Validate and preserve the original spec (number or URL) so RunPull can
		// pin -R from ParsePRSpec — do not collapse URLs to bare numbers.
		if _, err := ParsePRSpec(request.ChangeSpec); err != nil {
			return nil, fmt.Errorf("invalid GitHub pull request %q (expected number or URL)", request.ChangeSpec)
		}
		args = append(args, request.ChangeSpec)
	}
	return args, nil
}

func githubPushArgs(request forge.PushRequest) ([]string, error) {
	if request.Args != nil {
		return request.Args, nil
	}
	args, err := githubPullArgs(forge.PullRequest{
		ChangeSpec: request.ChangeSpec,
		OutputDir:  request.OutputDir,
		SessionID:  request.SessionID,
	})
	if err != nil {
		return nil, err
	}
	prefix := []string{}
	if request.DryRun {
		prefix = append(prefix, "--dry-run")
	}
	if request.Event != "" && request.Event != "comment" {
		prefix = append(prefix, "--event", request.Event)
	}
	if request.Message != "" {
		prefix = append(prefix, "--message", request.Message)
	}
	return append(prefix, args...), nil
}

func (Provider) FetchFile(_ context.Context, _ forge.RepoContext, source forge.RepoRef, sha, path string) ([]byte, error) {
	project := strings.Trim(source.Project, "/")
	parts := strings.Split(project, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid GitHub project %q", source.Project)
	}
	return session.FetchGitHubFileContent(parts[len(parts)-2], parts[len(parts)-1], sha, path)
}

func (Provider) Invalidate(id forge.ChangeID) {
	// Drop bare and project-qualified sibling keys (same as crit pull).
	InvalidatePRCache(id.Number)
}

var _ forge.Provider = Provider{}

func githubProject(raw string) string {
	if raw == "" {
		return ""
	}
	// HTML PR URLs must yield owner/repo, not owner/repo/pull/N.
	if id, err := ParsePRSpec(raw); err == nil && id.Project != "" {
		return id.Project
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
		return path
	}
	return strings.TrimSuffix(strings.Trim(raw, "/"), ".git")
}

// ProjectFromRemoteURL extracts "owner/repo" from a GitHub remote or PR URL.
func ProjectFromRemoteURL(raw string) string { return githubProject(raw) }
