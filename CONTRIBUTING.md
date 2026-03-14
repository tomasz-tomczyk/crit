# Contributing to Crit

## Before You Start

For bug fixes and small improvements, feel free to open a PR directly. For larger changes — new features, significant refactors, or anything that touches core architecture — please open an issue first to discuss the approach. This avoids spending time on something that might not be the right direction.

## Build from Source

Requires Go 1.26+ (install via [asdf](https://asdf-vm.com/), Homebrew, or [go.dev](https://go.dev/dl/)):

```bash
git clone https://github.com/tomasz-tomczyk/crit.git
cd crit
go build -o crit .
mv crit /usr/local/bin/
```

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

## Visual Diff Testing

`make test-diff` runs a manual visual test that simulates a full multi-round review:

1. Starts Crit on a fixture markdown file
2. Seeds 4 review comments via the API
3. Pauses so you can inspect the comments in the browser
4. Swaps in a v2 version of the file (simulating agent edits)
5. Marks 3 of the 4 comments as resolved, signals round-complete
6. Opens the diff view with resolved/open comments visible

Use this when working on diff rendering, round-to-round state, or the resolved comment UI — areas where automated assertions are hard to write but visual correctness matters.

```bash
make test-diff          # runs on port 3001
```

## Linting

```bash
gofmt -l .
golangci-lint run ./...
```
