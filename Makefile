VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%d)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build: generate
	go build -ldflags "$(LDFLAGS)" -o crit .

generate:
	go generate ./...

verify-generate:
	go generate ./...
	git diff --exit-code integration_hashes_gen.go || (echo "ERROR: integration_hashes_gen.go is stale. Run 'go generate ./...' and commit." && exit 1)

build-all:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/crit-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crit-darwin-amd64 .
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/crit-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/crit-linux-arm64 .

update-deps:
	bun install
	bun run update-deps

test:
	go test ./...

setup-hooks:
	git config core.hooksPath .githooks

test-diff:
	./test/test-diff.sh

test-share-sync: build
	go test -tags integration -run TestShareSync -v -count=1 ./...

e2e-share:
	./scripts/e2e-share.sh

test-daemon:
	./test/test-daemon-reuse.sh

test-plan-daemon:
	./test/test-plan-daemon.sh

clean:
	rm -f crit
	rm -rf dist

e2e:
	cd e2e && bash run.sh

e2e-failed:
	cd e2e && npx playwright test --last-failed

e2e-report:
	cd e2e && npx playwright show-report

.PHONY: build build-all generate verify-generate update-deps test setup-hooks clean test-diff test-share-sync e2e-share test-daemon test-plan-daemon e2e e2e-failed e2e-report
