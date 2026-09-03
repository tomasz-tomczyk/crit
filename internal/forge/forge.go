// Package forge defines the hosting-provider boundary used by Crit's remote
// review integrations. It deliberately contains only normalized data and an
// interface; provider-specific API payloads stay in internal/github and
// internal/gitlab.
package forge

import "context"

// Kind identifies a supported code-hosting provider.
type Kind string

const (
	Auto   Kind = "auto"
	GitHub Kind = "github"
	GitLab Kind = "gitlab"
)

// ChangeID identifies a pull or merge request. Number is the repository-local
// PR number / MR IID. Project and Host are populated when a URL points at a
// project other than the current checkout.
type ChangeID struct {
	Number  int
	Project string
	Host    string
}

// RemoteRef identifies a provider-owned review comment. GitHub uses only
// CommentID; GitLab additionally requires ThreadID for note mutations.
type RemoteRef struct {
	Forge        Kind   `json:"forge"`
	ChangeNumber int    `json:"change_number,omitempty"`
	CommentID    int64  `json:"comment_id"`
	ThreadID     string `json:"thread_id,omitempty"`
}

// RepoContext describes the repository against which a provider command runs.
type RepoContext struct {
	Root    string
	Remote  string
	Project string
	Host    string
}

// RepoRef identifies a repository that owns one side of a change request.
type RepoRef struct {
	Project  string
	Host     string
	CloneURL string
}

// ChangeRequest is the provider-neutral metadata Crit needs for focus mode.
type ChangeRequest struct {
	ID              ChangeID
	URL             string
	Title           string
	Body            string
	State           string
	Draft           bool
	BaseRefName     string
	HeadRefName     string
	BaseSHA         string
	HeadSHA         string
	BaseRepo        RepoRef
	HeadRepo        RepoRef
	CrossRepository bool
	Additions       int
	Deletions       int
	ChangedFiles    int
	Author          string
	CreatedAt       string
}

// ChangeSummary is the lightweight change-request shape used by the picker.
type ChangeSummary struct {
	ID          ChangeID `json:"id"`
	Number      int      `json:"number"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	HeadRefName string   `json:"head_ref_name"`
	HeadSHA     string   `json:"head_sha"`
	BaseRefName string   `json:"base_ref_name"`
	Draft       bool     `json:"draft"`
	Provider    Kind     `json:"provider"`
}

// PullRequest and PushRequest carry the already-tokenized CLI arguments while
// the existing GitHub implementation is migrated behind this boundary. The
// provider owns platform-specific validation and review-file persistence.
type PullRequest struct {
	Repo       RepoContext
	ChangeSpec string
	OutputDir  string
	SessionID  string
	Args       []string // compatibility path for direct provider callers
}

type PushRequest struct {
	Repo       RepoContext
	ChangeSpec string
	OutputDir  string
	SessionID  string
	DryRun     bool
	Message    string
	Event      string
	Args       []string // compatibility path for direct provider callers
}

type PullResult struct {
	Imported int
	Updated  int
	Skipped  int
	Warnings []string
}

type PushResult struct {
	Created  int
	Edited   int
	Deleted  int
	Replied  int
	Resolved int
	Warnings []string
}

// Provider is the complete remote-review boundary consumed by CLI, focus,
// picker, and remote-file loading code.
type Provider interface {
	Kind() Kind
	RequireAuth(ctx context.Context, repo RepoContext) error
	Detect(ctx context.Context, repo RepoContext) (ChangeID, error)
	Get(ctx context.Context, repo RepoContext, id ChangeID) (ChangeRequest, error)
	ListOpen(ctx context.Context, repo RepoContext) ([]ChangeSummary, error)
	Pull(ctx context.Context, request PullRequest) (PullResult, error)
	Push(ctx context.Context, request PushRequest) (PushResult, error)
	FetchFile(ctx context.Context, repo RepoContext, source RepoRef, sha, path string) ([]byte, error)
	Invalidate(id ChangeID)
}
