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
	fmt.Fprintf(os.Stderr, "[crit] design mode — origin: %s (Phase A stub)\n", origin)
	_ = origin
}
