VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%d)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build: generate
	go build -ldflags "$(LDFLAGS)" -o crit ./cmd/crit

generate:
	go generate ./...

verify-generate:
	go generate ./...
	git diff --exit-code cmd/crit/integration_hashes_gen.go || (echo "ERROR: integration_hashes_gen.go is stale. Run 'go generate ./...' and commit." && exit 1)

build-all:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/crit-darwin-arm64 ./cmd/crit
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crit-darwin-amd64 ./cmd/crit
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crit-linux-amd64 ./cmd/crit
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/crit-linux-arm64 ./cmd/crit
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crit-windows-amd64.exe ./cmd/crit
	GOOS=windows GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/crit-windows-arm64.exe ./cmd/crit

update-deps:
	npm install --ignore-scripts
	npm run update-deps

test:
	go test ./...

test-frontend:
	npm run test:frontend

verify-assets:
	npm run verify-assets:installed
	npm run update-deps
	git diff --exit-code -- web/dompurify.min.js web/markdown-it.min.js web/mermaid.min.js web/highlight.min.js web/diff-match-patch.min.js

# Run Go benchmarks locally. Compare against a base with:
#   git worktree add /tmp/crit-base origin/main
#   go test -run='^$' -bench=. -benchmem -count=6 ./internal/diff/ ./internal/session/ > old.txt  (in /tmp/crit-base)
#   go test -run='^$' -bench=. -benchmem -count=6 ./internal/diff/ ./internal/session/ > new.txt  (here)
#   benchstat old.txt new.txt   (go install golang.org/x/perf/cmd/benchstat@latest)
bench:
	go test -run='^$' -bench=. -benchmem -count=6 ./internal/diff/ ./internal/session/

bench-compare:
	benchstat bench-old.txt bench-new.txt | tee benchstat.txt
	python3 scripts/bench-compare.py benchstat.txt

setup-hooks:
	git config core.hooksPath .githooks

test-diff:
	./test/shell/test-diff.sh

test-share-sync: build
	go test -tags integration -run TestShareSync -v -count=1 ./...

test-share-sync-selfhosted: build
	@./scripts/run-selfhosted-tests.sh

test-live-cdp:
	go test -tags integration -run TestLiveCDPIntegration -v -count=1 ./internal/live/...

e2e-share:
	./scripts/e2e-share.sh

e2e-roundtrip: build
	./scripts/e2e-roundtrip.sh

e2e-gitlab-roundtrip: build
	./scripts/e2e-gitlab-roundtrip.sh

test-daemon:
	./test/shell/test-daemon-reuse.sh

test-plan-daemon:
	./test/shell/test-plan-daemon.sh

clean:
	rm -f crit
	rm -rf dist

e2e:
	cd test/e2e && bash run.sh

e2e-failed:
	cd test/e2e && npx playwright test --last-failed

e2e-report:
	cd test/e2e && npx playwright show-report

e2e-live-utils:
	node --test web/__tests__/*.test.js

test-preview: build
	@echo "Starting preview mode with sample page..."
	./crit preview test/preview-sample/index.html

.PHONY: build build-all generate verify-generate update-deps test test-frontend verify-assets setup-hooks clean test-diff test-share-sync test-share-sync-selfhosted test-live-cdp e2e-share e2e-roundtrip e2e-gitlab-roundtrip test-daemon test-plan-daemon e2e e2e-failed e2e-report e2e-live-utils test-preview
