build:
	go build -o planreview .

build-all:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -o dist/planreview-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -o dist/planreview-darwin-amd64 .
	GOOS=linux GOARCH=amd64 go build -o dist/planreview-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -o dist/planreview-linux-arm64 .

update-deps:
	bun install
	bun run update-deps

clean:
	rm -f planreview
	rm -rf dist

.PHONY: build build-all update-deps clean
