# Roundtrip integration tests

End-to-end exercise of `crit pull` / `crit push` against a real GitHub repo
and PR. Each scenario:

1. Clones a sandbox repo into a tempdir
2. Pushes a unique branch + opens a PR
3. Drives `crit` (and `gh api`) through one specific state transition
4. Asserts on local review-file state AND live PR comment state via `gh api`
5. Closes the PR and deletes the branch in `t.Cleanup`

## One-time setup

1. Pick a GitHub account or org you control. The sandbox repo is throwaway. The default in the harness is `crit-md/crit-roundtrip-sandbox`; override with your own slug if you don't have access.
2. Run the bootstrap procedure documented at the top of `docs/superpowers/plans/2026-05-04-pr-roundtrip-test-harness.md` (Task 1). It creates `<owner>/crit-roundtrip-sandbox`.
3. Export `CRIT_ROUNDTRIP_REPO=<owner>/crit-roundtrip-sandbox` in your shell (or stick it in a direnv `.envrc.local`).
4. Confirm `gh auth status` is green.

## Running

```bash
make e2e-roundtrip                                                  # all scenarios
./scripts/e2e-roundtrip.sh -run TestRoundtrip_PushIsIdempotent -v   # one
```

## Adding a scenario

- Add a `TestRoundtrip_<Name>` function to `roundtrip_integration_test.go`.
- Always start with `e := newRoundtripEnv(t)` — it owns branch+PR lifecycle.
- Read remote state via `e.listRemoteComments()`; read local state via `e.allLocalComments()` or `e.reviewFile()`.
- For new low-level helpers (e.g. editing a comment via the API), add them to `roundtrip_helpers_test.go` rather than inline.

## Caveats

- These tests hit GitHub's public API. They are slow (~10–25s per scenario) and rate-limited. Don't add them to the default `go test` run — they live behind the `e2e_github` build tag for that reason.
- Each scenario costs one open-then-closed PR in the sandbox. GitHub does not allow deleting closed PRs, so the sandbox accumulates closed PR history. That's fine.
- `gh pr close --delete-branch` is best-effort. If a test panics before cleanup runs, dangling branches remain — clean periodically with `gh pr list --repo <slug> --state closed --limit 100 --json headRefName` if you care.

## Cloning over HTTPS vs SSH

Helpers default to `git@github.com:<slug>.git` (SSH) so they work with the typical `gh` setup that uses SSH for git operations. To force HTTPS (or any other URL form), export `CRIT_ROUNDTRIP_CLONE_URL=https://github.com/<slug>.git` before running the suite.
