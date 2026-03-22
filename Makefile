VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%d)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o crit .

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

test-daemon:
	./test/test-daemon-reuse.sh

clean:
	rm -f crit
	rm -rf dist

e2e:
	cd e2e && bash run.sh

e2e-failed:
	cd e2e && npx playwright test --last-failed

e2e-report:
	cd e2e && npx playwright show-report

.PHONY: build build-all update-deps test setup-hooks clean test-diff test-daemon e2e e2e-failed e2e-report
