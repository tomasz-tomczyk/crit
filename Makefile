VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

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

clean:
	rm -f crit
	rm -rf dist

.PHONY: build build-all update-deps test setup-hooks clean
