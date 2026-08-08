package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// Provider implements GitLab merge-request operations through the authenticated
// glab CLI. glab selects the self-managed host associated with the current
// repository unless an explicit MR URL supplies a hostname.
type Provider struct {
	Host string
}

// NewProvider resolves the one configured GitLab base URL. Individual MR URLs
// remain valid change specs, but callers do not choose a host per operation.
func NewProvider(baseURL string) (Provider, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://gitlab.com"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || (u.Path != "" && u.Path != "/") {
		return Provider{}, fmt.Errorf("invalid gitlab_url %q", baseURL)
	}
	return Provider{Host: u.Host}, nil
}

func (p Provider) repo(repo forge.RepoContext) forge.RepoContext {
	if p.Host != "" {
		repo.Host = p.Host
	}
	return repo
}

func (p Provider) changeID(id forge.ChangeID) (forge.ChangeID, error) {
	if p.Host != "" && id.Host != "" && !strings.EqualFold(p.Host, id.Host) {
		return forge.ChangeID{}, fmt.Errorf("merge request URL host %q does not match configured gitlab_url host %q", id.Host, p.Host)
	}
	if p.Host != "" {
		id.Host = p.Host
	}
	return id, nil
}

func (Provider) Kind() forge.Kind { return forge.GitLab }

func (p Provider) RequireAuth(ctx context.Context, repo forge.RepoContext) error {
	return requireGLab(ctx, p.repo(repo))
}

func (p Provider) Detect(ctx context.Context, repo forge.RepoContext) (forge.ChangeID, error) {
	repo = p.repo(repo)
	branch := repoBranch(ctx)
	if branch == "" {
		return forge.ChangeID{}, fmt.Errorf("cannot detect current branch for GitLab merge request")
	}
	endpoint := "projects/:fullpath/merge_requests?state=opened&scope=all&source_branch=" + url.QueryEscape(branch)
	out, err := runAPI(ctx, repo.Host, endpoint, "GET", nil)
	if err != nil {
		return forge.ChangeID{}, err
	}
	var mrs []mrRaw
	if err := json.Unmarshal(out, &mrs); err != nil {
		return forge.ChangeID{}, fmt.Errorf("parsing GitLab merge request list: %w", err)
	}
	if len(mrs) == 0 {
		return forge.ChangeID{}, fmt.Errorf("no GitLab merge request found for current branch (try: crit pull <mr-iid>)")
	}
	return forge.ChangeID{Number: mrs[0].IID, Host: repo.Host, Project: repo.Project}, nil
}

func repoBranch(ctx context.Context) string {
	cmd := commandContext(ctx, "git", "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type gitlabUser struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

type diffRefs struct {
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	HeadSHA  string `json:"head_sha"`
}

type mrRaw struct {
	IID             int        `json:"iid"`
	WebURL          string     `json:"web_url"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	State           string     `json:"state"`
	Draft           bool       `json:"draft"`
	WorkInProgress  bool       `json:"work_in_progress"`
	SourceBranch    string     `json:"source_branch"`
	TargetBranch    string     `json:"target_branch"`
	SourceProjectID int        `json:"source_project_id"`
	TargetProjectID int        `json:"target_project_id"`
	ChangesCount    string     `json:"changes_count"`
	Author          gitlabUser `json:"author"`
	CreatedAt       string     `json:"created_at"`
	SHA             string     `json:"sha"`
	DiffRefs        diffRefs   `json:"diff_refs"`
}

type projectRaw struct {
	PathWithNamespace string `json:"path_with_namespace"`
	WebURL            string `json:"web_url"`
}

func (p Provider) Get(ctx context.Context, repo forge.RepoContext, id forge.ChangeID) (forge.ChangeRequest, error) {
	repo = p.repo(repo)
	var err error
	id, err = p.changeID(id)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	if id.Number <= 0 {
		return forge.ChangeRequest{}, fmt.Errorf("invalid GitLab merge request IID %d", id.Number)
	}
	out, err := runAPI(ctx, first(id.Host, repo.Host), projectEndpoint(id, ""), "GET", nil)
	if err != nil {
		return forge.ChangeRequest{}, err
	}
	var raw mrRaw
	if err := json.Unmarshal(out, &raw); err != nil {
		return forge.ChangeRequest{}, fmt.Errorf("parsing GitLab merge request: %w", err)
	}
	change := forge.ChangeRequest{
		ID: id, URL: raw.WebURL, Title: raw.Title, Body: raw.Description, State: raw.State,
		Draft: raw.Draft || raw.WorkInProgress, BaseRefName: raw.TargetBranch,
		HeadRefName: raw.SourceBranch, BaseSHA: raw.DiffRefs.BaseSHA,
		HeadSHA: raw.DiffRefs.HeadSHA, CrossRepository: raw.SourceProjectID != raw.TargetProjectID,
		ChangedFiles: atoi(raw.ChangesCount), Author: displayName(raw.Author), CreatedAt: raw.CreatedAt,
	}
	change.BaseRepo = projectRef(ctx, first(id.Host, repo.Host), raw.TargetProjectID)
	change.HeadRepo = projectRef(ctx, first(id.Host, repo.Host), raw.SourceProjectID)
	return change, nil
}

func projectRef(ctx context.Context, host string, projectID int) forge.RepoRef {
	if projectID == 0 {
		return forge.RepoRef{Host: host}
	}
	projectOut, err := runAPI(ctx, host, fmt.Sprintf("projects/%d", projectID), "GET", nil)
	if err != nil {
		return forge.RepoRef{Host: host}
	}
	var project projectRaw
	if json.Unmarshal(projectOut, &project) != nil {
		return forge.RepoRef{Host: host}
	}
	return forge.RepoRef{Project: project.PathWithNamespace, Host: host, CloneURL: strings.TrimSuffix(project.WebURL, "/") + ".git"}
}

func (p Provider) ListOpen(ctx context.Context, repo forge.RepoContext) ([]forge.ChangeSummary, error) {
	repo = p.repo(repo)
	out, err := runAPI(ctx, repo.Host, "projects/:fullpath/merge_requests?state=opened&scope=all&per_page=100", "GET", nil)
	if err != nil {
		return nil, err
	}
	var mrs []mrRaw
	if err := json.Unmarshal(out, &mrs); err != nil {
		return nil, fmt.Errorf("parsing GitLab merge request list: %w", err)
	}
	result := make([]forge.ChangeSummary, 0, len(mrs))
	for _, mr := range mrs {
		result = append(result, forge.ChangeSummary{
			ID:     forge.ChangeID{Number: mr.IID, Project: repo.Project, Host: repo.Host},
			Number: mr.IID, Title: mr.Title, URL: mr.WebURL, HeadRefName: mr.SourceBranch,
			HeadSHA: first(mr.DiffRefs.HeadSHA, mr.SHA), BaseRefName: mr.TargetBranch,
			Draft: mr.Draft || mr.WorkInProgress, Provider: forge.GitLab,
		})
	}
	return result, nil
}

func (p Provider) Pull(ctx context.Context, request forge.PullRequest) (forge.PullResult, error) {
	request.Repo = p.repo(request.Repo)
	return runPull(ctx, request)
}

func (p Provider) Push(ctx context.Context, request forge.PushRequest) (forge.PushResult, error) {
	request.Repo = p.repo(request.Repo)
	return runPush(ctx, request)
}

func (p Provider) FetchFile(ctx context.Context, repo forge.RepoContext, source forge.RepoRef, sha, path string) ([]byte, error) {
	repo = p.repo(repo)
	if p.Host != "" && source.Host != "" && !strings.EqualFold(p.Host, source.Host) {
		return nil, fmt.Errorf("remote file host %q does not match configured gitlab_url host %q", source.Host, p.Host)
	}
	project := source.Project
	if project == "" {
		project = repo.Project
	}
	if project == "" {
		project = ":fullpath"
	} else {
		project = strings.ReplaceAll(project, "/", "%2F")
	}
	endpoint := fmt.Sprintf("projects/%s/repository/files/%s/raw?ref=%s", project, url.PathEscape(path), url.QueryEscape(sha))
	return runAPI(ctx, repo.Host, endpoint, "GET", nil)
}

type mrDiffRaw struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
}

// FetchDiffs returns the complete MR layer diff for remote-mode rendering.
func (p Provider) FetchDiffs(ctx context.Context, repo forge.RepoContext, id forge.ChangeID) ([]session.RemoteDiffFile, error) {
	repo = p.repo(repo)
	var err error
	id, err = p.changeID(id)
	if err != nil {
		return nil, err
	}
	const perPage = 100
	var result []session.RemoteDiffFile
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/diffs?per_page=%d&page=%d", projectEndpoint(id, ""), perPage, page)
		out, err := runAPI(ctx, first(id.Host, repo.Host), endpoint, "GET", nil)
		if err != nil {
			return nil, err
		}
		var batch []mrDiffRaw
		if err := json.Unmarshal(out, &batch); err != nil {
			return nil, fmt.Errorf("parsing GitLab merge request diffs: %w", err)
		}
		for _, raw := range batch {
			status := "modified"
			switch {
			case raw.NewFile:
				status = "added"
			case raw.DeletedFile:
				status = "deleted"
			case raw.RenamedFile:
				status = "renamed"
			}
			result = append(result, session.RemoteDiffFile{
				FileChange: vcs.FileChange{Path: raw.NewPath, OldPath: raw.OldPath, Status: status},
				Hunks:      vcs.ParseUnifiedDiff(raw.Diff),
			})
		}
		if len(batch) < perPage {
			return result, nil
		}
	}
}

func (Provider) Invalidate(forge.ChangeID) {}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func atoi(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func displayName(user gitlabUser) string {
	if strings.TrimSpace(user.Name) != "" {
		return user.Name
	}
	return user.Username
}

var _ forge.Provider = Provider{}
