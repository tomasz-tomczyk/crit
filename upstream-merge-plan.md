# Upstream Merge Plan

## Context

We need to merge ~80 upstream commits from `tomasz-tomczyk/crit` into our fork. We have two custom changes that need to survive the merge:

1. **PR #47 (skill reuse-instance)** — Obsolete. Upstream already implemented instance reuse and restructured all integration files. Close the PR.
2. **Files outside repo root fix** — Still needed. When crit is given a file outside the repo root (e.g. `~/.claude/plans/foo.md`), `filepath.Rel` produces a `../..` path that the `/files/` endpoint rejects. Our fix: use absolute paths and validate against known session files.

We also have prior changes to prevent publishing to public registries:
3. **No Homebrew tap push** — upstream's release.yml now pushes to `tomasz-tomczyk/homebrew-tap` using a `HOMEBREW_TAP_TOKEN` secret. We don't have this secret, so it will fail silently, but we should strip this step to keep our workflow clean.
4. **No Nix flake publishing** — the `flake.nix` references `tomasz-tomczyk/crit`. We should either update it to our fork or leave it inert.
5. **Marketplace JSON** — upstream added `.claude-plugin/marketplace.json` and `.cursor-plugin/marketplace.json` pointing to `tomasz-tomczyk/crit`. These are discovery metadata — harmless in our fork but we should be aware they exist.

## Steps

### 1. Close PR #47

Close with a note that upstream implemented instance reuse natively and restructured integrations.

### 2. Merge upstream/main

```bash
git fetch upstream main && git merge upstream/main
```

Expected conflicts:
- **integrations/** — our modified files were deleted/moved upstream → take upstream's version
- **session.go** — upstream extracted watch logic into `watch.go`, refactored round-complete → re-apply our two-line fix
- **server.go** — upstream may have changed `handleFiles` → re-apply our absolute-path block
- **main.go** — upstream extracted subcommands into functions → take upstream, no custom code here
- **release.yml** — upstream added Homebrew tap step → take upstream then strip Homebrew step (step 2b)

### 2b. Strip public publishing from release workflow

After merge, remove the "Update Homebrew formula" step from `.github/workflows/release.yml`. This step pushes to `tomasz-tomczyk/homebrew-tap` which we don't control and don't want to publish to.

The Nix flake and marketplace JSON files can stay as-is — they reference `tomasz-tomczyk/crit` but are inert in our fork (Nix flake is a local build tool, marketplace JSON is discovery metadata that only matters when listed in a registry).

### 3. Re-apply the files-outside-repo-root fix

Two changes to verify/re-apply after merge:

**session.go** — In `NewSessionFromFiles`, where `filepath.Rel(root, absPath)` is called:
```go
// Before (broken):
if rel, err := filepath.Rel(root, absPath); err == nil {
    relPath = rel
}

// After (fixed):
if rel, err := filepath.Rel(root, absPath); err == nil && !strings.HasPrefix(rel, "..") {
    relPath = rel
}
```

**session.go** — Add `isSessionFile` method:
```go
func (s *Session) isSessionFile(absPath string) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    for _, f := range s.Files {
        if f.AbsPath == absPath {
            return true
        }
    }
    return false
}
```

**server.go** — In `handleFiles`, before the `..` check, add absolute path handling:
```go
if filepath.IsAbs(reqPath) {
    if s.session.isSessionFile(reqPath) {
        http.ServeFile(w, r, reqPath)
    } else {
        http.Error(w, "Access denied", http.StatusForbidden)
    }
    return
}
```

### 4. Update local skill

Rewrite `.claude/commands/crit.md` to match the new upstream skill at `integrations/claude-code/commands/crit.md`. Key changes:
- Uses `crit listen <port>` instead of asking user to type "go"
- No confirmation step — just proceed
- Quote-aware comments (`quote` field)
- Agent marks comments `resolved: true` in `.crit.json`
- Self-contained loop (Step 6 runs `crit listen` again automatically)
- Sharing support (`crit share`)

### 5. Build, test, verify

```bash
go test ./...
go build -o ~/.local/bin/crit .
crit --no-open --port 3199 ~/.claude/plans/some-plan.md  # verify outside-root fix
```

### 6. Optional: upstream the fix

Open a new PR for the files-outside-repo-root fix against current upstream. This is a clean 3-file change (session.go + server.go + test) independent of integration restructuring.

## Risk assessment

- **Low risk**: Closing PR #47, updating local skill, rebuilding
- **Medium risk**: Merge conflicts in session.go/server.go — upstream refactored these files significantly. The fix is small but needs to land in the right place in the refactored code.
- **No risk to upstream**: Our fix is additive (new method + two small guard clauses), doesn't change existing behavior for in-repo files.
