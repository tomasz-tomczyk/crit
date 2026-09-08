# Go unit-test coverage audit

## Gaps found

- `internal/config`: the public CLI resolution helpers had no direct tests. Flag/environment/config precedence was uncovered, including malformed `CRIT_PORT` handling and the meaningful distinction between an unset and explicitly empty `CRIT_SHARE_URL`.
- `internal/prompt`: failure behavior around custom prompts was uncovered. In particular, a missing project prompt should fall back to a valid global prompt, while a malformed selected template should return the safe `Review finished.` response. Unknown non-finish hooks and deduplication of displayed project prompt sources were also untested.
- `internal/browser`: command construction was covered, but the execution boundary was not. There was no test proving that a URL containing shell metacharacters is passed as one literal argument, or that launcher start failures are returned for fallback handling.

## Tests added

- `internal/config/cli_resolve_unit_test.go`
  - Covers flag > environment > config precedence for port and host.
  - Covers invalid port environment fallback.
  - Covers flag > environment > config > fallback precedence for share URLs.
  - Covers explicit empty `CRIT_SHARE_URL` disabling the configured/fallback service.
- `internal/prompt/render_errors_test.go`
  - Covers project prompt load failure falling back to the global prompt.
  - Covers malformed finish templates degrading to the safe finish message without metadata.
  - Covers the error returned for a hook with no configured, discovered, or stock template.
  - Covers project config/source reporting and duplicate referenced-file suppression.
- `internal/browser/browser_exec_test.go`
  - Covers literal URL argv propagation through the launcher process boundary.
  - Covers a missing launcher returning an error.

No production code was changed.

## Coverage impact

- `internal/config`: 78.3% to 81.4%; `ResolvePort`, `ResolveHost`, and `ResolveShareURL` are now 100% statement-covered.
- `internal/prompt`: 80.5% to 85.6%; `ListProjectPromptSources` is now 100%, and the main render/fallback paths increased materially.
- `internal/browser`: 68.3% to 70.7%; `runBrowserCommand` is now 100% statement-covered.

## Gaps still open

- `internal/browser.OpenBrowserWithCommand` and `systemIsWSL` retain host/platform-specific branches. Covering them well would require a clock/filesystem seam or OS-specific runners; the pure platform selection logic already has broad coverage.
- `internal/config` still has low-probability filesystem/lock-timeout branches and external `jj`/`sl` username command fallbacks. These are awkward to exercise deterministically without production seams or multi-process coordination.
- `internal/prompt.ResolveFinishTemplateSpecific` still has analogous filesystem error branches, while the shared non-specific fallback and rendering contracts are now covered.
- `internal/clicmd.Exit`/`Exitf` remain uncovered inside their package because they terminate the test process. They can be tested with more helper-subprocess cases, but were lower value than the selected configuration and prompt behavior.
- A broad baseline run could not complete `internal/auth`, `internal/comment`, or `internal/share` in this sandbox because existing `httptest.NewServer` tests are not permitted to bind loopback ports. Their existing suites were inspected and are already extensive, but a full coverage profile for those three packages should be regenerated in an unrestricted environment.

## Commands run

```bash
GOCACHE=/tmp/crit-go-cache mise exec -- go test -coverprofile=/tmp/crit-light.cover ./internal/hooks ./internal/notify ./internal/diff ./internal/browser ./internal/clicmd ./internal/reviewpath ./internal/forge ./internal/auth ./internal/story ./internal/prompt ./internal/config ./internal/comment ./internal/share ./web
GOCACHE=/tmp/crit-go-cache mise exec -- go test ./internal/config ./internal/prompt ./internal/browser
GOCACHE=/tmp/crit-go-cache mise exec -- go test -count=1 -coverprofile=/tmp/config-after.cover ./internal/config
GOCACHE=/tmp/crit-go-cache mise exec -- go test -count=1 -coverprofile=/tmp/prompt-after.cover ./internal/prompt
GOCACHE=/tmp/crit-go-cache mise exec -- go test -count=1 -coverprofile=/tmp/browser-after.cover ./internal/browser
git diff --check
git add RESULT.md internal/browser/browser_exec_test.go internal/config/cli_resolve_unit_test.go internal/prompt/render_errors_test.go
```

The three touched packages passed. The broad baseline command passed the non-network packages but failed when existing loopback-server tests attempted to bind ports in the sandbox.

The final `git add`/commit step was blocked because this linked worktree's Git index is under the read-only parent repository (`crit/.git/worktrees/crit.unit-audit-coverage/index`). The working tree is ready to commit, but no commit was created in this sandbox.
