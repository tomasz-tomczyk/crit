package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
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
	frameworkNotes       []string
}

// phoenixRE matches one of three discriminating Phoenix LiveView markers.
// A bare "phx-" substring is too loose — third-party CSS libraries use
// `phx-` prefixes in unrelated contexts.
var phoenixRE = regexp.MustCompile(`phx-track-static|phx-hook=|phx-main`)

// detectFrameworks returns informational notes (one per detected framework)
// produced by probing the upstream HTML body. None block startup.
func detectFrameworks(body []byte) []string {
	var notes []string
	if phoenixRE.Match(body) {
		notes = append(notes, "Phoenix LiveView detected. Make sure your dev endpoint allows iframing — strip CSP locally if needed.")
	}
	if bytes.Contains(body, []byte(`/@vite/client`)) {
		notes = append(notes, "Vite dev server detected. WebSocket HMR will be proxied automatically.")
	}
	if bytes.Contains(body, []byte(`id="__next"`)) {
		notes = append(notes, "Next.js dev detected. SPA route changes via `pushState` are supported.")
	}
	return notes
}

var smokeClient = &http.Client{Timeout: 10 * time.Second}

func runSmokeTest(origin, cookies string) smokeResult {
	req, err := http.NewRequest(http.MethodGet, origin, nil)
	if err != nil {
		return smokeResult{
			kind:    smokeConnRefused,
			fatal:   true,
			message: fmt.Sprintf("is your dev server running at %s? (%v)", origin, err),
		}
	}
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
	resp, err := smokeClient.Do(req)
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
			message:              fmt.Sprintf("upstream returned %d; live mode may not work as expected", resp.StatusCode),
			hasCSPFrameAncestors: hasCSP,
		}
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		return smokeResult{
			kind:    smokeNonHTML,
			fatal:   true,
			message: fmt.Sprintf("live mode needs HTML; this URL returns %q. Did you mean a different URL?", ct),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		// A read failure already signals an unstable upstream; skip the
		// missing-</body> warning here so it doesn't add noise on top.
		return smokeResult{kind: smokeOK, hasCSPFrameAncestors: hasCSP}
	}

	notes := detectFrameworks(body)
	if !strings.Contains(strings.ToLower(string(body)), "</body>") {
		return smokeResult{
			kind:                 smokeMissingBody,
			fatal:                false,
			message:              "couldn't find a </body> injection target; live-mode agent may not boot",
			hasCSPFrameAncestors: hasCSP,
			frameworkNotes:       notes,
		}
	}

	return smokeResult{kind: smokeOK, hasCSPFrameAncestors: hasCSP, frameworkNotes: notes}
}

// looksLikeLiveArgs returns true when args is exactly one element
// that parses as an http:// or https:// URL.
func looksLikeLiveArgs(args []string) bool {
	if len(args) != 1 {
		return false
	}
	u, err := url.Parse(args[0])
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// connectToLiveDaemon attaches the current CLI to an already-running live
// daemon for key, blocking on its review session. Returns true when an alive
// daemon was found and the review session has completed; false when the caller
// should spawn a fresh daemon.
func connectToLiveDaemon(key string) bool {
	entry, alive := findAliveSession(key)
	if !alive {
		return false
	}
	fmt.Fprintf(os.Stderr, "[crit] connected to live daemon at %s (proxy :%d)\n",
		entry.baseURL(), entry.Port+1)
	fmt.Fprintf(os.Stderr, "[crit] open %s/live\n", entry.baseURL())
	if !daemonHasBrowser(entry) {
		go openBrowser(entry.baseURL() + "/live")
	}
	runReviewClient(entry, key)
	return true
}

type liveCLIFlags struct {
	port        int
	host        string
	noOpen      bool
	quiet       bool
	shareURL    string
	cookieFlags stringSliceFlag
	cookieFile  string
	origin      string
}

func parseLiveCLIFlags(args []string) liveCLIFlags {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	port := fs.Int("port", 0, "Port to listen on")
	fs.IntVar(port, "p", 0, "Port (shorthand)")
	host := fs.String("host", "", "Host to listen on")
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status (shorthand)")
	shareURL := fs.String("share-url", "", "Share service URL")
	var cookieFlags stringSliceFlag
	fs.Var(&cookieFlags, "cookie", "Cookie header value for upstream requests (repeatable)")
	cookieFile := fs.String("cookie-file", "", "File with upstream cookies (raw header or Netscape jar)")
	fs.Parse(args)

	rawURL := ""
	for _, a := range fs.Args() {
		if len(a) > 0 && a[0] != '-' {
			rawURL = a
			break
		}
	}
	if rawURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: crit live <url>")
		os.Exit(1)
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		fmt.Fprintf(os.Stderr, "crit live: %q is not a valid http/https URL\n", rawURL)
		os.Exit(1)
	}
	// Preserve the full URL including path so the frontend can load the
	// correct page (e.g. http://localhost:3333/live.html, not just /).
	u.RawQuery = ""
	u.Fragment = ""
	return liveCLIFlags{
		port:        *port,
		host:        *host,
		noOpen:      *noOpen,
		quiet:       *quiet,
		shareURL:    *shareURL,
		cookieFlags: cookieFlags,
		cookieFile:  *cookieFile,
		origin:      strings.TrimSuffix(u.String(), "/"),
	}
}

func buildLiveDaemonArgs(origin, liveCookies string, f liveCLIFlags, cfg Config, noOpenResolved bool) []string {
	daemonArgs := []string{"--live-origin", origin}
	if liveCookies != "" {
		daemonArgs = append(daemonArgs, "--live-cookie", liveCookies)
	}
	return appendCommonDaemonFlags(daemonArgs, commonDaemonFlags{
		port:     resolvePort(f.port, cfg.Port),
		host:     resolveHost(f.host, cfg.Host),
		noOpen:   noOpenResolved,
		quiet:    f.quiet || cfg.Quiet,
		shareURL: resolveShareURL(f.shareURL, cfg, ""),
	})
}

// runLive is the entry point for `crit live <url>`.
func runLive(args []string) {
	f := parseLiveCLIFlags(args)

	cwd, err := resolvedCWD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cfg := LoadConfig(cwd)
	liveCookies, err := resolveLiveCookies(f.cookieFlags, f.cookieFile, cfg, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	checkLiveSmoke(f.origin, liveCookies)

	key := liveSessionKey(cwd, f.origin)
	if connectToLiveDaemon(key) {
		return
	}

	noOpenResolved := f.noOpen || cfg.NoOpen
	entry, err := startDaemon(key, buildLiveDaemonArgs(f.origin, liveCookies, f, cfg, noOpenResolved))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not start live daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[crit] starting daemon on :%d (api), :%d (proxy)\n",
		entry.Port, entry.Port+1)
	fmt.Fprintf(os.Stderr, "[crit] open %s/live\n", entry.baseURL())

	installDaemonSignalHandler(entry.PID)

	if !noOpenResolved {
		go openBrowser(entry.baseURL() + "/live")
	}

	runReviewClient(entry, key)
}

func checkLiveSmoke(origin, cookies string) {
	result := runSmokeTest(origin, cookies)
	switch result.kind {
	case smokeConnRefused, smokeNonHTML:
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.message)
		os.Exit(1)
	case smokeNon2xx, smokeMissingBody:
		fmt.Fprintf(os.Stderr, "[crit] warning: %s\n", result.message)
	}
	if result.hasCSPFrameAncestors {
		fmt.Fprintf(os.Stderr, "[crit] note: upstream has frame-ancestors CSP; stripped by proxy\n")
	}
	for _, n := range result.frameworkNotes {
		fmt.Fprintf(os.Stderr, "[crit] note: %s\n", n)
	}
}
