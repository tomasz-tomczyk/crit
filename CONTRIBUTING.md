# Contributing to Crit

## Before You Start

For bug fixes and small improvements, feel free to open a PR directly. For larger changes — new features, significant refactors, or anything that touches core architecture — please open an issue first to discuss the approach. This avoids spending time on something that might not be the right direction.

## Build from Source

See [Other Install Methods](README.md#other-install-methods) in the README for build instructions.

### Cross-compile

```bash
make build-all
# Outputs to dist/:
#   crit-darwin-arm64, crit-darwin-amd64
#   crit-linux-amd64, crit-linux-arm64
```

## Go Tests

```bash
go test ./...
```

## E2E Tests

The `e2e/` directory has a Playwright test suite that runs the full frontend against a real Crit server. Requires Node.js (listed in `mise.toml`).

```bash
cd e2e && npm install && npx playwright install chromium

make e2e                                              # Run full suite
cd e2e && npx playwright test tests/comments.spec.ts  # Run one test file
cd e2e && npx playwright test --headed                # Run with visible browser
make e2e-report                                       # View HTML report
```

**If your change touches the frontend, include E2E tests.** See the test organization table in `CLAUDE.md` and the existing specs in `e2e/tests/` for conventions and helpers.

## Local Testing & Seed Fixtures

`make test-diff` is a manual, visual seed harness for the review UI. It builds `crit`, spins up several local server instances — each seeding a different representative review scenario — prints their localhost URLs, and then blocks so you can open the tabs and eyeball the result. It is **not** an automated assertion suite; it exists for the parts of the review UI where visual correctness matters and automated assertions are awkward to write.

```bash
make test-diff          # builds crit, seeds the scenarios, runs from port 3001 up
```

Each instance binds a consecutive port starting at the one you pass (default `3001`) and covers a distinct scenario:

| # | Port | Scenario |
| --- | --- | --- |
| 1 | `3001` | Multi-round markdown review — resolved comments, threaded replies, deletion markers, inter-round diff |
| 2 | `3002` | Code diff — word-level diff, folded-line comments in spacer gaps, orphaned comments on a removed file |
| 3 | `3003` | Carry-forward (file mode) — comment positioning across a v1 → v2 content change |
| 4 | `3004` | Carry-forward (git mode) — same carry-forward exercise in a git context |
| 5 | `3005` | Range mode (`--range A..B`) — SHA-pinned diff, focus-picker round-trip |
| 6 | `3006` | Stacked PR — layer / full-stack diff-scope toggle and the push gate |

The harness seeds comments, swaps in v2 content to simulate agent edits, and signals round-complete, then prints what to look for at each URL. Use it when working on diff rendering, round-to-round state, the resolved-comment UI, carry-forward, range mode, or the stacked-PR toggle.

**Treat these scenarios as living seed fixtures.** When you add or change a review-UI feature, add a new seeded scenario (or extend an existing one) in `test/test-diff.sh` so a reviewer can spin it up and eyeball your change locally. A new scenario typically means starting another server instance on the next port and seeding the comments/content that exercise the feature.

## Integration Tests

These exercise crit against its real collaborators — `crit-web` and GitHub. They are heavier than the unit suite and live behind build tags so `go test ./...` stays fast and hermetic. Extend them when you touch the surfaces they cover.

### crit ↔ crit-web share roundtrip

`make e2e-share` runs the share roundtrip in `share_integration_test.go` (build tag `integration`): share a review, fetch web-authored comments, re-share without duplicates, unpublish. It needs a local `crit-web` checkout at `../crit-web` (or `CRIT_WEB_DIR`) and PostgreSQL running locally.

```bash
make e2e-share                                   # build crit, start crit-web on :4001, run all TestShareSync*, tear down
./scripts/e2e-share.sh --serve                   # start crit-web for manual inspection (logs review URLs)
./scripts/e2e-share.sh -run TestShareSyncFullLifecycle   # one case
```

When you change the share payload, comment sync, or any crit-web interaction, **add a `TestShareSync*` case** so the new behavior is covered, and use `--serve` to inspect the result on the web. See `scripts/AGENTS.md` for prerequisites, the full case list, and the seed helpers (`critShareCmd`, `seedComment`, `logReview`, etc.).

### crit ↔ GitHub PR roundtrip

`make e2e-roundtrip` runs the live GitHub PR roundtrip in `roundtrip_integration_test.go` (build tag `e2e_github`): each scenario opens a real sandbox PR, drives `crit pull` / `crit push` through one state transition, and asserts on both local review-file and live PR state. It needs `gh` authenticated and `CRIT_ROUNDTRIP_REPO=<owner>/crit-roundtrip-sandbox` exported. Scenarios are slow (~10–25s each) and rate-limited, which is why they stay out of CI.

```bash
make e2e-roundtrip                                              # all scenarios
./scripts/e2e-roundtrip.sh -run TestRoundtrip_PushIsIdempotent -v   # one
```

When you change `crit pull` / `crit push`, GitHub comment-bucket logic, or reply posting, **add a `TestRoundtrip_<Name>` scenario** for the new state transition. If a scenario is currently `t.Skip`'d against a known bug and your change fixes it, remove the skip and run it. See `test/roundtrip/README.md` for one-time setup and authoring notes.

### Leave a seed behind

When you ship a review-UI feature, leave a seeded scenario in `test/test-diff.sh`; when you ship share or GitHub-sync behavior, leave an integration test case. The next contributor — and you, three months from now — should be able to spin up your feature and verify it without reverse-engineering it first.

## Linting

```bash
gofmt -l .
golangci-lint run ./...
```
