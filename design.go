package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type smokeKind int

const (
	smokeOK          smokeKind = iota
	smokeConnRefused           // connection refused / DNS failure
	smokeNon2xx                // non-2xx HTTP status (warn, continue)
	smokeNonHTML               // non-text/html content type (fatal)
	smokeMissingBody           // HTML without </body> (warn, continue)
)

type smokeResult struct {
	kind                 smokeKind
	fatal                bool
	message              string
	hasCSPFrameAncestors bool
}

var smokeClient = &http.Client{Timeout: 10 * time.Second}

func runSmokeTest(origin string) smokeResult {
	resp, err := smokeClient.Get(origin)
	if err != nil {
		return smokeResult{
			kind:    smokeConnRefused,
			fatal:   true,
			message: fmt.Sprintf("is your dev server running at %s? (%v)", origin, err),
		}
	}
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	hasCSP := strings.Contains(strings.ToLower(csp), "frame-ancestors")

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return smokeResult{
			kind:                 smokeNon2xx,
			fatal:                false,
			message:              fmt.Sprintf("upstream returned %d; design mode may not work as expected", resp.StatusCode),
			hasCSPFrameAncestors: hasCSP,
		}
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		return smokeResult{
			kind:    smokeNonHTML,
			fatal:   true,
			message: fmt.Sprintf("design mode needs HTML; this URL returns %q. Did you mean a different URL?", ct),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return smokeResult{kind: smokeOK, hasCSPFrameAncestors: hasCSP}
	}

	if !strings.Contains(strings.ToLower(string(body)), "</body>") {
		return smokeResult{
			kind:                 smokeMissingBody,
			fatal:                false,
			message:              "couldn't find a </body> injection target; design-mode agent may not boot",
			hasCSPFrameAncestors: hasCSP,
		}
	}

	return smokeResult{kind: smokeOK, hasCSPFrameAncestors: hasCSP}
}

// looksLikeDesignArgs returns true when args is exactly one element
// that parses as an http:// or https:// URL.
func looksLikeDesignArgs(args []string) bool {
	if len(args) != 1 {
		return false
	}
	u, err := url.Parse(args[0])
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// runDesign is the entry point for `crit design <url>`.
func runDesign(args []string) {
	rawURL := ""
	for _, a := range args {
		if len(a) > 0 && a[0] != '-' {
			rawURL = a
			break
		}
	}
	if rawURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: crit design <url>")
		os.Exit(1)
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		fmt.Fprintf(os.Stderr, "crit design: %q is not a valid http/https URL\n", rawURL)
		os.Exit(1)
	}
	origin := u.Scheme + "://" + u.Host

	// 1. Smoke test.
	result := runSmokeTest(origin)
	switch result.kind {
	case smokeConnRefused, smokeNonHTML:
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.message)
		os.Exit(1)
	case smokeNon2xx:
		fmt.Fprintf(os.Stderr, "[crit] warning: %s\n", result.message)
	case smokeMissingBody:
		fmt.Fprintf(os.Stderr, "[crit] warning: %s\n", result.message)
	}
	if result.hasCSPFrameAncestors {
		fmt.Fprintf(os.Stderr, "[crit] note: upstream has frame-ancestors CSP; stripped by proxy\n")
	}

	// 2. Session key + existing daemon check.
	cwd, err := resolvedCWD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	key := designSessionKey(cwd, origin)

	if entry, alive := findAliveSession(key); alive {
		fmt.Fprintf(os.Stderr, "[crit] connected to design daemon at http://localhost:%d (proxy :%d)\n",
			entry.Port, entry.Port+1)
		if !daemonHasBrowser(entry) {
			go openBrowser(fmt.Sprintf("http://localhost:%d/design", entry.Port))
		}
		runReviewClient(entry)
		return
	}

	// 3. Spawn daemon via _serve. startDaemon prepends "_serve" itself.
	daemonArgs := []string{"--design-origin", origin}
	entry, err := startDaemon(key, daemonArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not start design daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[crit] starting daemon on :%d (api), :%d (proxy)\n",
		entry.Port, entry.Port+1)

	installDaemonSignalHandler(entry.PID)

	// 4. Open browser.
	go openBrowser(fmt.Sprintf("http://localhost:%d/design", entry.Port))

	// 5. Block until review complete.
	runReviewClient(entry)
}
