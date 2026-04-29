# PR-Scoped and Commit-Range Review (issue #300)

Status: spec, pre-implementation
Scope: `crit/` (Go) only. Sibling parity in `crit-web/` is a follow-up.

## Goal

Today, `crit` in a stacked PR (e.g. `main ← A ← B ← C`) shows the entire stack diff
(`mergeBase(HEAD, default)..workingtree`) because `Session.BaseRef` is computed once
from `vcs.MergeBase(defaultBranch)` (`session.go:312-316`). Reviewing just PR `B`
(i.e. `A..B`) is impossible without checking out the parent and re-running.

We want three new entry points that all funnel into the same internal "focus":

1. `crit --range <baseSHA>..<headSHA>` — diff exactly that range.
2. `crit --pr <num|url>` — resolve via `gh pr view`, then drive a range.
3. UI picker in the review header — switch focus without restarting.

The working-tree path stays the default with byte-identical behavior.

## Glossary

- **Focus**: which "diff" the session is showing. Either the working tree (today's
  default) or a fixed `(baseSHA, headSHA)` range. Not the same thing as `BaseRef`,
  which today is always derived from a branch ref — the whole point of this work
  is decoupling.
- **PR head SHA**: GitHub's `headRefOid`. The tip commit of the PR branch at the
  time we resolved it. Comments anchor to this.
- **Layer / full-stack**: borrowed from prior art. *Layer* = parent PR's head to
  this PR's head (what GitHub renders, the default). *Full-stack* = repo default
  branch to this PR's head (cumulative). Both ship in v1 as a per-session toggle
  in the picker. Layer is always available; full-stack is gated on resolving
  `DefaultSHA`.
- **Diff scope (new) vs. existing scope toggle**: the existing UI scope toggle
  (all/branch/staged/unstaged, see `frontend/scope-toggle.spec.ts`) is unrelated
  — it filters working-tree changes. The PR/range scope is a separate concept,
  only shown when `Focus.Kind == FocusRange`. To avoid name confusion in code,
  we use **`DiffScope`** for the new type and `scope=` query param keeps its
  existing meaning. On the wire we use `diff_scope` for the new field.
- **`Comment.Scope` collision**: `Comment.Scope` (`session.go:69`) already
  exists with values `"line" | "file" | "review"`. We must **not** overload it.
  The new field is `Comment.DiffScope` (`"layer" | "full_stack" | ""`).

---

## A. VCS interface changes

### Current state

`VCS` lives at `vcs.go:14-96` and already has SHA-aware primitives:

- `FileContentAtRef(path, ref, dir)` (`vcs.go:85`) — implemented for git via
  `git show <ref>:<path>` (`git.go:170-181`) and for sapling via `sl cat -r`
  (`sapling.go:284-297`).
- `ChangedFilesForCommit(sha, dir)` (`vcs.go:46`) — single-commit changes.
- `FileDiffForCommit(path, sha, dir)` (`vcs.go:58`) — single-commit hunks.
- `MergeBase(ref)` (`vcs.go:34`).

There is **no** primitive for `<baseSHA>..<headSHA>`. `ChangedFilesFromBaseInDir`
(`vcs.go:40`) accepts a baseRef but always diffs against the working tree
(`git.go:434-455`: `git diff <base> --name-status`, no head argument).

### Additions

Add two methods to `VCS`:

```go
// ChangedFilesBetweenSHAs returns the files changed in the range baseSHA..headSHA.
// Renames are reported with status "renamed" and the new path; the old path is
// not surfaced (matches existing parseNameStatus behavior in git.go:570-600).
// Binary files are returned with their textual status; FileDiffBetweenSHAs is
// responsible for detecting "Binary files differ" and returning empty hunks.
ChangedFilesBetweenSHAs(baseSHA, headSHA, dir string) ([]FileChange, error)

// FileDiffBetweenSHAs returns parsed diff hunks for path in the range
// baseSHA..headSHA. Returns (nil, nil) if there is no diff for that path.
FileDiffBetweenSHAs(path, baseSHA, headSHA, dir string) ([]DiffHunk, error)

// ReadFileAtSHA returns the bytes of path at the given SHA. Returns
// (nil, nil) when the file does not exist at that SHA (deleted/added cases).
// Errors are reserved for "git command failed" (e.g. SHA not present locally).
ReadFileAtSHA(sha, path, dir string) ([]byte, error)

// HasObject reports whether the given SHA is reachable in the local object
// store. Used by the daemon before resolving a range so we can prompt-fetch
// missing objects rather than failing mid-render.
HasObject(sha, dir string) bool
```

`ChangedFile` already exists as `FileChange` (`git.go:17-20`). Reuse it; do not
introduce a parallel type.

### Git implementation (`git_vcs.go`)

Each method delegates to a package-level helper in `git.go`, matching the
existing pattern (`git_vcs.go:14-113`). Skeletons:

```go
// git.go (new)

// ChangedFilesBetweenSHAs runs `git diff --name-status <base>..<head>`.
// Note the two-dot range: we want files reachable from head that differ from
// base, not symmetric difference (...).
func ChangedFilesBetweenSHAs(baseSHA, headSHA, dir string) ([]FileChange, error) {
    cmd := exec.Command("git", "diff", "--name-status", "-M", baseSHA+".."+headSHA)
    if dir != "" {
        cmd.Dir = dir
    }
    out, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("git diff %s..%s --name-status: %w", baseSHA, headSHA, err)
    }
    return parseNameStatus(string(out)), nil
}

// FileDiffBetweenSHAs mirrors fileDiffUnified but takes both endpoints.
func FileDiffBetweenSHAs(path, baseSHA, headSHA, dir string) ([]DiffHunk, error) {
    cmd := exec.Command("git", "diff",
        "--no-color", "--no-ext-diff",
        baseSHA+".."+headSHA, "--", path)
    if dir != "" {
        cmd.Dir = dir
    }
    out, err := cmd.Output()
    if err != nil {
        var exitErr *exec.ExitError
        if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
            return nil, fmt.Errorf("git diff: %w", err)
        }
    }
    return ParseUnifiedDiff(string(out)), nil
}

// ReadFileAtSHA returns nil bytes (no error) when the file does not exist at sha.
// Distinguishes "missing path" (exit 128) from "command failed" (other exits).
func ReadFileAtSHA(sha, path, dir string) ([]byte, error) {
    cmd := exec.Command("git", "show", sha+":"+path)
    if dir != "" {
        cmd.Dir = dir
    }
    var stderr bytes.Buffer
    cmd.Stderr = &stderr
    out, err := cmd.Output()
    if err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
            return nil, nil // path not present at sha — not an error
        }
        return nil, fmt.Errorf("git show %s:%s: %s", sha, path, strings.TrimSpace(stderr.String()))
    }
    return out, nil
}

// HasObject uses `git cat-file -e <sha>^{commit}` which exits 0 iff the
// commit object exists locally. Cheap (no walk).
func HasObject(sha, dir string) bool {
    cmd := exec.Command("git", "cat-file", "-e", sha+"^{commit}")
    if dir != "" {
        cmd.Dir = dir
    }
    return cmd.Run() == nil
}
```

Notes:
- `-M` enables rename detection in `--name-status` so we get `R100\told\tnew`
  lines that `parseNameStatus` (`git.go:583-587`) already handles.
- Binary files: `git diff` emits `Binary files a/x and b/x differ`. `ParseUnifiedDiff`
  (`git.go:777-864`) silently skips lines that don't start with `@@` / ` ` / `+` /
  `-`, so we get empty hunks — same as today's behavior for binary files in the
  working-tree path. No new code needed.

### Sapling implementation (`sapling.go`)

Sapling has equivalents:

- `sl status --rev <base> --rev <head>` for the file list.
- `sl diff --rev <base> --rev <head> <path>` for hunks.
- `sl cat -r <sha> <path>` already used by `FileContentAtRef`
  (`sapling.go:284-297`).
- `sl log -r <sha> -T '{node}'` for object existence (no native `cat-file -e`).

```go
func (s *SaplingVCS) ChangedFilesBetweenSHAs(baseSHA, headSHA, dir string) ([]FileChange, error) {
    out, err := slCommandInDir(dir, "status", "--rev", baseSHA, "--rev", headSHA)
    if err != nil {
        return nil, err
    }
    return parseSaplingStatus(out), nil
}

func (s *SaplingVCS) FileDiffBetweenSHAs(path, baseSHA, headSHA, dir string) ([]DiffHunk, error) {
    cmd := exec.Command("sl", "diff", "--rev", baseSHA, "--rev", headSHA, path)
    if dir != "" {
        cmd.Dir = dir
    }
    out, _ := cmd.Output()
    return ParseUnifiedDiff(string(out)), nil
}

func (s *SaplingVCS) ReadFileAtSHA(sha, path, dir string) ([]byte, error) {
    cmd := exec.Command("sl", "cat", "-r", sha, path)
    if dir != "" {
        cmd.Dir = dir
    }
    out, err := cmd.Output()
    if err != nil {
        // sapling exits non-zero for missing path; can't reliably distinguish.
        // Treat as "not present", matching git behavior contract.
        return nil, nil
    }
    return out, nil
}

func (s *SaplingVCS) HasObject(sha, dir string) bool {
    err := exec.Command("sl", "log", "-r", sha, "-T", "{node}").Run()
    return err == nil
}
```

Sapling caveat: in repos where the SHA is a git commit not yet imported into
`sl`'s commit cloud, `HasObject` may return false even when the equivalent git
object exists. We accept that in v1 — the picker's "Other PRs" path is git-only
anyway (powered by `gh`).

### Auto-fetch missing SHAs

When `HasObject` returns false for either endpoint, we attempt to fetch the
SHA before failing. Only attempted for git; for sapling, surface the
missing-object error with a hint. Auto-fetch is **only** attempted for SHAs
that came from `gh` (we trust those came from the PR's repos). For SHAs the
user typed via `--range`, we fail loudly rather than guessing what remote
they meant.

**Fork-PR fallback.** A fork-PR's head SHA lives on the contributor's fork,
not `origin`. The first `git fetch origin <sha>` will fail. If `PRInfo` carries
a fork URL (we extend the `gh pr view` `--json` query to include
`headRepository.url` and `isCrossRepository` — see §D), the second attempt
fetches from the fork URL. If both fail, the error message points at manual
fetch.

**v1 limitation — HTTPS auth prompts.** Fork PRs from private repos that
require HTTPS auth may produce a 30s pause before timing out (the 30s
context cancels `git fetch` while it waits for an interactive credential
prompt). v2 can prefer SSH or detect anonymously-unreadable forks before
fetching.

```go
// Canonical signature — referenced verbatim by Task 5 step 7 in the plan.
// Note: forkURL may be "" when the PR is not from a fork (or when the caller
// is `--range`, where there is no PR). Final-stage callers always pass a value
// (possibly empty) — never nil.
func ensureSHAFetched(vcs VCS, sha, repoRoot, forkURL string) error {
    if vcs.HasObject(sha, repoRoot) {
        return nil
    }

    if vcs.Name() == "sapling" {
        return ensureSHAFetchedSapling(vcs, sha, repoRoot)
    }
    if vcs.Name() != "git" {
        return fmt.Errorf("commit %s not present locally (auto-fetch not supported for vcs=%q)", sha, vcs.Name())
    }

    // First attempt: origin. Suffices for same-repo PRs.
    if err := tryGitFetch(repoRoot, "origin", sha); err == nil &&
        vcs.HasObject(sha, repoRoot) {
        return nil
    }

    // Second attempt: fork URL, if known.
    if forkURL != "" {
        if err := tryGitFetch(repoRoot, forkURL, sha); err == nil &&
            vcs.HasObject(sha, repoRoot) {
            return nil
        }
        return fmt.Errorf(
            "commit %s not present locally; tried origin and fork %s — manual fetch required",
            sha, forkURL)
    }
    return fmt.Errorf("commit %s not present locally; manual fetch required (run `git fetch <remote> %s`)", sha, sha)
}

// ensureSHAFetchedSapling tries sapling-aware pull first, then falls back to
// git fetch when the repo has both .sl and .git (sapling-on-git, the common
// case). Why two attempts: sapling's commit cloud may not have the SHA but
// the underlying git repo (origin) often does.
func ensureSHAFetchedSapling(vcs VCS, sha, repoRoot string) error {
    // Attempt 1: `sl pull -r <sha>` — sapling-aware, works for both
    // pure-sapling and sapling-on-git when the SHA is reachable from
    // configured paths.
    if err := trySLPull(repoRoot, sha); err == nil &&
        vcs.HasObject(sha, repoRoot) {
        return nil
    }
    // Attempt 2: fall back to `git fetch origin <sha>` if .git exists
    // alongside .sl. Sapling-on-git stores objects in the underlying git
    // repo, so a git fetch populates them and `sl` will see them on the
    // next `HasObject` check.
    if hasGitDirAt(repoRoot) {
        if err := tryGitFetch(repoRoot, "origin", sha); err == nil &&
            vcs.HasObject(sha, repoRoot) {
            return nil
        }
    }
    return fmt.Errorf("commit %s not present locally; tried `sl pull -r %s` and `git fetch origin %s` — run `sl pull` manually with the right source", sha, sha, sha)
}

// hasGitDirAt reports whether repoRoot has a .git/ directory (either at the
// root or as a parent — sapling-on-git always has both). Cheap stat check.
func hasGitDirAt(repoRoot string) bool {
    info, err := os.Stat(filepath.Join(repoRoot, ".git"))
    return err == nil && info.IsDir()
}

// tryGitFetch shells `git fetch <remote> <sha>` with a 30s timeout.
// Local git ops are normally context-free in this codebase (see CLAUDE.md
// Code Conventions), but `git fetch` is the one path that touches the
// network and warrants a timeout — so this is the documented exception.
func tryGitFetch(repoRoot, remote, sha string) error {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctx, "git", "fetch", remote, sha)
    cmd.Dir = repoRoot
    return cmd.Run()
}

// trySLPull shells `sl pull -r <sha>` with a 30s timeout. Same exception
// as tryGitFetch: this is the network path, the rest of the codebase is
// context-free local ops.
func trySLPull(repoRoot, sha string) error {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    cmd := exec.CommandContext(ctx, "sl", "pull", "-r", sha)
    cmd.Dir = repoRoot
    return cmd.Run()
}
```

---

## B. Focus model

### Idiomatic Go shape: discriminator struct, not interface

```go
// FocusKind tags which arm of Focus is populated.
type FocusKind string

const (
    FocusWorkingTree FocusKind = "working_tree"
    FocusRange       FocusKind = "range"
)

// DiffScope selects which range to diff in FocusRange mode.
//   layer      — BaseSHA..HeadSHA  (what GitHub shows for the PR)
//   full_stack — DefaultSHA..HeadSHA (cumulative from default branch)
// Empty string is the implicit "no scope" used by FocusWorkingTree comments
// authored before this feature shipped.
type DiffScope string

const (
    DiffScopeLayer     DiffScope = "layer"
    DiffScopeFullStack DiffScope = "full_stack"
)

// Focus is what the session is currently showing. Exactly one arm is meaningful
// per Kind; the other fields are zero. No interface — keep it serializable for
// /api/session, comparable, and trivially copyable. Sum types in Go are usually
// worse than this struct in practice.
type Focus struct {
    Kind FocusKind `json:"kind"`

    // FocusWorkingTree fields.
    BaseRef        string `json:"base_ref,omitempty"`         // e.g. merge-base SHA
    BaseBranchName string `json:"base_branch_name,omitempty"` // e.g. "main"

    // FocusRange fields. All optional except BaseSHA + HeadSHA.
    PRNumber    int    `json:"pr_number,omitempty"`
    PRURL       string `json:"pr_url,omitempty"`
    Label       string `json:"label,omitempty"`     // e.g. "PR #123" or "abc1234..def5678"
    BaseSHA     string `json:"base_sha,omitempty"`     // PR baseRefOid (or A in --range A..B)
    HeadSHA     string `json:"head_sha,omitempty"`     // PR headRefOid (or B)
    DefaultSHA  string `json:"default_sha,omitempty"`  // tip of repo default branch; empty if unresolved
    ForkURL     string `json:"fork_url,omitempty"`     // PR head is on a fork; passed to ensureSHAFetched
    BaseRefName string `json:"base_ref_name,omitempty"` // e.g. "main"
    HeadRefName string `json:"head_ref_name,omitempty"` // e.g. "feature-b"
    DiffScope   DiffScope `json:"diff_scope,omitempty"` // "layer" (default) or "full_stack"
    IsStacked   bool   `json:"is_stacked,omitempty"`   // BaseRefName != repo default branch
}

// ReadOnly reports whether comments may be added/edited in this focus.
// v1: false. Range mode is fully writable so users can annotate while reviewing
// — push to platform is gated separately (§B "Push gate").
// (Earlier draft made Range mode read-only; we adopt prior art's model:
// comments per-scope, push gated server-side instead.)
func (f Focus) ReadOnly() bool { return false }

// DiffBaseSHA returns the SHA to use as the diff base for the current scope.
// Pure function of fields — never errors; if the scope is full_stack but
// DefaultSHA is unset, returns BaseSHA (caller is expected to have prevented
// this combination via SetFocus validation).
func (f Focus) DiffBaseSHA() string {
    if f.Kind != FocusRange {
        return f.BaseRef
    }
    if f.DiffScope == DiffScopeFullStack && f.DefaultSHA != "" {
        return f.DefaultSHA
    }
    return f.BaseSHA
}

// FullStackAvailable reports whether the full-stack scope can be selected.
// False when DefaultSHA could not be resolved (detached HEAD, no remote, etc.).
func (f Focus) FullStackAvailable() bool {
    return f.Kind == FocusRange && f.DefaultSHA != ""
}

// PickerVisible reports whether the layer/full-stack picker should render.
// Prior art's rule: hide when the PR is not stacked (base IS the default
// branch), because layer and full-stack would be identical.
func (f Focus) PickerVisible() bool {
    return f.Kind == FocusRange && f.IsStacked
}
```

The `Range` arm is now a single struct, not nested — keeps JSON shape flat for
the wire. `Focus.DiffScope` is the per-session toggle the picker drives.

**Read-only correction.** The earlier draft of this spec said Range mode is
read-only and rejected all writes with 403. We adopt prior art's model
instead: range comments are first-class, tagged with their scope, and the
**push-to-platform** path is what's gated server-side. See §B "Push gate" and
§E for storage.

Why discriminator, not interface:
- Serializes to JSON cleanly — single shape for `/api/session` and the picker.
- `f.ReadOnly()` is a one-liner; no type-switch boilerplate.
- The two arms share most data (refs are still useful in Range mode for the UI
  banner) — interfaces would force duplication.
- Matches existing patterns in the codebase: `Comment.Scope` (`session.go:69`)
  and `FileChange.Status` (`git.go:17-20`) are both stringly-tagged structs, not
  interfaces.

### Slot in `serverConfig`

Extend `serverConfig` (`main.go:1630-1649`) with a parsed focus. Keep
`baseBranch` for the working-tree case — the focus is computed *from* it.

```go
type serverConfig struct {
    // ... existing fields ...

    // focus describes what the session should show. Built by resolveServerConfig
    // from --pr / --range / nothing. Nil means "default" — derive working-tree
    // focus inside the session as today.
    focus *Focus
}
```

### Slot in `Session`

Extend `Session` (`session.go:173-219`):

```go
type Session struct {
    // ... existing fields ...

    Focus Focus // discriminator; never nil — defaults to FocusWorkingTree.
}
```

`Session.BaseRef` (`session.go:179`) stays. In `FocusWorkingTree`, it's the
merge-base SHA as today. In `FocusRange`, it's the *base* SHA of the range — so
existing call sites that pass `s.BaseRef` to git diff functions Just Work.
`Session.Branch` similarly stays (display purposes).

### Push gate (server-side, full-stack only)

Local annotations are unrestricted in any focus/scope. What's gated is
**pushing comments back to GitHub** (the `gh` review API). When the focus is
range and `DiffScope == full_stack`, line numbers don't correspond to what
GitHub knows about the PR — the diff includes commits from earlier layers, so
positions would land in the wrong place or be rejected.

`crit push` lives in `main.go` (`runPush`, dispatched from `commandDispatch`
at `main.go:54`). It builds comments via `critJSONToGHComments`
(`github.go:632-658`) and POSTs via `createGHReview` (`github.go:691-745`).
`crit push` does **not** go through HTTP — it reads the centralized review
file directly. So the gate lives in `runPush`, before `createGHReview`:

```go
// In runPush (main.go), after loading CritJSON and before pushing:
if cj.ActiveDiffScope == string(DiffScopeFullStack) {
    return fmt.Errorf("Switch to Layer diff before posting a platform review")
}
```

Where does `cj.ActiveDiffScope` come from? Add a single field to `CritJSON`
(`session.go:222-234`) that records the most recent focus diff_scope used
during this session. `Session.SetFocus` writes it whenever scope changes (and
the debounced `WriteFiles` flushes to disk). On `crit push`:
- Empty string or `"layer"` → push proceeds.
- `"full_stack"` → exit 1 with the message above.

There's also an HTTP equivalent for completeness, since the frontend may add a
"Push to GitHub" button later. Add:

```
POST /api/push    →  forbidden when focus.diff_scope == "full_stack"
                     status 409, body {"error": "Switch to Layer diff before posting a platform review"}
```

This handler is new; if not implemented in v1, the message in the picker UI
("Posting requires Layer scope") is enough — and the CLI gate is sufficient.

### What is *not* gated

Per prior art's model:
- `POST /api/file/comments` — allowed in any focus/scope. Comments get tagged
  with `DiffScope`.
- `PUT/DELETE /api/comment/{id}` — allowed in any focus/scope, regardless of
  the comment's stored `DiffScope` (you can edit a layer comment while
  viewing full-stack — the comment just won't be visible in the current view
  until you switch back).
- `POST /api/finish` — allowed.
- `POST /api/base-branch` — only meaningful in working-tree mode; leave
  unguarded but document that it's a no-op in range mode (the field it
  changes, `Session.BaseRef`, is ignored when `Focus.Kind == FocusRange`).
- `POST /api/agent/request` — allowed; agent replies tag with current scope.

### What IS gated (additional to the push gate)

- `POST /api/round-complete` — **rejected** with HTTP 409 + body
  `{"error": "round-complete is not meaningful in range mode"}` when
  `Focus.Kind == FocusRange`. Range mode pins the diff to fixed SHAs;
  triggering a new round (which would re-run `git diff` against `Session.BaseRef`)
  would produce inconsistent state. The user should switch focus to working
  tree if they want multi-round behavior.

A small inline guard in `handleRoundComplete` (`server.go:861-868`) is
sufficient — no shared middleware needed for one route.

```go
func (s *Server) handleRoundComplete(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    sess := s.session.Load()
    if sess.Focus.Kind == FocusRange {
        // Read s.Focus under sess.mu — see "Locking" note in §B.
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusConflict)
        json.NewEncoder(w).Encode(map[string]string{
            "error": "round-complete is not meaningful in range mode",
        })
        return
    }
    sess.SignalRoundComplete()
    writeJSON(w, map[string]string{"status": "ok"})
}
```

### Daemon HTTP API

Add focus to `/api/session` (`server.go:292-300`). Today it returns a
`SessionInfo`; extend that struct (in session.go near `SessionInfo`):

```go
type SessionInfo struct {
    // ... existing fields ...
    Focus Focus `json:"focus"`
}
```

Populate in `GetSessionInfoScoped` and the regular `GetSessionInfo`. The
frontend reads `session.focus` — there is no session-wide `read_only` flag
(see §B "Push gate" — gating is per-action, not per-session). No new endpoint
needed for the toggle; `POST /api/focus` handles all transitions.

**Locking.** `s.Focus` is a value field; reading it concurrently with
`SetFocus` is a data race unless held under `s.mu`. Both `GetSessionInfo`
and `GetSessionInfoScoped` use the existing snapshot pattern
(`snapshotForScoped` at `session.go:2420` takes RLock once and copies what
it needs). Extend `scopedSessionSnapshot` to include `focus Focus` and copy
`s.Focus` inside that RLock. The same applies to `GetComments` /
`GetReviewComments` — they must read `s.Focus` under the same RLock as the
comment slice they're filtering.

For switching focus mid-session, add **one** new endpoint:

```
POST /api/focus
Body: {"kind": "range", "base_sha": "...", "head_sha": "...",
        "label": "...", "pr_number": 123, "base_ref_name": "main",
        "head_ref_name": "feat-b"}
   or  {"kind": "working_tree"}
```

Handler in `server.go`:

```go
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var req Focus
    r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Bad request", http.StatusBadRequest)
        return
    }
    if err := s.session.Load().SetFocus(req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    writeJSON(w, map[string]string{"status": "ok"})
}
```

**`Session.SetFocus` skeleton.** Mirrors the rollback discipline of
`Session.ChangeBaseBranch` (`session.go:1408-1507`): snapshot enough state to
restore on failure, mutate, on error restore.

Note on real APIs (these are the only persist/SSE/lookup primitives in the
codebase — do not invent helpers):

- **Persist trigger**: `s.scheduleWrite()` (`session.go:1510`). Debounces a
  `WriteFiles()` 200ms later. Routes through `saveCritJSON()` per project
  rules (CLAUDE.md "Route all review file writes through saveCritJSON").
- **SSE**: `s.notify(SSEEvent{Type: "focus-changed", Content: <json>})`
  (`session.go:2101`). There is no `s.emit(...)`.
- **Comment lookup**: `s.FindCommentByID(id, filePath) (Comment, string, bool)`
  (`session.go:1024`) or `s.GetComments(filePath) []Comment`
  (`session.go:1004`). There is no `s.findComment(id)`.

```go
// SetFocus atomically swaps the session's Focus and rebuilds the file list.
// On any failure during rebuild, the previous Focus + Files are restored.
//
// Caller (handleFocus) has already validated request shape; SetFocus owns
// SHA validation and ensureSHAFetched calls.
func (s *Session) SetFocus(f Focus) error {
    // Validate the full-stack ↔ DefaultSHA constraint up front.
    if f.Kind == FocusRange &&
        f.DiffScope == DiffScopeFullStack &&
        f.DefaultSHA == "" {
        return fmt.Errorf("full-stack scope requires a resolvable default branch tip")
    }

    s.mu.RLock()
    repoRoot := s.RepoRoot
    vcs := s.VCS
    s.mu.RUnlock()

    // Validate SHAs are present locally (or auto-fetch from the appropriate
    // remote — fork URL is "" for non-cross-repo PRs, see §A).
    if f.Kind == FocusRange {
        forkURL := f.ForkURL // populated by resolveFocus / handleFocus
        if err := ensureSHAFetched(vcs, f.BaseSHA, repoRoot, ""); err != nil {
            return err
        }
        if err := ensureSHAFetched(vcs, f.HeadSHA, repoRoot, forkURL); err != nil {
            return err
        }
        if f.DiffScope == DiffScopeFullStack && f.DefaultSHA != "" {
            if err := ensureSHAFetched(vcs, f.DefaultSHA, repoRoot, ""); err != nil {
                return err
            }
        }
    }

    // Snapshot for rollback.
    s.mu.Lock()
    oldFocus := s.Focus
    oldFiles := s.Files
    oldBaseRef := s.BaseRef
    s.mu.Unlock()

    newFiles, newBaseRef, err := s.buildFilesForFocus(f, vcs, repoRoot)
    if err != nil {
        // No mutation happened; nothing to roll back.
        return fmt.Errorf("rebuilding file list for focus: %w", err)
    }

    s.mu.Lock()
    s.Focus = f
    s.Files = newFiles
    s.BaseRef = newBaseRef
    s.mu.Unlock()

    // Re-anchor heuristic — only when transitioning between two range
    // focuses on the same PR but with a different head SHA (force-push,
    // new commits). See §E "Re-anchoring after head SHA change".
    // Working-tree ↔ range transitions don't reanchor (different anchor
    // semantics). Same-SHA toggles (layer ↔ full-stack) don't reanchor
    // either — heads haven't moved.
    if oldFocus.Kind == FocusRange && f.Kind == FocusRange &&
        oldFocus.HeadSHA != "" && f.HeadSHA != "" &&
        oldFocus.HeadSHA != f.HeadSHA {
        // Load current cj, run heuristic, save. saveCritJSON handles
        // atomic write + parent dir creation.
        critPath := s.critJSONPath()
        if cj, loadErr := loadCritJSON(critPath); loadErr == nil {
            re, st, miss := reanchorComments(vcs, repoRoot,
                oldFocus.HeadSHA, f.HeadSHA, &cj)
            if re+st+miss > 0 {
                if err := saveCritJSON(critPath, cj); err == nil {
                    payload, _ := json.Marshal(map[string]any{
                        "reanchored": re, "stale": st, "missing": miss,
                        "old": oldFocus.HeadSHA, "new": f.HeadSHA,
                    })
                    s.notify(SSEEvent{Type: "reanchor", Content: string(payload)})
                    log.Printf("Re-anchored %d / %d stale / %d missing after head moved %s..%s",
                        re, st, miss, oldFocus.HeadSHA[:7], f.HeadSHA[:7])
                }
            }
        }
    }

    // Persist active scope so `crit push` can read it (§E push gate).
    //
    // CRITICAL — clear on FocusWorkingTree. When toggling back to
    // working tree, f.DiffScope is "" (Range fields aren't populated for
    // FocusWorkingTree). We MUST persist that empty value so `crit push`
    // sees `cj.ActiveDiffScope == ""` and runs the working-tree gate path.
    // If we skipped the call when scope is empty, a stale "layer" would
    // linger from a previous range session and gate 2 would skip every
    // working-tree comment as "wrong scope". Always call, never short-circuit.
    if err := s.persistActiveDiffScope(string(f.DiffScope)); err != nil {
        // Roll back in-memory state to keep wire ↔ disk consistent.
        // (Rollback only restores in-memory state — disk persistence goes
        // through `atomicWriteFile` (daemon.go) so a partial write can't
        // desync disk from in-memory state. See `saveCritJSON`.)
        s.mu.Lock()
        s.Focus = oldFocus
        s.Files = oldFiles
        s.BaseRef = oldBaseRef
        s.mu.Unlock()
        return fmt.Errorf("persisting active diff scope: %w", err)
    }

    s.scheduleWrite() // 200ms debounced WriteFiles → saveCritJSON.

    eventBytes, _ := json.Marshal(map[string]any{"focus": f})
    s.notify(SSEEvent{Type: "focus-changed", Content: string(eventBytes)})
    return nil
}

// persistActiveDiffScope updates CritJSON.ActiveDiffScope on disk via the
// canonical save path (saveCritJSON, called from WriteFiles → atomicWriteFile
// in daemon.go). Implementation: load → set (including the empty-string
// case) → save through saveCritJSON. Do not write directly. See
// watch.go:mergeExternalCritJSON for the load-modify-save pattern already in
// use.
```

`Focus.ForkURL` is a new field on the Range arm (added below in §B "Idiomatic
Go shape" — update the struct to include it). It carries `PRInfo.HeadRepoURL`
through the wire so `handleFocus` and `SetFocus` can pass the correct URL to
`ensureSHAFetched` without re-fetching `gh pr view`.

Register the route in `NewServer` (`server.go:73-99`):

```go
mux.HandleFunc("/api/focus", s.withReady(s.handleFocus))
mux.HandleFunc("/api/picker", s.withReady(s.handlePicker))
```

(The picker endpoint is described in §F.)

---

## C. CLI surface

### Style: extend `flag.NewFlagSet`, not new framework

`parseServerFlags` (`main.go:1667-1703`) already uses the stdlib `flag` package.
Keep that.

```go
// Additions in parseServerFlags
prSpec    := fs.String("pr", "",    "Review a specific PR by number or URL (e.g. 295 or https://github.com/o/r/pull/295)")
rangeSpec := fs.String("range", "", "Review a commit range, base..head (e.g. abc1234..def5678)")
scopeFlag := fs.String("scope", "", "Diff scope when reviewing a PR: layer (default) or full-stack")
```

Push them into `serverFlagSet` and pass through. `--scope` is only meaningful
with `--pr`; it's ignored (with a stderr warning) for `--range` because an
arbitrary range doesn't have an implicit "stack" — the user can already say
`crit --range main..B` if that's what they want.

### Funnel: one `Focus` builder

```go
// resolveFocus turns CLI inputs into a *Focus, or nil for working-tree default.
// Mutually exclusive: errors if both --pr and --range are given.
func resolveFocus(prSpec, rangeSpec, scopeSpec string, vcs VCS, repoRoot string) (*Focus, error) {
    if prSpec != "" && rangeSpec != "" {
        return nil, fmt.Errorf("--pr and --range are mutually exclusive")
    }
    scope, err := parseScopeSpec(scopeSpec) // "" → DiffScopeLayer
    if err != nil {
        return nil, err
    }
    if scopeSpec != "" && rangeSpec != "" {
        fmt.Fprintln(os.Stderr, "Note: --scope is ignored with --range; pass an explicit base..head instead")
    }
    switch {
    case prSpec != "":
        prNum, err := parsePRSpec(prSpec)
        if err != nil {
            return nil, err
        }
        info, err := fetchPRByNumber(prNum)
        if err != nil {
            return nil, fmt.Errorf("resolving PR #%d: %w", prNum, err)
        }
        // forkURL is empty for same-repo PRs and for the base SHA (always on origin).
        forkURL := ""
        if info.IsCrossRepository {
            forkURL = info.HeadRepoURL
        }
        if err := ensureSHAFetched(vcs, info.BaseRefOid, repoRoot, ""); err != nil {
            return nil, err
        }
        if err := ensureSHAFetched(vcs, info.HeadRefOid, repoRoot, forkURL); err != nil {
            return nil, err
        }

        // Resolve repo default-branch tip for full-stack support. Best effort —
        // empty DefaultSHA simply disables the full-stack option.
        defaultBranch := vcs.DefaultBranch()
        defaultSHA, _ := ResolveDefaultBranchSHA(vcs, repoRoot, defaultBranch)
        isStacked := info.BaseRefName != defaultBranch

        // Strict check when user explicitly asked for full-stack.
        if scope == DiffScopeFullStack && defaultSHA == "" {
            return nil, fmt.Errorf("--scope=full-stack requires a resolvable default branch tip; got none for %q (detached HEAD or no remote?)", defaultBranch)
        }

        return &Focus{
            Kind:        FocusRange,
            PRNumber:    info.Number,
            PRURL:       info.URL,
            Label:       fmt.Sprintf("PR #%d: %s", info.Number, info.Title),
            BaseSHA:     info.BaseRefOid,
            HeadSHA:     info.HeadRefOid,
            DefaultSHA:  defaultSHA,
            ForkURL:     forkURL, // empty for non-cross-repo PRs
            BaseRefName: info.BaseRefName,
            HeadRefName: info.HeadRefName,
            DiffScope:   scope,
            IsStacked:   isStacked,
        }, nil

    case rangeSpec != "":
        base, head, err := parseRangeSpec(rangeSpec)
        if err != nil {
            return nil, err
        }
        if !vcs.HasObject(base, repoRoot) {
            return nil, fmt.Errorf("base SHA %s not present locally", base)
        }
        if !vcs.HasObject(head, repoRoot) {
            return nil, fmt.Errorf("head SHA %s not present locally", head)
        }
        return &Focus{
            Kind:      FocusRange,
            BaseSHA:   base,
            HeadSHA:   head,
            Label:     fmt.Sprintf("%s..%s", short(base), short(head)),
            DiffScope: DiffScopeLayer, // ranges are always "layer" — they ARE explicit
            IsStacked: false,           // no picker for raw ranges
        }, nil
    }
    return nil, nil
}

// parseScopeSpec maps the --scope flag to a DiffScope. Accepts "layer" or
// "full-stack" / "full_stack". Empty string defaults to layer.
func parseScopeSpec(s string) (DiffScope, error) {
    switch s {
    case "", "layer":
        return DiffScopeLayer, nil
    case "full-stack", "full_stack":
        return DiffScopeFullStack, nil
    default:
        return "", fmt.Errorf("invalid --scope value %q (expected layer or full-stack)", s)
    }
}

var prURLRe = regexp.MustCompile(`^https?://[^/]+/[^/]+/[^/]+/pull/(\d+)(?:[/?#].*)?$`)

func parsePRSpec(spec string) (int, error) {
    if m := prURLRe.FindStringSubmatch(spec); m != nil {
        return strconv.Atoi(m[1])
    }
    n, err := strconv.Atoi(spec)
    if err != nil || n <= 0 {
        return 0, fmt.Errorf("invalid --pr value %q (expected number or https://.../pull/N URL)", spec)
    }
    return n, nil
}

// parseRangeSpec splits "base..head" with strict validation. Three dots ("...")
// are explicitly rejected — the symmetric-difference semantic is not what users
// expect from PR review.
func parseRangeSpec(spec string) (base, head string, err error) {
    if strings.Contains(spec, "...") {
        return "", "", fmt.Errorf("--range expects two-dot syntax (base..head), got %q", spec)
    }
    parts := strings.SplitN(spec, "..", 2)
    if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
        return "", "", fmt.Errorf("invalid --range value %q (expected base..head)", spec)
    }
    return parts[0], parts[1], nil
}

func short(sha string) string {
    if len(sha) > 7 { return sha[:7] }
    return sha
}

// IsStackedPR reports whether the PR's base branch is something other than the
// repo's default branch. Prior art's heuristic for "is this a stacked PR?".
// Limitation: a PR whose base IS the default branch but is part of a longer
// logical stack tracked elsewhere will be misclassified as not stacked. See §H.
//
// CANONICAL SIGNATURE — any stub in earlier tasks must match this exactly.
// Does not need repoRoot: vcs.DefaultBranch() reads cached state from the VCS
// instance, not the working directory.
func IsStackedPR(info *PRInfo, vcs VCS) bool {
    if info == nil || vcs == nil {
        return false
    }
    return info.BaseRefName != vcs.DefaultBranch()
}

// ResolveDefaultBranchSHA returns the tip SHA of the repo's default branch,
// preferring the remote (origin) and falling back to local. Used to compute
// the full-stack diff base. Returns ("", err) on failure — callers treat
// that as "full-stack unavailable" rather than fatal.
//
// No context.Context: per project convention (CLAUDE.md "Code Conventions"),
// local git ops complete in ms and don't take a context.
//
// CANONICAL SIGNATURE — any stub in earlier tasks must match this exactly.
func ResolveDefaultBranchSHA(vcs VCS, repoRoot, defaultBranch string) (string, error) {
    if vcs == nil || defaultBranch == "" {
        return "", fmt.Errorf("default branch unknown")
    }
    // Try origin/<branch> first. We can't go through the VCS interface for this
    // (no method); use exec directly for git, sl-equivalent for sapling.
    if vcs.Name() == "git" {
        out, err := runGitInDir(repoRoot, "rev-parse", "--verify", "origin/"+defaultBranch)
        if err == nil {
            return strings.TrimSpace(out), nil
        }
        out, err = runGitInDir(repoRoot, "rev-parse", "--verify", defaultBranch)
        if err == nil {
            return strings.TrimSpace(out), nil
        }
        return "", fmt.Errorf("could not resolve %s tip: %w", defaultBranch, err)
    }
    // Sapling: try remote/<branch> (sapling's remote bookmark name) then bookmark.
    out, err := slCommandInDir(repoRoot, "log", "-r", "remote/"+defaultBranch, "-T", "{node}")
    if err == nil && strings.TrimSpace(out) != "" {
        return strings.TrimSpace(out), nil
    }
    out, err = slCommandInDir(repoRoot, "log", "-r", defaultBranch, "-T", "{node}")
    if err == nil && strings.TrimSpace(out) != "" {
        return strings.TrimSpace(out), nil
    }
    return "", fmt.Errorf("could not resolve %s tip: %w", defaultBranch, err)
}

// runGitInDir is a small helper used by IsStackedPR / ResolveDefaultBranchSHA.
// Mirrors the existing inline `exec.Command("git", ...)` pattern in git.go.
func runGitInDir(dir string, args ...string) (string, error) {
    cmd := exec.Command("git", args...)
    if dir != "" {
        cmd.Dir = dir
    }
    out, err := cmd.Output()
    return string(out), err
}
```

`resolveServerConfig` (`main.go:1742-1785`) calls `resolveFocus` after VCS
detection and assigns to `sc.focus`.

### Subcommand variant: `crit pr <num>`

`commandDispatch` (`main.go:41-64`) dispatches by first arg. Add:

```go
"pr": runPR,
```

`runPR` is a thin shim: validate one arg, then forward to `runReview` with
`["--pr", arg]` prepended. This means the daemon sees the same flags and
funnels through the same path.

```go
func runPR(args []string) {
    if len(args) != 1 {
        fmt.Fprintln(os.Stderr, "Usage: crit pr <num|url>")
        os.Exit(1)
    }
    runReview([]string{"--pr", args[0]})
}
```

### `crit comment` scope inheritance

`crit comment` writes directly to the review file via `addCommentToCritJSON`
/ `addFileCommentToCritJSON` / `addReviewCommentToCritJSON`
(`github.go:956+`). Today (`runComment` at `main.go:950`) it has **no** HTTP
probe and **no** Focus context — it just calls the helpers with the
configured `outputDir`. Without a fix, after this feature ships:
`crit --pr 295` … reviewer types `crit comment foo.go:42 "..."` in another
shell → comment is written with empty `DiffScope`, hidden in the PR view.
Looks like data loss.

The fix is a daemon probe before the disk write. There is no existing probe
to extend; this is new code in `runComment`.

```go
// In main.go (alongside runComment).

// commentFocusOverride captures the user's --scope flag.
type commentFocusOverride string

const (
    scopeOverrideUnset       commentFocusOverride = ""
    scopeOverrideLayer       commentFocusOverride = "layer"
    scopeOverrideFullStack   commentFocusOverride = "full-stack"
    scopeOverrideWorkingTree commentFocusOverride = "working-tree"
)

// inheritedScope is the (HeadSHA, DiffScope) pair that comment-write helpers
// will stamp on new comments. Both fields are empty for working-tree mode.
type inheritedScope struct {
    HeadSHA   string
    DiffScope string // "layer" | "full_stack" | ""
}

// resolveCommentScope decides which scope tags `crit comment` should stamp.
// Order of precedence:
//   1. --scope=working-tree → always empty (explicit reset).
//   2. --scope=layer or --scope=full-stack → use that, sourced HeadSHA from
//      daemon if available; otherwise leave HeadSHA empty and warn.
//   3. No flag, daemon running with FocusRange → inherit from daemon focus.
//   4. No flag, no daemon, cj.ActiveDiffScope set → use it (HeadSHA empty,
//      warn that the daemon isn't running so head SHA can't be confirmed).
//   5. Otherwise → empty (working-tree comment, today's behavior).
func resolveCommentScope(override commentFocusOverride, outputDir string) (inheritedScope, error) {
    daemon := probeDaemonFocus(outputDir) // *Focus or nil; never errors

    switch override {
    case scopeOverrideWorkingTree:
        return inheritedScope{}, nil

    case scopeOverrideFullStack:
        if daemon != nil && daemon.Kind == FocusRange && daemon.DiffScope == DiffScopeFullStack {
            return inheritedScope{HeadSHA: daemon.HeadSHA, DiffScope: "full_stack"}, nil
        }
        // No live daemon — accept if disk says full_stack is the active scope.
        if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope == "full_stack" {
            return inheritedScope{DiffScope: "full_stack"}, nil
        }
        return inheritedScope{}, fmt.Errorf("--scope=full-stack: no active full-stack focus to attach to (start `crit --pr <n> --scope=full-stack` first)")

    case scopeOverrideLayer:
        if daemon != nil && daemon.Kind == FocusRange && daemon.DiffScope == DiffScopeLayer {
            return inheritedScope{HeadSHA: daemon.HeadSHA, DiffScope: "layer"}, nil
        }
        if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope == "layer" {
            return inheritedScope{DiffScope: "layer"}, nil
        }
        return inheritedScope{}, fmt.Errorf("--scope=layer: no active layer focus to attach to (start `crit --pr <n>` first)")

    case scopeOverrideUnset:
        // No flag — auto-inherit.
        if daemon != nil && daemon.Kind == FocusRange {
            return inheritedScope{
                HeadSHA:   daemon.HeadSHA,
                DiffScope: string(daemon.DiffScope),
            }, nil
        }
        if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope != "" {
            fmt.Fprintf(os.Stderr,
                "Note: stamping comment with diff_scope=%q from review file (no daemon running; head_sha unknown)\n",
                cj.ActiveDiffScope)
            return inheritedScope{DiffScope: cj.ActiveDiffScope}, nil
        }
        return inheritedScope{}, nil
    }

    return inheritedScope{}, fmt.Errorf("invalid --scope value %q", override)
}

// probeDaemonFocus contacts the running daemon (if any) and returns its
// Focus. Returns nil on any failure — no error path because this is
// best-effort. Pattern: same daemon-locator code already used by
// resolveReviewPathFromDaemon (github.go:517).
//
// On success, the daemon's /api/session response includes the Focus per
// §B "Daemon HTTP API" (SessionInfo.Focus).
func probeDaemonFocus(outputDir string) *Focus {
    cwd, err := resolvedCWD()
    if err != nil { return nil }
    sessions, _ := listSessionsForCWD(cwd)
    if len(sessions) == 0 { return nil }
    // Pick the alive session matching the current branch (mirrors
    // resolveReviewPathFromSessions logic). For range-mode daemons,
    // sessionKey is `pr:N` or `range:A..B` so multiple may co-exist —
    // any of them carries the active focus we want.
    sess := sessions[0]
    client := &http.Client{Timeout: 2 * time.Second}
    resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/session", sess.Port))
    if err != nil { return nil }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK { return nil }
    var info struct {
        Focus *Focus `json:"focus"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&info); err != nil { return nil }
    return info.Focus
}
```

**CLI flag.** Add `--scope=layer|full-stack|working-tree` to
`parseCommentFlags`. Default empty (auto-inherit).

**Wire-in.** Each of `addCommentToCritJSON`, `addFileCommentToCritJSON`,
`addReviewCommentToCritJSON`, `bulkAddCommentsToCritJSON`, and the JSON-bulk
path takes a new `scope inheritedScope` parameter (or, smaller diff: a free
function `stampCommentFromScope(c *Comment, s inheritedScope)` called right
after the existing `Comment{...}` construction). The resolution happens once
at the top of `runComment` and is threaded down.

**Reply path.** `crit comment --reply-to <id>` does not stamp — replies
inherit the parent's scope by being attached to it. No change needed for the
`addReplyToCritJSON` path.

**Testing strategy.** Pure logic in `resolveCommentScope` is unit-tested
with a fake `probeDaemonFocus` (factor it into an injectable function-typed
field so tests don't need a real HTTP server, or use `httptest.NewServer`
to serve `/api/session`). See plan Task 4b for the full test list.

`sessionKey` (`daemon.go:57-73`) hashes `cwd + branch + args`. For PR/range
sessions, **include** the `--pr` / `--range` arg so distinct PRs get distinct
daemons. This already happens by virtue of `args` being included for non-empty
file args — but in our case `len(sc.files) == 0` and the keying logic at
`daemon.go:64-67` falls back to including `branch`. We need to make sure
`--pr 295` produces a stable key.

The fix: `runReview` (`main.go:1444`) currently computes `key :=
sessionKey(cwd, branch, sc.files)`. Change to:

```go
keyArgs := sc.files
if sc.focus != nil && sc.focus.Kind == FocusRange {
    // Stable key for range/PR sessions, branch-independent.
    // Crucially: do NOT include DiffScope in the key. The picker must let
    // users toggle scopes within a single session; if scope was part of the
    // key, every toggle would spawn a new daemon.
    if sc.focus.PRNumber > 0 {
        keyArgs = []string{fmt.Sprintf("pr:%d", sc.focus.PRNumber)}
    } else {
        keyArgs = []string{fmt.Sprintf("range:%s..%s", sc.focus.BaseSHA, sc.focus.HeadSHA)}
    }
}
key := sessionKey(cwd, branch, keyArgs)
```

Mirror the same logic in `serveSessionKey` (`main.go:1860-1870`) so the daemon
computes the same key.

---

## D. GitHub integration

### Extend `PRInfo`

`PRInfo` (`github.go:70-84`) is missing the SHA fields we need. Add:

```go
type PRInfo struct {
    URL               string `json:"url"`
    Number            int    `json:"number"`
    Title             string `json:"title"`
    IsDraft           bool   `json:"isDraft"`
    State             string `json:"state"`
    Body              string `json:"body"`
    BaseRefName       string `json:"baseRefName"`
    HeadRefName       string `json:"headRefName"`
    BaseRefOid        string `json:"baseRefOid"`         // NEW
    HeadRefOid        string `json:"headRefOid"`         // NEW
    HeadRepoURL       string `json:"headRepoURL,omitempty"`       // NEW: fork URL for cross-repo PRs
    IsCrossRepository bool   `json:"isCrossRepository,omitempty"` // NEW: PR head is on a fork
    Additions         int    `json:"additions"`
    Deletions         int    `json:"deletions"`
    ChangedFiles      int    `json:"changedFiles"`
    AuthorLogin       string `json:"authorLogin"`
    CreatedAt         string `json:"createdAt"`
}
```

Update `prInfoRaw` (`github.go:139-153`) and the `--json` field list passed
to `gh pr view` at `github.go:163` to include
`baseRefOid,headRefOid,isCrossRepository,headRepository`. The
`headRepository` field is an object whose `url` is what we want; mirror the
existing nested-`author` pattern (`github.go:88-92`):

```go
// In github.go alongside prAuthor:
type prHeadRepo struct {
    URL string `json:"url"`
}

type prInfoRaw struct {
    // ... existing fields ...
    BaseRefOid        string     `json:"baseRefOid"`
    HeadRefOid        string     `json:"headRefOid"`
    IsCrossRepository bool       `json:"isCrossRepository"`
    HeadRepository    prHeadRepo `json:"headRepository"`
}
```

### `fetchPRByNumber`

```go
// fetchPRByNumber resolves a PR by number using `gh pr view <num>`.
// Returns nil if gh is unavailable, the PR doesn't exist, or it's MERGED/CLOSED.
// Unlike detectPRInfo, this works against an explicit number (no "current branch"
// inference) and includes the BaseRefOid/HeadRefOid fields needed for ranged review.
func fetchPRByNumber(num int) (*PRInfo, error) {
    if err := requireGH(); err != nil {
        return nil, err
    }
    out, err := exec.Command("gh", "pr", "view", strconv.Itoa(num), "--json",
        "number,url,title,isDraft,state,body,baseRefName,headRefName,baseRefOid,headRefOid,isCrossRepository,headRepository,additions,deletions,changedFiles,author,createdAt").Output()
    if err != nil {
        return nil, fmt.Errorf("gh pr view %d: %w", num, err)
    }
    return parsePRViewJSON(out)
}

// parsePRViewJSON is factored out for testing — feed it fixture bytes,
// no `gh` invocation needed. (Test-strategy follow-up: §G + plan Task 5.)
func parsePRViewJSON(out []byte) (*PRInfo, error) {
    var raw prInfoRaw
    if err := json.Unmarshal(out, &raw); err != nil {
        return nil, fmt.Errorf("parsing gh output: %w", err)
    }
    return &PRInfo{
        URL:               raw.URL,
        Number:            raw.Number,
        Title:             raw.Title,
        IsDraft:           raw.IsDraft,
        State:             raw.State,
        Body:              raw.Body,
        BaseRefName:       raw.BaseRefName,
        HeadRefName:       raw.HeadRefName,
        BaseRefOid:        raw.BaseRefOid,
        HeadRefOid:        raw.HeadRefOid,
        HeadRepoURL:       raw.HeadRepository.URL,
        IsCrossRepository: raw.IsCrossRepository,
        Additions:         raw.Additions,
        Deletions:         raw.Deletions,
        ChangedFiles:      raw.ChangedFiles,
        AuthorLogin:       displayName(raw.Author.Login, raw.Author.Name),
        CreatedAt:         raw.CreatedAt,
    }, nil
}
```

Decision: unlike `detectPRInfo` (`github.go:158-189`), do **not** filter out
MERGED/CLOSED PRs here. A user explicitly asking for `--pr 295` should be able
to review a merged PR; the comment-anchoring rules still apply because the head
SHA is fixed.

### `fetchOpenPRs` for the picker

```go
// PRSummary is the lightweight shape returned for the picker's "Other PRs"
// section. Distinct from PRInfo so we don't pay the cost of fetching body etc.
type PRSummary struct {
    Number      int    `json:"number"`
    Title       string `json:"title"`
    URL         string `json:"url"`
    HeadRefName string `json:"headRefName"`
    HeadRefOid  string `json:"headRefOid"`
    BaseRefName string `json:"baseRefName"`
    IsDraft     bool   `json:"isDraft"`
}

func fetchOpenPRs() ([]PRSummary, error) {
    if err := requireGH(); err != nil {
        return nil, err
    }
    // --limit 100: gh's max page size is 100. We don't paginate beyond that
    // in v1; document below. Bump or page if real-world feedback warrants.
    out, err := exec.Command("gh", "pr", "list",
        "--state", "open",
        "--limit", "100",
        "--json", "number,title,url,headRefName,headRefOid,baseRefName,isDraft",
    ).Output()
    if err != nil {
        return nil, fmt.Errorf("gh pr list: %w", err)
    }
    return parsePRListJSON(out)
}

// parsePRListJSON is factored for testing (same rationale as parsePRViewJSON).
func parsePRListJSON(out []byte) ([]PRSummary, error) {
    var prs []PRSummary
    if err := json.Unmarshal(out, &prs); err != nil {
        return nil, fmt.Errorf("parsing gh pr list: %w", err)
    }
    return prs, nil
}
```

**v1 limitation:** capped at 100 open PRs. Beyond that, the picker's "Other PRs"
section omits the rest. v2 may paginate via `--page <n>` if real-world usage
warrants it. The error message in `/api/picker` includes a note when the cap
is reached so the user understands why a known PR isn't visible.

The picker endpoint caches this response in memory for 60s — `gh pr list` can
take a few seconds on big orgs and the picker will be opened/closed multiple
times. Stub:

```go
type prListCache struct {
    mu      sync.Mutex
    fetched time.Time
    data    []PRSummary
}

func (c *prListCache) get() ([]PRSummary, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if time.Since(c.fetched) < 60*time.Second && c.data != nil {
        return c.data, nil
    }
    data, err := fetchOpenPRs()
    if err != nil {
        return nil, err
    }
    c.data = data
    c.fetched = time.Now()
    return data, nil
}
```

Live on the `Server` struct.

---

## E. Comment anchoring

### Where comments live today

- In-memory: `FileEntry.Comments` (`session.go:95`) and `Session.reviewComments`
  (`session.go:188`).
- On disk: `~/.crit/reviews/<key>.json`, schema `CritJSON` (`session.go:222-241`).
- Per-comment fields in `Comment` (`session.go:57-78`): `StartLine`, `EndLine`,
  `Side`, `Body`, `Quote`, `Anchor`, `Drifted`, `Author`, `UserID`, `Scope`,
  timestamps, `Resolved`, `Replies`, `GitHubID`. **No SHA.**

### Schema additions

Add to `Comment` (`session.go:57-78`):

```go
// HeadSHA is the head SHA of the focus when this comment was authored.
// For working-tree comments, this is empty (today's behavior).
// For range/PR comments, this is the focus.HeadSHA at write time.
HeadSHA string `json:"head_sha,omitempty"`

// DiffScope tags which range scope this comment belongs to: "layer",
// "full_stack", or "" (working tree / pre-feature). Filters the UI: a
// "layer" comment is hidden in full-stack view and vice versa.
//
// This field is NEW and distinct from Comment.Scope (which already exists
// with values "line" | "file" | "review"). Do not collide them.
DiffScope string `json:"diff_scope,omitempty"`

// Stale is set on read when HeadSHA != current focus.HeadSHA. Computed,
// not persisted. emitted only by the API serializer when relevant.
Stale bool `json:"stale,omitempty"`
```

Add to `CritJSON` (`session.go:222-234`):

```go
// ActiveDiffScope is the most recent focus.diff_scope from this session,
// flushed on every focus change. Read by `crit push` to gate full-stack
// pushes (§B "Push gate").
ActiveDiffScope string `json:"active_diff_scope,omitempty"`
```

Decision rationale: if `HeadSHA` no longer matches the current focus, mark the
comment as stale rather than auto-reanchoring. v1 ships staleness as a flag
the UI dims; reanchoring is a separate v2 concern.

### Migration

Existing comments in `~/.crit/reviews/<key>.json` have no `HeadSHA` or
`DiffScope`. They:
- never appear stale (rule below requires both sides non-empty);
- show only in **working-tree** focus. They are hidden in any range/PR focus
  regardless of scope. This is the intended behavior — pre-feature comments
  weren't authored against any specific SHA, so promoting them into a layer or
  full-stack view would conflate two distinct contexts.

Existing tests must continue to pass: when no `Focus` is set
(`Focus.Kind == FocusWorkingTree`), all comments render exactly as today.

### Write path

Every site that constructs a new `Comment{...}` literal must stamp scope.
Confirmed via `grep -n "Comment{" /Users/tomasztomczyk/Server/side/crit-mono/crit/*.go`
on the codebase as of this spec. **Authoring sites** (apply the stamp):

| File | Line | Function | Notes |
| --- | --- | --- | --- |
| `session.go` | 639 | `Session.AddComment` (line/range) | session has Focus; stamp from it |
| `session.go` | 668 | `Session.AddFileComment` | stamp |
| `session.go` | 688 | `Session.AddReviewComment` | stamp |
| `github.go` | 399 | `mergeRootComment` | `crit pull` import — see N-6 |
| `github.go` | 859 | `appendComment` | `crit comment` CLI + pull — see N-6 |
| `github.go` | 1317 | `appendReviewComment` | bulk comment CLI |
| `github.go` | 1341 | `appendFileComment` | bulk file comment CLI |
| `share.go` | 688 | `mergeWebComments` | crit-web import — see below |
| `watch.go` | 312 | `carryForwardComment` | **must preserve, not stamp** — see below |

**Authoring sites use a shared helper to avoid drift:**

```go
// stampWithFocus copies focus-derived metadata onto a freshly authored
// Comment. Apply at every Comment{} authoring site listed in the table
// above. No-op when Focus.Kind == FocusWorkingTree.
func stampWithFocus(c Comment, f Focus) Comment {
    if f.Kind == FocusRange {
        c.HeadSHA = f.HeadSHA
        c.DiffScope = string(f.DiffScope)
    }
    return c
}
```

Use as `c := stampWithFocus(Comment{...}, sess.Focus)` immediately before
appending to a slice or returning. Working-tree path is unchanged (the
no-op branch).

**Carry-forward (watch.go:312) — preserve, don't restamp.**
`carryForwardComment` builds a new `Comment{}` from an old one across review
rounds. Per CLAUDE.md "Code Conventions" — *enumerate ALL fields when
copying state* — extend this function to copy `HeadSHA` and `DiffScope`
**from `old`** (not from the current focus). Carrying forward must preserve
the comment's authored scope; otherwise toggling rounds would silently
strip scope tags. The session.go:74 `CarriedForward` flag already follows
this discipline; we extend it.

```go
// Updated carryForwardComment (watch.go:312) — only the new lines shown:
return Comment{
    // ... existing field copies ...
    HeadSHA:   old.HeadSHA,    // NEW
    DiffScope: old.DiffScope,  // NEW
    // ... existing field copies ...
}
```

**`crit pull` import path (`github.go:399, 859`).** Per N-6 below: when the
session is in `FocusRange` mode (or when `cj.ActiveDiffScope != ""` for the
non-daemon CLI path), stamp imported GitHub comments with
`DiffScope = "layer"` and `HeadSHA = <PR's headRefOid>`. Without this,
pulled comments fall through `visibleInFocus` and look like data loss in PR
mode. Implementation: thread the `(headSHA, diffScope)` pair into
`mergeRootComment` / `appendComment` as new parameters; default to empty
strings (preserves working-tree behavior).

**`crit-web` import path (`share.go:688`).** Web comments are authored
against the same content the local crit is reviewing. When the importing
session is in `FocusRange`, stamp them the same way as the pull path
(layer scope, current head SHA). When in `FocusWorkingTree`, leave both
empty.

**Initialization sites (do NOT stamp — these create empty slices):**
`session.go:246, 281, 493, 1165, 1356, 1388, 1483, 1598, 1960`,
`main.go:1833`, `github.go:374, 852, 1337`, `watch.go:138`. These all
construct `[]Comment{}` (the empty slice) or `Comment{}` zero values for
non-found returns. No mutation needed.

### Read / filter path

Comments are filtered by scope at read time. The single helper:

```go
// visibleInFocus reports whether c should be shown in the given focus.
//   FocusWorkingTree:        only comments with empty DiffScope (legacy + new
//                            working-tree comments).
//   FocusRange + scope=...:  only comments whose DiffScope matches.
// This is a pure function — no I/O, no locks. Easy to unit-test.
func visibleInFocus(c Comment, f Focus) bool {
    switch f.Kind {
    case FocusWorkingTree:
        return c.DiffScope == ""
    case FocusRange:
        return c.DiffScope == string(f.DiffScope)
    default:
        return true
    }
}

// annotateStaleness sets c.Stale when the comment's HeadSHA disagrees with the
// focus HEAD. Caller should call this on every comment before serializing.
func annotateStaleness(c Comment, focus Focus) Comment {
    if c.HeadSHA != "" && focus.HeadSHA != "" && c.HeadSHA != focus.HeadSHA {
        c.Stale = true
    }
    return c
}
```

Slot points:

- `GET /api/file/comments` → `Server.handleFileComments` GET branch
  (`server.go:439-443`) calls `s.session.Load().GetComments(path)`. Inside
  `GetComments` (in `session.go`), filter through `visibleInFocus(c,
  s.Focus)` and apply `annotateStaleness`.
- `GET /api/comments` (review-level) → `Server.handleReviewComments` GET branch
  (`server.go:754-757`) calls `GetReviewComments()`. Same treatment.
- `GET /api/session` → `Session.GetSessionInfoScoped` (`session.go:2493`)
  builds per-file `CommentCount`. The count must be the **filtered** count —
  otherwise badges lie. Replace the inline tally with one that walks
  `f.Comments` calling `visibleInFocus`.

The on-disk `~/.crit/reviews/<key>.json` always stores **all** comments
regardless of scope; filtering only happens on the way to the UI / the agent
prompt. This is what makes scope toggling lossless: comments persist across
toggles because nothing is ever discarded based on the active scope.

**Decision (N-6).** `visibleInFocus` does NOT include legacy/empty-scope
comments in `FocusRange` views. The fix lives on the **write side**
(§E "Write path"): import paths (`crit pull`, `mergeWebComments`) and the
`crit comment` CLI when a range-mode daemon is running stamp comments with
`DiffScope = "layer"` and the active head SHA, so they appear correctly.
Rationale: showing legacy comments in any range view would mix two contexts
(working-tree line numbers vs PR diff line numbers) and produce
confusing/wrong placements.

### Re-anchoring after head SHA change

When the focus's `HeadSHA` shifts (force-push, new commits on the PR
branch), v1 runs a best-effort heuristic to re-attach existing comments to
the new head's line numbers. Comments that can't be confidently re-anchored
are left at their old position with `Stale: true` (matching the
pre-feature behavior).

```go
// reanchorComments walks comments with c.HeadSHA == oldHead in this session's
// review file and tries to re-anchor each to newHead. Pure-ish — reads via
// VCS but doesn't mutate session state. Returns counts: reanchored, stale,
// missing. Caller (Session.SetFocus) writes the result back through
// saveCritJSON.
//
// Heuristic (per comment):
//   1. ReadFileAtSHA(oldHead, c.path) → extract lines [c.StartLine, c.EndLine].
//      If file missing at oldHead, skip (already orphaned; missing++).
//   2. ReadFileAtSHA(newHead, c.path) → search for the original lines.
//   3. Search window: ±20 lines around the original position.
//   4. Match strategy:
//      a. Exact match (full-line, including whitespace) within window.
//      b. Whitespace-normalized match (strings.TrimSpace per line) if (a) finds none.
//   5. Decide:
//      - Exactly one match → update HeadSHA, StartLine, EndLine; clear Stale; reanchored++.
//      - Zero matches → keep HeadSHA at oldHead, set Stale=true; stale++.
//      - Multiple matches → keep HeadSHA at oldHead, set Stale=true; stale++ (ambiguous).
//
// Out of scope for v1 (left for v2):
//   - Drift beyond ±20 lines.
//   - Comments on deleted-then-re-added files.
//   - Cross-file moves (file renamed; we report stale instead).
//   - Cross-scope migration (full-stack ↔ layer when the PR rebases onto a new base).
func reanchorComments(vcs VCS, repoRoot, oldHead, newHead string,
    cj *CritJSON) (reanchored, stale, missing int) {
    for path, cf := range cj.Files {
        oldContent, err := vcs.ReadFileAtSHA(oldHead, path, repoRoot)
        if err != nil || oldContent == nil {
            for i := range cf.Comments {
                if cf.Comments[i].HeadSHA == oldHead {
                    cf.Comments[i].Stale = true
                    missing++
                }
            }
            cj.Files[path] = cf
            continue
        }
        newContent, err := vcs.ReadFileAtSHA(newHead, path, repoRoot)
        if err != nil || newContent == nil {
            for i := range cf.Comments {
                if cf.Comments[i].HeadSHA == oldHead {
                    cf.Comments[i].Stale = true
                    missing++
                }
            }
            cj.Files[path] = cf
            continue
        }
        oldLines := strings.Split(string(oldContent), "\n")
        newLines := strings.Split(string(newContent), "\n")
        for i := range cf.Comments {
            c := &cf.Comments[i]
            if c.HeadSHA != oldHead { continue }
            newStart, newEnd, ok := findReanchorPosition(oldLines, newLines,
                c.StartLine, c.EndLine, 20)
            if !ok {
                c.Stale = true
                stale++
                continue
            }
            c.HeadSHA = newHead
            c.StartLine = newStart
            c.EndLine = newEnd
            c.Stale = false
            reanchored++
        }
        cj.Files[path] = cf
    }
    return
}

// findReanchorPosition returns the new (start, end) line range for a comment
// that originally spanned oldStart..oldEnd, by searching newLines within
// ±window of oldStart for an exact-then-fuzzy match. ok=false on zero or
// multiple matches.
func findReanchorPosition(oldLines, newLines []string,
    oldStart, oldEnd, window int) (int, int, bool) {
    // Lines are 1-indexed in Comment; convert.
    if oldStart < 1 || oldEnd < oldStart { return 0, 0, false }
    if oldEnd > len(oldLines) { return 0, 0, false }
    target := oldLines[oldStart-1 : oldEnd] // slice of original lines
    span := len(target)

    // Search range in newLines (1-indexed): [oldStart-window, oldStart+window]
    lo := oldStart - window
    if lo < 1 { lo = 1 }
    hi := oldStart + window
    if hi+span-1 > len(newLines) { hi = len(newLines) - span + 1 }

    matches := scanForMatches(newLines, target, lo, hi, false /* exact */)
    if len(matches) == 0 {
        matches = scanForMatches(newLines, target, lo, hi, true /* whitespace-normalized */)
    }
    if len(matches) != 1 {
        return 0, 0, false
    }
    start := matches[0]
    return start, start + span - 1, true
}

func scanForMatches(haystack, needle []string, lo, hi int, normalize bool) []int {
    var matches []int
    norm := func(s string) string {
        if normalize { return strings.TrimSpace(s) }
        return s
    }
    for i := lo; i <= hi; i++ {
        all := true
        for j, want := range needle {
            if norm(haystack[i-1+j]) != norm(want) { all = false; break }
        }
        if all { matches = append(matches, i) }
    }
    return matches
}
```

**Trigger points.**

1. **Inside `Session.SetFocus`** (§B): when `oldFocus.Kind == FocusRange &&
   newFocus.Kind == FocusRange && oldFocus.HeadSHA != newFocus.HeadSHA`,
   call `reanchorComments(vcs, repoRoot, oldFocus.HeadSHA, newFocus.HeadSHA,
   &cj)` after the file-list rebuild but before `saveCritJSON`.
2. **Inside the `crit --pr <num>` boot path** (in `createSession` /
   `applySessionOverrides`): when a daemon-spawned session loads a review
   file whose comments have `HeadSHA != prInfo.HeadRefOid`, run reanchor
   once on startup. Use `loadCritJSON` then `saveCritJSON`. Don't pass
   through `Session.SetFocus` (the session isn't fully constructed yet).

**UI surfacing.** After reanchor, emit one line on the daemon log AND a
one-shot SSE event the frontend renders as a banner:

```
SSEEvent{Type: "reanchor", Content: `{"reanchored":3,"stale":1,"missing":0,"old":"abc1234","new":"def5678"}`}
```

The frontend dismisses the banner on click; no persistent state needed.

**Edge cases.**

- File deleted between old and new head: reanchor returns `missing` for
  every comment in that file. Comments stay with their old `HeadSHA`,
  `Stale: true`, and the file's `Status` is reported as deleted in the new
  view.
- File renamed: same as deleted (we don't track renames cross-SHA in v1).
  Comments stay stale; user re-authors.
- `HeadSHA` empty (legacy/working-tree comment): not eligible — the
  heuristic only operates on comments where `c.HeadSHA == oldHead`.

### `crit push --pr <num>` interaction

`crit push` already collects unresolved comments and posts them via
`createGHReview` (`github.go:691-745`). Four gates apply, in order:

1. **Full-stack scope gate (§B).** If `cj.ActiveDiffScope == "full_stack"`,
   abort with `"Switch to Layer diff before posting a platform review"`.
   Done before any network call.
2. **Scope filter on payload.** When push *is* allowed, only comments
   intended for this PR's coordinates are sent:
   - In range/PR mode (`cj.ActiveDiffScope != ""`): include only comments
     with `DiffScope == "layer"`. Full-stack comments stay local.
   - In working-tree mode (`cj.ActiveDiffScope == ""`, the legacy path):
     include comments with empty `DiffScope` (today's behavior, unchanged).
3. **Empty-HeadSHA gate in range mode.** When `cj.ActiveDiffScope != ""`,
   exclude comments with `HeadSHA == ""`. These were authored before the
   focus was adopted (e.g. via `crit comment` CLI without daemon context)
   and have no anchor to a specific PR head — pushing them would land them
   at line numbers that don't match the PR's diff. If the exclusion drops
   any unresolved comment, abort the push with a clear message:
   `"<N> comments have no PR anchor and would be skipped. Re-author them in PR mode (open the picker) or run `crit push` from working-tree mode."`
4. **HeadSHA mismatch.** If any included comment has `HeadSHA` non-empty and
   different from the current PR head SHA from `gh pr view`, abort with:
   `"review was authored against <old SHA>, current PR head is <new SHA>; re-fetch with crit --pr <num> and re-review"`.
   Don't silently push — GitHub will reject most positions and the rest will
   land in the wrong place.

A `--force-anchor` flag is a v2 escape hatch; the v1 message stops at
"re-fetch".

---

## F. Picker / UI

### Backend endpoint(s)

Two distinct surfaces:

1. **Focus picker** (PR/range switcher) — `GET /api/picker`.
2. **Diff scope toggle** (layer/full-stack) — already covered by `GET
   /api/session` exposing `focus`. The frontend renders the toggle from
   `focus` alone; switching scope is `POST /api/focus` with a modified
   `Focus` body.

```
GET /api/picker
Response:
{
  "current":   Focus,                  // full Focus shape (see §B)
  "stack":     [StackEntry, ...],      // local stack: HEAD's ancestors that are PR heads or branch tips
  "other_prs": [PRSummary, ...],       // open PRs whose head SHA isn't in `stack`
  "branches":  [BranchEntry, ...],     // recent remote branches not already covered above
  "errors":    []                      // gh-unavailable etc., not 5xx
}

type StackEntry struct {
    Label       string `json:"label"`         // "PR #295: feat-b" or "feat-b" (branch only)
    PRNumber    int    `json:"pr_number,omitempty"`
    HeadSHA     string `json:"head_sha"`
    BaseSHA     string `json:"base_sha,omitempty"`
    BaseRefName string `json:"base_ref_name,omitempty"`
    Current     bool   `json:"current"`       // tip of HEAD
}

type BranchEntry struct {
    Name    string `json:"name"`     // "origin/feat-x"
    HeadSHA string `json:"head_sha"`
}
```

**Branches section** is populated by `remoteBranchTips` (see §F "Stack
detection algorithm"). Selection treats it as a synthetic Range focus:
`BaseSHA = merge-base(branch, default)`, `HeadSHA = branch tip`. Sorting:
recency (`committerdate` for git, `date` for sapling), capped at 20.

`/api/picker` filters out from `branches` any SHAs already in `stack` or
`other_prs`, so each commit appears in exactly one section.

**Diff-scope toggle data** comes from the existing `Focus` payload:
- `focus.kind == "range"` AND `focus.is_stacked` → render the toggle.
- `focus.diff_scope` → which radio is selected.
- `focus.default_sha != ""` → full-stack option enabled; otherwise disabled
  with tooltip "Requires resolvable default branch tip".

To switch scope: `POST /api/focus` with the current focus payload but
`diff_scope` flipped. Server validates that full-stack ↔ DefaultSHA constraint
holds and emits SSE `focus-changed`.

```go
// server.go: handleFocus rejects full-stack without DefaultSHA.
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
    // ... decode req ...
    if req.Kind == FocusRange &&
        req.DiffScope == DiffScopeFullStack &&
        req.DefaultSHA == "" {
        http.Error(w, "full-stack scope requires a resolvable default branch tip", http.StatusBadRequest)
        return
    }
    // SetFocus rebuilds Files using ChangedFilesBetweenSHAs(req.DiffBaseSHA(),
    // req.HeadSHA, repoRoot) and emits focus-changed.
    if err := s.session.Load().SetFocus(req); err != nil { ... }
    writeJSON(w, map[string]string{"status": "ok"})
}
```

Single endpoint to keep the frontend simple. `/api/picker` returns whatever
it can: gh-unavailable → empty `other_prs` plus an error string. Stack
detection runs even without gh.

### Stack detection algorithm (Go)

```go
// detectStack walks back from HEAD up to depth N, collects each commit SHA, then
// intersects with: (a) local branch tips, (b) open-PR head SHAs from gh.
// The "current" flag is set on whichever stack entry matches the current focus
// HEAD.
//
// Sapling: equivalent walks via `sl log -r 'ancestors(.) & draft()'`. Concrete
// implementation lives in vcs-specific helpers; the picker doesn't care.
func detectStack(vcs VCS, repoRoot string, openPRs []PRSummary) ([]StackEntry, error) {
    const maxDepth = 20

    headSHAs, err := walkAncestors(vcs, repoRoot, maxDepth)
    if err != nil {
        return nil, err
    }
    headSet := make(map[string]int, len(headSHAs))
    for i, sha := range headSHAs {
        headSet[sha] = i // smaller i = closer to HEAD
    }

    branchTips, _ := localBranchTips(vcs, repoRoot)
    prByHead := make(map[string]PRSummary, len(openPRs))
    for _, pr := range openPRs {
        prByHead[pr.HeadRefOid] = pr
    }

    var entries []StackEntry
    for sha, distance := range headSet {
        if pr, ok := prByHead[sha]; ok {
            entries = append(entries, StackEntry{
                Label:    fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title),
                PRNumber: pr.Number,
                HeadSHA:  sha,
                Distance: distance,
            })
            continue
        }
        if branch, ok := branchTips[sha]; ok {
            entries = append(entries, StackEntry{
                Label:    branch,
                HeadSHA:  sha,
                Distance: distance,
            })
        }
    }
    sort.Slice(entries, func(i, j int) bool { return entries[i].Distance < entries[j].Distance })
    // Compute base_sha for each entry as the previous entry's head_sha (or merge-base
    // with default branch for the bottom of the stack).
    return assignStackBases(vcs, entries, repoRoot)
}

// walkAncestors enumerates HEAD-first the recent ancestor SHAs that are
// candidates for stack stops. Returns SHAs HEAD-first; consumer maps each
// to its picker entry via localBranchTips / openPRs.
func walkAncestors(vcs VCS, repoRoot string, maxDepth int) ([]string, error) {
    if vcs.Name() == "git" {
        out, err := runGitInDir(repoRoot, "rev-list",
            "--first-parent", "-n", strconv.Itoa(maxDepth), "HEAD")
        if err != nil { return nil, err }
        return splitNonEmpty(out), nil
    }
    // Sapling: enumerate ancestors of `.` that are still draft (i.e. not
    // landed on any public branch). draft() is the standard sapling stack
    // model — any commit between the nearest public ancestor and `.`.
    out, err := slCommandInDir(repoRoot, "log", "-r",
        fmt.Sprintf("ancestors(., %d) & draft()", maxDepth),
        "-T", "{node}\n")
    if err != nil { return nil, err }
    return splitNonEmpty(out), nil
}

// localBranchTips returns commit SHAs that are useful as stack labels in
// the picker, mapped to a human-readable name.
//
// For git: refs/heads/ entries (refname:short).
// For sapling: bookmarks (when present) plus draft commits (whose first-line
// description acts as a label). Sapling has no equivalent of git branches —
// stacks are typically named via bookmarks or first-line commit messages.
func localBranchTips(vcs VCS, repoRoot string) (map[string]string, error) {
    if vcs.Name() == "git" {
        out, err := runGitInDir(repoRoot, "for-each-ref",
            "--format=%(objectname) %(refname:short)", "refs/heads/")
        if err != nil { return nil, err }
        result := make(map[string]string)
        for _, line := range splitNonEmpty(out) {
            parts := strings.SplitN(line, " ", 2)
            if len(parts) == 2 { result[parts[0]] = parts[1] }
        }
        return result, nil
    }
    // Sapling: combine bookmarks + draft-commit first-line descriptions.
    result := make(map[string]string)
    if bookmarks, err := slCommandInDir(repoRoot, "bookmarks",
        "-T", "{node} {bookmark}\n"); err == nil {
        for _, line := range splitNonEmpty(bookmarks) {
            parts := strings.SplitN(line, " ", 2)
            if len(parts) == 2 { result[parts[0]] = parts[1] }
        }
    }
    // Draft commits whose first-line desc acts as the label. Only fill in
    // SHAs that don't already have a bookmark name (bookmarks are nicer).
    if drafts, err := slCommandInDir(repoRoot, "log", "-r", "draft()",
        "-T", "{node} {desc|firstline}\n"); err == nil {
        for _, line := range splitNonEmpty(drafts) {
            parts := strings.SplitN(line, " ", 2)
            if len(parts) == 2 {
                if _, ok := result[parts[0]]; !ok {
                    result[parts[0]] = parts[1]
                }
            }
        }
    }
    return result, nil
}
```

**Picker section: remote branches (third bucket).** Beyond the stack and
"Other PRs", the picker shows recently-active remote branches whose tips
aren't already covered. This catches local branches that don't have an open
PR yet (e.g. someone pushed `feat/x` but hasn't opened a PR).

```go
type BranchEntry struct {
    Name    string `json:"name"`     // "origin/feat-x"
    HeadSHA string `json:"head_sha"`
}

// remoteBranchTips returns up to 20 remote branches sorted by recency,
// excluding the default branch. Caller subtracts the SHAs already covered
// by the stack and other_prs sections.
//
// For git: `git for-each-ref --sort=-committerdate refs/remotes/`
// For sapling: `sl log -r 'remote() & ::tip & date(\"-30d\")'`
//   (sapling's "remote" predicate matches commits with a remote bookmark).
func remoteBranchTips(vcs VCS, repoRoot, defaultBranch string) ([]BranchEntry, error) {
    if vcs.Name() == "git" {
        out, err := runGitInDir(repoRoot, "for-each-ref",
            "--sort=-committerdate", "--count=40",
            "--format=%(objectname) %(refname:short)",
            "refs/remotes/")
        if err != nil { return nil, err }
        var entries []BranchEntry
        for _, line := range splitNonEmpty(out) {
            parts := strings.SplitN(line, " ", 2)
            if len(parts) != 2 { continue }
            name := parts[1]
            // Drop "origin/HEAD", default branch, and "<remote>/HEAD" pointers.
            if strings.HasSuffix(name, "/HEAD") { continue }
            if name == "origin/"+defaultBranch || name == defaultBranch { continue }
            entries = append(entries, BranchEntry{Name: name, HeadSHA: parts[0]})
            if len(entries) >= 20 { break }
        }
        return entries, nil
    }
    // Sapling: enumerate remote bookmarks pointing at recent commits.
    out, err := slCommandInDir(repoRoot, "log", "-r",
        "sort(remote(), -date)", "--limit", "40",
        "-T", "{node} {remotebookmarks}\n")
    if err != nil { return nil, nil } // best-effort
    var entries []BranchEntry
    for _, line := range splitNonEmpty(out) {
        parts := strings.SplitN(line, " ", 2)
        if len(parts) != 2 || parts[1] == "" { continue }
        // First bookmark is enough as the label.
        bookmark := strings.SplitN(parts[1], " ", 2)[0]
        if bookmark == defaultBranch || strings.HasSuffix(bookmark, "/"+defaultBranch) {
            continue
        }
        entries = append(entries, BranchEntry{Name: bookmark, HeadSHA: parts[0]})
        if len(entries) >= 20 { break }
    }
    return entries, nil
}
```

The picker handler in §F "Backend endpoint(s)" subtracts SHAs already in
`stack` or `other_prs` before returning `branches`, so each commit appears
in exactly one section.

Push these into `git.go` and `sapling.go` as plain helper funcs (not on the
`VCS` interface — they're picker-specific and we don't want to bloat the
interface). `IsStackedPR` and `ResolveDefaultBranchSHA` (defined in §C) live
in `github.go` and `git.go` respectively, since they are reused by both the
CLI focus builder and the picker.

### Frontend integration points (don't write the JS, just identify them)

Two new UI elements:

**1. Focus picker** — switch the whole session between working tree, PRs in
the local stack, and other open PRs.

`frontend/index.html` — add a `<button id="focus-picker-btn" class="header-btn">`
in the existing header next to the share/theme controls. The header element is
queried at `frontend/app.js:1312`.

`frontend/app.js`:
- Render the picker as a popover similar to the existing commit picker
  (`frontend/app.js:7023-7060`). The commit picker uses `.commit-picker-item`
  classes; mirror with `.focus-picker-item`.
- On open, `fetch('/api/picker')` and render two sections: "Your stack" and
  "Other PRs".
- On select, `POST /api/focus` with the chosen entry; the existing SSE listener
  picks up the `focus-changed` event and reloads the file list (reuse the same
  refresh path that `base-changed` triggers).

**2. Diff scope toggle (layer / full-stack)** — only when in a stacked range
focus. This is the prior art parity feature.

`frontend/app.js`:
- Render two radio rows in a popover anchored to the diff-area header (NOT the
  global header — keep it scoped to the diff being viewed). Labels: "Layer
  (only changes in this PR)" and "Full-stack (all changes from `<base_ref_name
  of default branch>`)".
- Visibility rule: `session.focus.kind === 'range' && session.focus.is_stacked`.
  If false, do not render the popover trigger at all.
- Full-stack disabled when `!session.focus.default_sha`. Tooltip: "Requires
  local checkout".
- Selected radio comes from `session.focus.diff_scope`.
- On change, `POST /api/focus` with `{...session.focus, diff_scope: <new>}`.
  SSE `focus-changed` triggers reload.
- **Don't** show a banner about platform push being disabled in full-stack —
  per prior art's UX, the push button just rejects with 409 server-side and
  the toast surfaces the message. Less UI clutter; matches the source.

The two pickers are independent. Switching focus changes the whole PR/range
being viewed; the scope toggle just changes the diff base within the current
range.

### Read-only / scope enforcement reminder

There is **no** session-level read-only mode (§B correction). Local
annotations work in any focus/scope. The only server-side gate is the
**push-to-platform** path, which rejects full-stack with 409. Frontend may
show a non-blocking hint in the push button's title attr when scope is
full-stack, but doesn't need to disable the button — the server is the source
of truth.

---

## G. Test strategy

### Unit (Go)

Extend or create:

1. **`git_vcs_test.go`** — already has `runGit` test helper. Add table-driven
   tests for:
   - `ChangedFilesBetweenSHAs` — one commit, two commits, rename across the
     range, deletion, untracked-not-included.
   - `FileDiffBetweenSHAs` — happy path, identical SHAs (empty), missing path,
     binary file (empty hunks).
   - `ReadFileAtSHA` — happy path, missing path returns nil/nil, missing SHA
     returns error.
   - `HasObject` — true/false.

2. **`sapling_test.go`** — same matrix, gated by `sl` being on PATH (the
   existing tests use this guard, see `sapling_test.go`).

3. **`github_test.go`** — `parsePRSpec`, `parseRangeSpec`, and `prURLRe` are
   pure-Go and get straightforward table tests with no `gh` dependency. For
   `fetchPRByNumber` / `fetchOpenPRs` the codebase doesn't currently mock `gh`
   (one incidental match in the file). Adopt the cheap pattern: factor the JSON
   parse out of the network call, test the parser against fixture JSON. Skip
   end-to-end `gh` tests in CI; gate behind a build tag like share-integration.

4. **`session_test.go`** — new tests:
   - `Focus.ReadOnly()` is always false (regression guard).
   - `Focus.DiffBaseSHA()` returns BaseSHA in layer scope and DefaultSHA in
     full-stack scope; falls back to BaseSHA when DefaultSHA is empty.
   - `Focus.PickerVisible()` true only when range + stacked.
   - `Focus.FullStackAvailable()` truth table.
   - `Session.SetFocus(FocusRange{... DiffScope: layer ...})` builds Files
     from `ChangedFilesBetweenSHAs(BaseSHA, HeadSHA)` and reads content via
     `ReadFileAtSHA(HeadSHA, ...)`.
   - Same in full-stack scope: Files come from `(DefaultSHA, HeadSHA)`.
   - Toggling scope preserves all comments on disk; only filter changes.
   - Comment write path stamps both `HeadSHA` and `DiffScope` from focus.
   - `visibleInFocus` truth table:
     - Working-tree focus + empty DiffScope → visible.
     - Working-tree focus + "layer" DiffScope → hidden.
     - Range focus + scope "layer" + comment "layer" → visible.
     - Range focus + scope "layer" + comment "full_stack" → hidden.
     - Range focus + scope "layer" + empty DiffScope (legacy) → hidden.
   - Stale flag computation.
   - Pre-feature comments (no DiffScope, no HeadSHA) appear in working-tree
     focus and ARE NOT visible in any range focus, regardless of scope.

5. **`server_test.go`** — there's already a `TestSetPRInfo_*` pair
   (`server_test.go:2124, 2169`); add:
   - `TestHandleFocus_SwitchToRange` — POST `/api/focus`, then read `s.Focus`
     under `s.mu.RLock` and assert the new value (don't reach into private
     fields without the lock).
   - `TestHandleFocus_SwitchScope_Layer_To_FullStack` — POST with new
     diff_scope, verify file list rebuilds, SSE `focus-changed` fires.
   - `TestHandleFocus_FullStackRejectedWithoutDefaultSHA` — 400.
   - `TestSetFocus_RollbackOnRebuildFailure` — inject a VCS that errors from
     `ChangedFilesBetweenSHAs`. Assert `s.Focus`, `s.Files`, `s.BaseRef` are
     unchanged after the failed `SetFocus` call. Assert no SSE event was
     emitted (subscribe + select with timeout).
   - `TestSetFocus_RollbackOnEnsureSHAFetchedFailure` — make
     `ensureSHAFetched` return an error (SHA missing locally and no remote
     can satisfy it). Assert no state mutated.
   - `TestSessionInfo_IncludesFocus` — `/api/session` exposes focus + the
     fields the frontend toggle needs.
   - `TestFileComments_FilteredByDiffScope` — POST a comment in layer scope
     via the real `AddComment(filePath, startLine, endLine, side, body,
     quote, author, userID)` path; switch to full-stack via `/api/focus`;
     GET `/api/file/comments` returns empty for that file; switch back,
     comment reappears. Use `s.GetComments(path)` to verify in-memory state
     under lock; use `s.FindCommentByID(id, "")` for cross-file lookups.
   - `TestHandleRoundComplete_RejectedInRange` — Focus = FocusRange,
     POST `/api/round-complete`, assert HTTP 409 + JSON error body.
     (Per CLAUDE.md "Daemon/Server" — validate response body, not just
     status.)
   - `TestHandlePicker_StackOnly` (no gh path, fakeable via `prListCache`
     pre-population).

   Push gate (CLI-side, not HTTP):
   - `TestRunPush_RejectedInFullStackScope` — `cj.ActiveDiffScope =
     "full_stack"`, run push, expect exit 1 with the gate message and no
     `gh` invocation.
   - `TestRunPush_FullStackCommentsExcluded` — review file mixes layer and
     full-stack comments; in layer scope only the layer ones are sent to
     GitHub.
   - `TestRunPush_EmptyHeadSHACommentsAborted` — review file in range mode
     (`ActiveDiffScope="layer"`) with one comment lacking `HeadSHA`. Push
     aborts with the gate-3 message; no comments sent.
   - `TestRunPush_StaleHeadSHARejected` — comments have `HeadSHA="abc"` but
     current PR head is `"def"`. Push aborts with gate-4 message.

6. **`main_test.go`** — extend the existing arg-parsing tests with cases for
   `--pr 295`, `--pr https://...`, `--range a..b`, mutually-exclusive error,
   `crit pr 295` subcommand routing, `--scope=layer`, `--scope=full-stack`,
   `--scope=invalid` (error), `--scope` with `--range` (warning, not error).

7. **`session_test.go` carry-forward** — `TestCarryForwardComment_PreservesScope`:
   build an old `Comment{HeadSHA: "abc", DiffScope: "layer", ...}`, run
   `carryForwardComment(old, "new-id", now)`, assert both fields copied.
   Failing this test catches the C-4 silent-strip regression.

8. **Auto-fetch failures (T-3)** — `TestEnsureSHAFetched_StillMissingAfterFetch`:
   stub a `VCS` whose `HasObject` always returns false; assert error
   contains "manual fetch required" and the SHA. Pair with
   `TestEnsureSHAFetched_ForkFallback`: `HasObject` returns false on first
   call, true on third (after fork fetch); assert success and that the
   second `tryGitFetch` was called with the fork URL. (See T-2 for the
   testing pattern: factor `tryGitFetch` to be replaceable in tests.)
   Add `TestEnsureSHAFetched_Sapling_PullThenGitFetch`: VCS reports
   `Name() == "sapling"`, `HasObject` returns false twice then true after
   the git-fetch fallback. Use `t.Setenv("PATH", fakeBinDir)` to make `sl`
   and `git` resolve to scripted shims that record their args.

9. **Re-anchor heuristic** — `findReanchorPosition` is pure and
   table-testable. Cases (in `session_test.go`):
   - Identical content at the same line → reanchored to the same line.
   - Identical content shifted +3 lines → reanchored to old+3.
   - Identical content shifted -5 lines → reanchored to old-5.
   - Content removed → 0 matches, ok=false (caller marks Stale).
   - Content duplicated → 2 matches, ok=false (caller marks Stale).
   - Whitespace-only difference → falls through to normalized match,
     ok=true.
   - Drift beyond ±20 lines → ok=false.
   - Multi-line range (StartLine != EndLine), all lines must match.

   And `TestReanchorComments_Integration` against a real temp git repo:
   - Build commits A and B where line 10's content is unchanged →
     comment authored at A line 10 reanchors to B line 10, Stale=false.
   - Build commits A and B where line 10's content moves to line 13 →
     reanchors to line 13.
   - Build commits A and B where line 10's content is removed → marked
     Stale, HeadSHA stays at A.
   - File deleted between A and B → marked Stale, missing++.

   Plus `TestSetFocus_ReanchorOnHeadShift` (server_test.go): SetFocus
   from FocusRange{HeadSHA: A} to FocusRange{HeadSHA: B} on the same PR,
   assert the SSE `reanchor` event carries the right counts and that
   reanchored comments have the new line numbers.

10. **`crit comment` scope inheritance** (`main_test.go` or new
    `comment_scope_test.go`):
    - `TestResolveCommentScope_NoOverride_NoDaemon_NoActiveScope` →
      empty (working-tree).
    - `TestResolveCommentScope_NoOverride_DaemonInRange` → inherits
      HeadSHA + DiffScope from daemon. Use `httptest.NewServer` to serve
      `/api/session` with a synthetic Focus.
    - `TestResolveCommentScope_NoOverride_NoDaemon_DiskScopeSet` → uses
      `cj.ActiveDiffScope`, leaves HeadSHA empty, prints warning to
      stderr (capture and assert).
    - `TestResolveCommentScope_OverrideWorkingTree` → empty even if
      daemon is in range.
    - `TestResolveCommentScope_OverrideFullStack_NoActive` → error
      "no active full-stack focus to attach to".
    - `TestResolveCommentScope_OverrideLayer_DaemonInFullStack_Conflict`
      → error (override layer doesn't match daemon's full_stack).
    - End-to-end `TestRunComment_StampsScopeFromDaemon`: spawn fake
      daemon, run `runComment` writing to a temp review file, assert the
      written `Comment.HeadSHA` and `Comment.DiffScope` match the
      daemon's focus.

11. **Sapling stack detection** (gated by `sl` on PATH; mirror existing
    sapling test guards in `sapling_test.go`):
    - `TestWalkAncestors_Sapling`: build a small sapling repo with a
      draft chain of 3 commits; assert `walkAncestors` returns the 3
      SHAs HEAD-first.
    - `TestLocalBranchTips_Sapling_Bookmarks`: create a bookmark on a
      draft commit, assert it appears in the result map.
    - `TestLocalBranchTips_Sapling_DraftFallback`: no bookmarks, assert
      draft commits are labeled by their first-line description.
    - `TestRemoteBranchTips_Sapling`: remote bookmark exists, returns
      one entry with the bookmark name.

12. **Picker branches section** (`server_test.go`):
    - `TestHandlePicker_BranchesExcludesStackAndPRs`: pre-populate
      `prListCache` with two PRs; create local branches such that one
      branch tip equals a PR head (should be filtered) and another is
      standalone (should appear in `branches`).
    - `TestHandlePicker_BranchesExcludesDefault`: assert `origin/main`
      (or whatever default is) never appears.

### E2E

Per CLAUDE.md, projects are named by mode and gated by spec filename suffix.
Add **one** new Playwright project named `range-mode` (port 3128), fixture
`setup-fixtures-range-mode.sh`:

1. Creates a git repo with a stacked structure: `main → A (PR #1) → B (PR #2)
   → C (PR #3)` — three commits on a feature branch.
2. Boots crit with `--range <SHA-of-A>..<SHA-of-B>` (no real GitHub round-trip;
   `--range` doesn't need `gh`).

Specs (suffix `.rangemode.spec.ts`, mirroring CLAUDE.md naming):

- `range-loading.rangemode.spec.ts` — UI shows only files changed in B-not-A;
  header label reads `<short(A)>..<short(B)>`.
- `range-comments.rangemode.spec.ts` — comment forms are NOT hidden in range
  mode; POSTing a comment succeeds and persists the `head_sha` and
  `diff_scope: "layer"` (verified by reading `~/.crit/reviews/<key>.json`).
- `scope-toggle.rangemode.spec.ts` — note: the existing
  `scope-toggle.spec.ts` covers the unrelated all/branch/staged/unstaged
  toggle. This new file tests the **layer/full-stack** toggle. Stack the
  fixture so `B`'s base is `A` (not `main`), making it stacked. Assert:
  - Toggle is visible (`is_stacked === true`).
  - Switching to full-stack rebuilds file list to include both A and B's
    changes.
  - Comments persist across toggles (layer comment hidden in full-stack;
    full-stack comment hidden in layer).
- `focus-switch.rangemode.spec.ts` — open the picker, switch back to working
  tree, file list refreshes.

For `crit --pr <num>`, real `gh` is required → cover via Go-level integration
tests under the existing `integration` build tag (see
`share_integration_test.go` for the pattern). Do not add Playwright tests
that hit GitHub.

**Interactive multi-instance reviewer (`make test-diff`).** `test/test-diff.sh`
boots multiple crit instances side-by-side for human inspection. Per the plan
(Task 9 step 7b), extend it with two new instances:

1. **Range mode** (`crit --range A..B`) on a stacked git fixture — verifies
   on-disk `head_sha` + `diff_scope: "layer"` stamping and the focus-picker
   round-trip (range → working tree → range preserves comments).
2. **Stacked toggle** — synthesizes stacked metadata via `POST /api/focus`
   with `is_stacked: true`, exercises the layer ↔ full-stack toggle, and
   asserts at the CLI that `crit push --pr 999 --dry-run` refuses with the
   gate-1 message when `ActiveDiffScope == "full_stack"`.

This complements (does not replace) Playwright. Playwright covers automated
assertions; `test/test-diff.sh` covers eyeballed UX a human can't easily
script (focus switching mid-session, stamping, push-gate refusal at the CLI).

### Mocking strategy

Crit currently does **not** mock `gh` in tests; assertions on PR features
either skip if `gh` isn't authed or don't exist. Don't try to retrofit a
heavy mock — the practical pattern is:

- Pure logic (URL parsing, range parsing, focus building, stale-flag
  computation, `visibleInFocus`, `annotateStaleness`, `IsStackedPR`) →
  unit-tested directly.
- gh-touching functions → factor a parser entry point that operates on
  bytes, so tests can feed fixture JSON without invoking `gh`. The spec
  factors `parsePRViewJSON([]byte) (*PRInfo, error)` (see §D
  `fetchPRByNumber`) and `parsePRListJSON` for this purpose. Apply the
  same pattern to any future helper that runs `gh ...` and parses output.
- Anything that genuinely needs `gh` end-to-end → behind a build tag, run
  locally before merging.

For `git`-touching helpers (`ResolveDefaultBranchSHA`, `ensureSHAFetched`,
`tryGitFetch`), the existing test pattern (`testutil_test.go` `runGit`
helper that operates on a temp directory) is sufficient — no `git` mocking
needed. For `tryGitFetch` failure paths, swap in a fake remote URL that
points at a non-existent path.

This is consistent with crit's existing testing philosophy.

---

## H. Risks / open questions

1. **Range mode and the daemon registry.** Today's `sessionKey` is
   `cwd+branch` for git mode. Two PRs against the same branch would currently
   collide; we fix that by including `--pr N` / `--range A..B` in keying
   (§C). Verify: starting `crit --pr 294` then `crit --pr 295` from the same
   cwd produces two daemons, not one.

2. **Comment storage format compatibility.** Adding `HeadSHA` to `Comment` is
   forward-compatible (omitempty), but **not backward-compatible** if a user
   downgrades crit after authoring range-mode comments — the field is silently
   dropped on round-trip through an older binary. Acceptable v1 risk; document
   in release notes.

3. **`crit push --pr <num>` against a stale head — best-effort re-anchor in v1.**
   When the daemon detects that the focus's `HeadSHA` has shifted (force-push
   or new commits), it runs the re-anchor heuristic in §E "Re-anchoring after
   head SHA change":

   - Existing comments stay in `~/.crit/reviews/<key>.json`. The PR-keyed
     daemon (`pr:N`) reuses the same review file across head changes.
     Comments are NOT auto-deleted.
   - For each comment with `HeadSHA == oldHead`, the heuristic searches for
     the original line content within ±20 lines of its old position in the
     new head. If found exactly once, the comment's `(HeadSHA, StartLine,
     EndLine)` is updated and `Stale` is cleared. If zero or multiple
     matches, `Stale` stays true.
   - Re-anchored comments are pushable. Stale-flagged comments still abort
     `crit push` (gate 4 in §E) until the user re-authors them.
   - The daemon log and a one-line UI banner summarize the result:
     `"Re-anchored 3 comments / 1 stale / 0 missing after head moved abc1234..def5678"`.
   - **v2 covers cross-scope migration** (move full-stack comments to layer
     after a PR is rebased), `--force-anchor` push override, and ML-style
     fuzzy match for comments that drifted past ±20 lines.

4. **Sapling parity.** `--pr` requires `gh`. Sapling-on-git (the common
   stacked-PR setup) works because we resolve SHAs via `gh` and then read
   files via `sl cat -r`. Auto-fetch from gh PRs uses `sl pull -r <sha>`
   first and falls back to `git fetch origin <sha>` when `.git` is present
   alongside `.sl` (§A). Sapling-without-git is untested; flag in release
   notes.

   **Stack detection works on sapling in v1** (§F "Stack detection
   algorithm"): `walkAncestors` uses `sl log -r 'ancestors(., 20) & draft()'`
   and `localBranchTips` enumerates draft commits via
   `sl log -r 'draft()' -T '{node} {desc|firstline}\n'` (with bookmarks
   layered on top via `sl bookmarks -T '{node} {bookmark}\n'` when
   present).

5. **Picker performance on large orgs.** `gh pr list` on big orgs takes 3-5s.
   The 60s in-memory cache helps, but the **first** open is slow. Consider
   kicking off the fetch in a goroutine when the daemon starts so the cache is
   warm by the time the user clicks.

6. **`--range` rename detection edge cases.** When a file is renamed
   `old.go → new.go` between base and head, we get a single `R100\told\tnew`
   line. `parseNameStatus` already turns this into `{Path: "new.go", Status:
   "renamed"}` (`git.go:583-587`). But `ReadFileAtSHA(baseSHA, "new.go")` will
   return nil/nil because the file didn't exist under that name yet. The diff
   renderer needs to know to read the *old* path at base. v1: don't render a
   rename diff specially — show it as add+delete. Document and accept.

7. **`crit comment` CLI and scope tagging.** Solved in v1 — see §C
   "`crit comment` scope inheritance" for the full design. Summary:
   `crit comment` probes any running daemon for its `Focus`, stamps
   `HeadSHA` and `DiffScope` accordingly, and accepts an explicit
   `--scope=layer|full-stack|working-tree` override.

8. **`crit --range` and key isolation.** Range sessions key by
   `pr:<n>` / `range:<base>..<head>` (§C), distinct from the working-tree key
   (`cwd+branch`). Comments authored in working-tree mode and comments
   authored in range mode therefore live in **different review files**. This
   is intentional — they correspond to different points in time and different
   line numbering — but verify in the test plan that switching between them
   doesn't produce surprising data loss.

9. **Scope toggle within a single session is lossless.** When the user
   toggles layer ↔ full-stack via the picker, both scopes' comments stay on
   disk; only the filter changes. Verify with a regression test (§G).

10. **Comment scope migration / pre-feature comments.** Existing comments
    have neither `HeadSHA` nor `DiffScope`. They render in **working-tree**
    focus only (per the `visibleInFocus` rule). Rationale: they were authored
    against the working tree's line numbers, not against any specific PR
    head. Promoting them into a range view would conflate contexts. Test
    coverage: a fixture review file with a pre-feature comment, opened in
    range mode, must show the comment count as zero for files that have it.
    No data is deleted — switching back to working tree shows the comment.

    The flip side (N-6): in range mode, **newly imported** comments from
    `crit pull` and `crit-web` MUST be stamped with the active scope at
    write time, not left empty — otherwise they'd be invisible in the very
    view that imported them. See §E "Write path" for the `crit pull` /
    `mergeWebComments` stamping rules. The migration rule (legacy comments
    are working-tree only) and the import rule (new imports inherit the
    active scope) are not in conflict — they apply at different points in
    time.

11. **`IsStacked` heuristic limitation.** A PR whose base IS the default
    branch but is part of a longer logical stack tracked in another tool
    (e.g. Graphite, ghstack) will report `IsStacked == false` and the picker
    will hide the layer/full-stack toggle for it. We accept this limitation
    in v1 — prior art has the same one. If a user needs full-stack for
    such a PR, `crit --range <default>..<head>` is the explicit escape
    hatch.

12. **One big feature, many commits.** Suggested commit boundaries for the
    planner. Note `Focus.DiffScope` is in the type system from C2 — before
    the CLI exposes it — so the wire shape is stable when later commits
    arrive.

    - **C1** `vcs: add ChangedFilesBetweenSHAs / ReadFileAtSHA / HasObject (+ tests)`
    - **C2** `session: add Focus tagged union with DiffScope + ForkURL baked in (working-tree default)`
    - **C3** `comments: add Comment.HeadSHA, Comment.DiffScope, CritJSON.ActiveDiffScope; visibleInFocus + annotateStaleness; stampWithFocus helper applied at every authoring site (incl. carryForwardComment)`
    - **C4** `cli: parse --pr / --range / --scope, build Focus via resolveFocus, key the daemon`
    - **C4b** `crit comment: --scope flag + daemon probe, inherits HeadSHA + DiffScope from running daemon`
    - **C5** `github: extend PRInfo (BaseRefOid/HeadRefOid/HeadRepoURL/IsCrossRepository), add fetchPRByNumber + fetchOpenPRs + ensureSHAFetched (with fork-fallback + sapling pull/git-fallback) + parsePRViewJSON/parsePRListJSON factor-outs`
    - **C6** `server: /api/focus with rollback + reanchorComments heuristic, /api/picker, push-gate in runPush, round-complete gate in handleRoundComplete; SSE focus-changed + reanchor`
    - **C7** `picker backend: stack detection (git + sapling), remoteBranchTips, prListCache, IsStackedPR/ResolveDefaultBranchSHA`
    - **C8** `frontend: focus picker popover (4 sections incl. branches), diff-scope toggle, focus-changed/reanchor SSE handlers`
    - **C9** `tests: e2e range-mode fixture, .rangemode.spec.ts files, integration-tagged gh tests, sapling stack tests, re-anchor heuristic tests, carry-forward regression test`

    Each is small, self-contained, and reviewable. Ship behind no flag — the
    feature is gated by the user passing `--pr` / `--range` / opening the
    picker.
