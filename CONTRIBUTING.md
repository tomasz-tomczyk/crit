# Contributing to Crit

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

## E2E Tests

The `e2e/` directory has a Playwright test suite that runs the full frontend against a real Crit server. Requires Node.js (listed in `mise.toml`).

```bash
cd e2e && npm install && npx playwright install chromium

make e2e                                              # Run full suite
cd e2e && npx playwright test tests/comments.spec.ts  # Run one test file
cd e2e && npx playwright test --headed                # Run with visible browser
make e2e-report                                       # View HTML report
```

## Go Tests

```bash
go test ./...
```

## Linting

```bash
gofmt -l .
golangci-lint run ./...
```

