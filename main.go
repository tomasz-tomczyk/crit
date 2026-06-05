package main

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"
	"golang.org/x/term"
)

//go:embed frontend/*.html frontend/*.css frontend/*.js frontend/*.png frontend/*.svg frontend/*.ico frontend/*.webmanifest
var frontendFS embed.FS

//go:embed all:integrations/*
var integrationsFS embed.FS

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		runReview(nil)
		return
	}
	if handler, ok := commandDispatch[os.Args[1]]; ok {
		handler(os.Args[2:])
		return
	}
	args := os.Args[1:]
	if looksLikeLiveArgs(args) {
		runLive(args)
		return
	}
	if looksLikePreviewArgs(args) {
		runPreview(args)
		return
	}
	runReview(args)
}

type shareFlags struct {
	outputDir  string
	svcURL     string
	showQR     bool
	org        string
	visibility string
	preview    string
	files      []string
}

func parseShareFlags(args []string) shareFlags {
	var sf shareFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			sf.outputDir = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --share-url requires a value\n")
				os.Exit(1)
			}
			i++
			sf.svcURL = args[i]
		case arg == "--preview":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --preview requires an HTML file path\n")
				os.Exit(1)
			}
			i++
			sf.preview = args[i]
		case arg == "--qr":
			sf.showQR = true
		case arg == "--org":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --org requires a value\n")
				os.Exit(1)
			}
			i++
			sf.org = args[i]
		case arg == "--visibility":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --visibility requires a value\n")
				os.Exit(1)
			}
			i++
			sf.visibility = args[i]
		default:
			sf.files = append(sf.files, arg)
		}
	}
	return sf
}

// runSharePreview crawls the local HTML file referenced by sf.preview and its
// assets, then uploads the snapshot to crit-web as a preview review. It is the
// --preview branch of the share command (the direct transport; the in-UI Share
// button and proxy-auth relay are handled in server.go / frontend/app.js).
func runSharePreview(sf shareFlags) {
	if len(sf.files) > 0 {
		fmt.Fprintln(os.Stderr, "Error: --preview cannot be combined with file arguments")
		os.Exit(1)
	}
	cfg := loadShareConfig()
	svcURL := resolveShareURL(sf.svcURL, cfg, defaultShareURL)
	url, err := postPreviewShare(sf.preview, svcURL, resolveAuthToken(cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(url)
}

// postPreviewShare crawls preview files and POSTs them to crit-web with
// review_type=preview, returning the resulting share URL.
func postPreviewShare(htmlPath, svcURL, authToken string) (string, error) {
	files, err := crawlPreview(htmlPath)
	if err != nil {
		return "", fmt.Errorf("crawling preview assets: %w", err)
	}

	payload := buildSharePayload(files, nil, 1, nil, "", "", "preview")
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling preview payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, svcURL+"/api/reviews", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("posting preview to share service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", errShareUnauthorized
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("preview share failed with status %d", resp.StatusCode)
	}

	var result struct {
		URL         string `json:"url"`
		DeleteToken string `json:"delete_token"`
	}
	if err := decodeJSONOrHTMLHint(resp, &result); err != nil {
		return "", err
	}
	return result.URL, nil
}

func printShareUsage() {
	fmt.Fprintln(os.Stderr, "Usage: crit share [--output <dir>] [--share-url <url>] [--org <slug>] [--visibility <level>] [--qr] <file> [file...]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Shares files to crit-web and prints the review URL.")
	fmt.Fprintln(os.Stderr, "Comments from the review file are included automatically.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  crit share plan.md")
	fmt.Fprintln(os.Stderr, "  crit share plan.md src/main.go")
	fmt.Fprintln(os.Stderr, "  crit share --qr plan.md")
	os.Exit(1)
}

func loadShareFiles(paths []string) []shareFile {
	var files []shareFile
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", path, err)
			os.Exit(1)
		}
		relPath := path
		if filepath.IsAbs(path) {
			if rel, err := filepath.Rel(mustGetwd(), path); err == nil {
				relPath = rel
			}
		}
		files = append(files, shareFile{Path: relPath, Content: string(content)})
	}
	return files
}

func printQR(url string, showQR bool) {
	if showQR {
		fmt.Println()
		qrterminal.GenerateWithConfig(url, qrterminal.Config{
			Level:      qrterminal.L,
			Writer:     os.Stdout,
			HalfBlocks: true,
			QuietZone:  1,
		})
	}
}

// handleShareAuthError clears cached credentials and prints the standard
// re-auth message to stderr when a share-related call returned 401.
// It does NOT exit; callers decide whether to exit immediately or fall through
// (e.g. so a subsequent generic "Error: %v" line still gets printed).
func handleShareAuthError() {
	clearAuthIdentity()
	fmt.Fprintln(os.Stderr, "Auth token rejected by server; cleared local credentials. Run 'crit auth login' to re-authenticate.")
}

func runShareExisting(existingCfg CritJSON, critPath string, files []shareFile, sharePaths []string, authToken, fallbackAuthor string, showQR bool) {
	localIDs := buildLocalIDSet(existingCfg)
	localFingerprints, localFingerprintIDs := buildLocalFingerprintIndex(existingCfg)
	if fetched, err := fetchWebComments(existingCfg.ShareURL, localIDs, localFingerprints, localFingerprintIDs, authToken); err != nil {
		if errors.Is(err, errShareUnauthorized) {
			handleShareAuthError()
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "warning: could not pull remote comments: %v\n", err)
	} else if len(fetched.NewComments) > 0 || len(fetched.ReplyUpdates) > 0 {
		if err := mergeWebComments(critPath, fetched.NewComments, fetched.ReplyUpdates); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not merge remote comments: %v\n", err)
		}
	}

	allComments, _ := loadCommentsForShare(critPath, sharePaths, fallbackAuthor)

	result, err := upsertShareToWeb(existingCfg, files, allComments, authToken)
	if err != nil {
		if errors.Is(err, errShareUnauthorized) {
			handleShareAuthError()
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := updateShareState(critPath, computeShareHash(files, allComments), result.ReviewRound); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state: %v\n", err)
	}
	if result.Changed {
		fmt.Printf("Updated (round %d): %s\n", result.ReviewRound, result.URL)
	} else {
		fmt.Println(existingCfg.ShareURL)
	}

	printQR(result.URL, showQR)
}

func runShareNew(critPath string, files []shareFile, filePaths []string, svcURL, authToken, fallbackAuthor, org, visibility string, showQR bool) {
	res, err := shareReviewFiles(critPath, files, filePaths, svcURL, authToken, fallbackAuthor, org, visibility, "")
	if err != nil {
		if errors.Is(err, errShareUnauthorized) {
			handleShareAuthError()
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := persistShareState(critPath, res.URL, res.DeleteToken, shareScope(filePaths), org, "", visibility); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state to review file: %v\n", err)
	}

	initialComments, _ := loadCommentsForShare(critPath, filePaths, fallbackAuthor)
	_ = updateShareState(critPath, computeShareHash(files, initialComments), res.ReviewRound)

	fmt.Println(res.URL)
	printQR(res.URL, showQR)

	if authToken == "" {
		showLoginHint()
	}
}

// promptShareConsent prints the first-time consent message to out and reads the
// user's answer from in. Returns true only if the user typed "y".
func promptShareConsent(out io.Writer, in io.Reader) bool {
	fmt.Fprintln(out, "  Your review will be securely uploaded to crit.md.")
	fmt.Fprintln(out, "  You'll get a private link — share it with whoever you choose.")
	fmt.Fprintln(out, "  You won't be asked again after confirming.")
	fmt.Fprint(out, "\n  Continue? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(strings.ToLower(answer)) == "y"
}

// promptShareURLConfirm asks the user to confirm sharing to a custom URL.
func promptShareURLConfirm(out io.Writer, in io.Reader, shareURL string) bool {
	fmt.Fprintf(out, "  Sharing to %s — continue? [y/N] ", shareURL)
	answer, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(strings.ToLower(answer)) == "y"
}

func runShare(args []string) {
	sf := parseShareFlags(args)

	if sf.preview != "" {
		runSharePreview(sf)
		return
	}

	if len(sf.files) == 0 {
		printShareUsage()
	}

	flagURL := sf.svcURL != ""

	cfg := loadShareConfig()
	sf.svcURL = resolveShareURL(sf.svcURL, cfg, defaultShareURL)
	cfg.AuthToken = resolveAuthToken(cfg)
	// If we have a token but no cached user id, fetch it from /api/auth/whoami
	// before building the share payload so authenticated comments carry the
	// user id. Best-effort: failures fall through to anonymous attribution.
	lazyBackfillAuthUserID(&cfg, sf.svcURL)
	authToken := cfg.AuthToken

	files := loadShareFiles(sf.files)

	critPath, err := resolveReviewPath(sf.outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := checkShareAllowed(critPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	sharePaths := make([]string, len(files))
	for i, f := range files {
		sharePaths[i] = f.Path
	}

	existingCfg, ok, err := loadExistingShareCfg(critPath, sharePaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// First-time consent gate: only for the default service, only for new shares
	if !ok && needsShareConsent(cfg, sf.svcURL) {
		if !promptShareConsent(os.Stderr, os.Stdin) {
			return
		}
		if err := saveGlobalConfig(func(m map[string]json.RawMessage) error {
			m["share_consented"] = json.RawMessage("true")
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not save consent: %v\n", err)
		}
		cfg.ShareConsented = true
	}
	// Confirm sharing to a custom URL — but only in genuine interactive
	// sessions. In non-interactive contexts (CI, the integration suite,
	// scripted/agent usage) there is no TTY, so the prompt would read EOF,
	// abort, and the share would never persist its result to the review file.
	// Auto-proceed there; keep the confirm for real terminals.
	if flagURL && term.IsTerminal(int(os.Stdin.Fd())) {
		if !promptShareURLConfirm(os.Stderr, os.Stdin, sf.svcURL) {
			return
		}
	}
	if ok {
		runShareExisting(existingCfg, critPath, files, sharePaths, authToken, cfg.Author, sf.showQR)
		return
	}

	runShareNew(critPath, files, sharePaths, sf.svcURL, authToken, cfg.Author, sf.org, sf.visibility, sf.showQR)
}

func parseFetchOutputDir(args []string) string {
	outputDir := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			outputDir = args[i]
		default:
			fmt.Fprintln(os.Stderr, "Usage: crit fetch [--output <dir>]")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Fetches comments added on crit-web into the review file.")
			fmt.Fprintln(os.Stderr, "Requires a prior `crit share` so a share URL is recorded.")
			os.Exit(1)
		}
	}
	return outputDir
}

func printFetchedComments(webComments []webComment) {
	fmt.Printf("Fetched %d new comment(s) into review file\n", len(webComments))
	for _, wc := range webComments {
		runes := []rune(wc.Body)
		body := wc.Body
		if len(runes) > 60 {
			body = string(runes[:60]) + "..."
		}
		if wc.Scope == "review" || wc.FilePath == "" {
			fmt.Printf("  [review] %s\n", body)
		} else {
			fmt.Printf("  [%s:%d] %s\n", wc.FilePath, wc.StartLine, body)
		}
	}
}

func runFetch(args []string) {
	outputDir := parseFetchOutputDir(args)

	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	data, readErr := readFileShared(reviewPathsFor(critPath).Review)
	if readErr != nil {
		fmt.Fprintln(os.Stderr, "Error: no review file found. Run `crit share` first.")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid review file: %v\n", err)
		os.Exit(1)
	}
	if cj.ShareURL == "" {
		fmt.Fprintln(os.Stderr, "Error: no share URL in review file. Run `crit share` first.")
		os.Exit(1)
	}

	authToken := resolveAuthToken(loadShareConfig())
	localIDs := buildLocalIDSet(cj)
	localFingerprints, localFingerprintIDs := buildLocalFingerprintIndex(cj)

	fetched, err := fetchWebComments(cj.ShareURL, localIDs, localFingerprints, localFingerprintIDs, authToken)
	if err != nil {
		if errors.Is(err, errShareUnauthorized) {
			handleShareAuthError()
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error fetching remote comments: %v\n", err)
		os.Exit(1)
	}

	if len(fetched.NewComments) == 0 && len(fetched.ReplyUpdates) == 0 {
		fmt.Println("No new comments.")
		fmt.Printf("Review file: %s\n", reviewPathsFor(critPath).Review)
		return
	}

	if err := mergeWebComments(critPath, fetched.NewComments, fetched.ReplyUpdates); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving review file: %v\n", err)
		os.Exit(1)
	}

	printFetchedComments(fetched.NewComments)
	if len(fetched.ReplyUpdates) > 0 {
		replyCount := 0
		for _, replies := range fetched.ReplyUpdates {
			replyCount += len(replies)
		}
		fmt.Printf("Updated %d comment(s) with %d new reply(ies).\n", len(fetched.ReplyUpdates), replyCount)
	}
	fmt.Printf("Review file: %s\n", reviewPathsFor(critPath).Review)
}

func runUnpublish(args []string) {
	unpubOutputDir := ""
	unpubSvcURL := ""
	var unpubFiles []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			unpubOutputDir = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --share-url requires a value\n")
				os.Exit(1)
			}
			i++
			unpubSvcURL = args[i]
		default:
			unpubFiles = append(unpubFiles, arg)
		}
	}

	unpubCfg := loadShareConfig()
	unpubSvcURL = resolveShareURL(unpubSvcURL, unpubCfg, defaultShareURL)
	unpubAuthToken := resolveAuthToken(unpubCfg)

	critPath, err := resolveReviewPathWithArgs(unpubOutputDir, unpubFiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	data, err := readFileShared(reviewPathsFor(critPath).Review)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: no review file found. Nothing to unpublish.")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid review file: %v\n", err)
		os.Exit(1)
	}
	if cj.DeleteToken == "" {
		fmt.Fprintln(os.Stderr, "No shared review found — nothing to unpublish.")
		return
	}

	if err := unpublishFromWeb(unpubSvcURL, cj.DeleteToken, unpubAuthToken); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Clear share state from the review file
	if err := clearShareState(critPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clear share state: %v\n", err)
	}

	fmt.Println("Review unpublished.")
}

func runConfig(args []string) {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			printConfigHelp()
			return
		}
		if arg == "--generate" || arg == "-g" {
			fmt.Print(defaultConfig().String())
			return
		}
	}
	configDir := ""
	if vcs := DetectVCS(""); vcs != nil {
		configDir, _ = vcs.RepoRoot()
	}
	if configDir == "" {
		configDir = mustGetwd()
	}
	cfg := LoadConfig(configDir)
	fmt.Print(cfg.String())
}

type pullFlags struct {
	prFlag    int
	outputDir string
}

func parsePullFlags(args []string) pullFlags {
	var f pullFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			f.outputDir = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit pull [--output <dir>] [pr-number]\n")
			os.Exit(1)
		}
		f.prFlag = n
	}
	return f
}

// resolvePullScope picks the (HeadSHA, DiffScope) pair stamped on imported
// GitHub PR comments. Per spec §E "crit pull interaction": pulled comments
// always anchor to the PR's actual diff, so DiffScope is always "layer". The
// HeadSHA is best-effort: a running range-mode daemon's HeadSHA wins;
// otherwise the on-disk ActiveDiffScope tells us a range mode is active but
// HeadSHA is unknown. When neither indicates range mode, scope is empty
// (legacy working-tree behavior — comments stay visible in working-tree view).
func resolvePullScope(cj *CritJSON) inheritedScope {
	if focus := probeDaemonFocus(); focus != nil && focus.Kind == FocusRange {
		return inheritedScope{HeadSHA: focus.HeadSHA, DiffScope: "layer"}
	}
	if cj != nil && cj.ActiveDiffScope != "" {
		return inheritedScope{DiffScope: "layer"}
	}
	return inheritedScope{}
}

// redirectReviewPathForPR detects when the cwd-resolved review file is for a
// different branch than the explicit PR the user named (or the cwd review file
// is missing entirely), and redirects to a review file matching the PR's head
// branch when exactly one such file exists. Returns ok=false silently when no
// PRInfo can be fetched, the PR's branch already matches cwd, or no unique alt
// file exists — caller falls back to cwd path.
//
// When cwdBranch is empty (no cwd review file existed), the cwd-vs-PR-branch
// early-out is skipped: there's no cwd state to false-positive against, so a
// unique branch-matching alt file is the user's intent.
//
// When multiple review files match the PR's branch, this logs a Note to stderr
// (so multi-worktree users see why the cwd file was used) and returns false.
//
// This mirrors PR #424's findReviewFileByCommentID fallback for `crit comment`:
// the user's intent is encoded in the PR number, so when cwd resolves to an
// unrelated review file we should try to honor the explicit intent first.
func redirectReviewPathForPR(prNumber int, cwdBranch, cwdCritPath string) (string, CritJSON, bool) {
	info, err := fetchPRByNumber(prNumber)
	if err != nil || info == nil || info.HeadRefName == "" {
		return "", CritJSON{}, false
	}
	// Skip early-out when cwdBranch is empty (no cwd review file existed) —
	// there's no cwd state that could false-positive a match.
	if cwdBranch != "" && info.HeadRefName == cwdBranch {
		return "", CritJSON{}, false
	}
	altPath, err := findReviewFileByBranch(info.HeadRefName, cwdCritPath)
	if err != nil {
		if errors.Is(err, errReviewFileAmbiguousForBranch) {
			fmt.Fprintf(os.Stderr,
				"Note: multiple review files match branch %q; using cwd-resolved path. Pass --output to disambiguate.\n",
				info.HeadRefName)
		}
		return "", CritJSON{}, false
	}
	altCJ, err := loadCritJSON(altPath)
	if err != nil {
		return "", CritJSON{}, false
	}
	fmt.Fprintf(os.Stderr, "Note: PR #%d targets branch %q; routing to %s (not the cwd-resolved review file)\n",
		prNumber, info.HeadRefName, filepath.Base(altPath))
	return altPath, altCJ, true
}

func runPull(args []string) { //nolint:gocyclo // CLI dispatcher: branches for arg/flag parsing, file load, scope resolution, and live-review guard
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	f := parsePullFlags(args)

	prNumber, err := detectPR(f.prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// `crit pull` is the user's explicit refresh signal — drop any cached
	// metadata for this PR so a daemon already running on it sees fresh
	// title/body/head_sha on the next focus resolution.
	invalidatePRCache(prNumber)

	ghComments, err := fetchPRComments(prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Thread-resolved state lives on the GraphQL reviewThreads edge, not on
	// REST /pulls/{n}/comments. We fetch it best-effort: a GraphQL failure
	// (token scope, transient outage) shouldn't block a comment pull. See #453.
	threadResolved, threadErr := fetchPRThreadResolved(prNumber)
	if threadErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch review-thread resolution state: %v\n", threadErr)
		threadResolved = nil
	}

	critPath, err := resolveReviewPath(f.outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var cj CritJSON
	if data, readErr := readFileShared(reviewPathsFor(critPath).Review); readErr == nil {
		if jsonErr := json.Unmarshal(data, &cj); jsonErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: existing review file is invalid, starting fresh: %v\n", jsonErr)
		}
	}

	// Redirect when the user passed an explicit PR number and the cwd-resolved
	// review file is for a different branch (or doesn't exist, or unmarshalled
	// empty above) — same class of cwd-vs-intent mismatch that PR #424 fixed
	// for `crit comment`. cj.Branch is "" when the cwd file was missing or
	// corrupt; the helper treats that as "no cwd state to false-positive
	// against" and still attempts a branch-based match.
	if f.prFlag != 0 && f.outputDir == "" {
		if altPath, altCJ, ok := redirectReviewPathForPR(prNumber, cj.Branch, critPath); ok {
			critPath = altPath
			cj = altCJ
		}
	}

	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
		cj.Branch = CurrentBranch()
		cfg := LoadConfig("")
		base := cfg.BaseBranch
		if base == "" {
			base = defaultBaseRef()
		}
		cj.BaseRef, _ = MergeBase(base)
		cj.ReviewRound = 1
	}

	if err := checkGitHubSyncAllowed(cj, "crit pull"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		os.Exit(1)
	}

	scope := resolvePullScope(&cj)
	added := mergeGHCommentsScoped(&cj, ghComments, scope, threadResolved)

	if added == 0 {
		fmt.Printf("No new inline comments found on PR #%d\n", prNumber)
		return
	}

	if err := saveCritJSON(critPath, cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Pulled %d comments from PR #%d into %s\n", added, prNumber, critPath)
	fmt.Println("Run 'crit' to view them in the browser.")
}

type pushFlags struct {
	prFlag    int
	dryRun    bool
	message   string
	outputDir string
	eventFlag string
}

func parsePushFlags(args []string) pushFlags {
	var f pushFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--dry-run" {
			f.dryRun = true
			continue
		}
		if arg == "--message" || arg == "-m" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --message requires a value\n")
				os.Exit(1)
			}
			i++
			f.message = args[i]
			continue
		}
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --output requires a value\n")
				os.Exit(1)
			}
			i++
			f.outputDir = args[i]
			continue
		}
		if arg == "--event" || arg == "-e" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --event requires a value (comment, approve, request-changes)\n")
				os.Exit(1)
			}
			i++
			f.eventFlag = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit push [--dry-run] [--event <type>] [--message <msg>] [--output <dir>] [pr-number]\n")
			os.Exit(1)
		}
		f.prFlag = n
	}
	return f
}

// postPushReplies posts each reply via `gh api`. On the first auth-rotation
// failure (HTTP 401) it aborts the rest of the batch — every subsequent
// call would fail identically, and bailing cleanly lets the outer push
// loop print the K-of-N recovery message. authFailed signals that to the
// caller. replyCount is the number of replies actually accepted by GitHub
// before the abort (or in total if no abort happened).
func postPushReplies(prNumber int, allReplies []ghReplyForPush) (map[replyKey]int64, int, bool) {
	replyCount := 0
	replyIDs := make(map[replyKey]int64)
	authFailed := false
	for _, reply := range allReplies {
		replyID, err := postGHReply(prNumber, reply.ParentGHID, reply.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to post reply: %v\n", err)
			if errors.Is(err, errGHAuthFailed) {
				authFailed = true
				break
			}
			continue
		}
		replyCount++
		if replyID != 0 {
			replyIDs[replyKey{ParentGHID: reply.ParentGHID, BodyPrefix: truncateStr(reply.Body, 60)}] = replyID
		}
	}
	if replyCount > 0 {
		fmt.Printf("Posted %d replies\n", replyCount)
	}
	return replyIDs, replyCount, authFailed
}

// resolveCurrentPRHead fetches the PR's current head SHA when in range mode.
// Returns "" silently when not in range mode or on tolerated fetch failure
// (dry-run); returns an error when fetching is required but failed.
//
// On dry-run with a fetch error, surfaces a stderr note so the user knows the
// stale-head check was skipped — silent skipping makes the dry-run plan
// misleading.
func resolveCurrentPRHead(prNumber int, inRange, dryRun bool) (string, error) {
	if !inRange {
		return "", nil
	}
	info, err := fetchPRByNumber(prNumber)
	if err != nil {
		if dryRun {
			fmt.Fprintf(os.Stderr,
				"Note: could not resolve current PR #%d head; stale-head check not enforced in this dry-run: %v\n",
				prNumber, err)
			return "", nil
		}
		return "", fmt.Errorf("fetching PR #%d for stale-head check: %w", prNumber, err)
	}
	if info == nil {
		return "", nil
	}
	return info.HeadRefOid, nil
}

// pushContext captures everything runPush needs after parsing flags +
// loading the review file. Splitting this out keeps runPush itself short
// and the cyclomatic complexity inside Go Report Card limits.
type pushContext struct {
	flags    pushFlags
	event    string
	prNumber int
	critPath string
	cj       CritJSON
}

// loadPushContext parses flags, validates them, resolves the PR number, and
// reads + parses the review file. Exits the process on any error since this
// runs at the top of a CLI subcommand.
func loadPushContext(args []string) pushContext {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	f := parsePushFlags(args)

	event, err := parsePushEvent(f.eventFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if event == "REQUEST_CHANGES" && f.message == "" {
		fmt.Fprintf(os.Stderr, "Error: --event request-changes requires --message\n")
		os.Exit(1)
	}

	prNumber, err := detectPR(f.prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	critPath, err := resolveReviewPath(f.outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Read the cwd-resolved file first (best-effort) so we know its branch.
	// We tolerate "not found" here so an explicit `--pr N` from a clean
	// checkout can still find the right file by branch via the redirect.
	var cj CritJSON
	cwdFileExists := true
	data, readErr := readFileShared(reviewPathsFor(critPath).Review)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", readErr)
			os.Exit(1)
		}
		cwdFileExists = false
	} else if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid review file: %v\n", err)
		os.Exit(1)
	}

	// Redirect when the user passed an explicit PR number and the cwd-resolved
	// review file is for a different branch (or is missing) — pushing the wrong
	// comments to a PR is destructive, so honor the explicit intent first. Same
	// pattern as PR #424's findReviewFileByCommentID fallback for `crit comment`.
	if f.prFlag != 0 && f.outputDir == "" {
		if altPath, altCJ, ok := redirectReviewPathForPR(prNumber, cj.Branch, critPath); ok {
			critPath = altPath
			cj = altCJ
			cwdFileExists = true
		}
	}

	if !cwdFileExists {
		fmt.Fprintf(os.Stderr, "Error: no review file found. Run a crit review first.\n")
		os.Exit(1)
	}

	return pushContext{flags: f, event: event, prNumber: prNumber, critPath: critPath, cj: cj}
}

// runPushDryRun prints the bucket plan to stdout and returns. Does not write
// the export file — dry-run is read-only by definition.
func runPushDryRun(ctx pushContext, b pushBuckets) {
	fmt.Println(summarizeBuckets(ctx.prNumber, b))
	fmt.Println()
	fmt.Print(detailedDryRun(b))
	fmt.Printf("Use `crit push --pr %d` to confirm.\n", ctx.prNumber)
}

// runPushLive performs the actual push: writes the orphan export (if any
// orphans), posts the postable bucket via gh, and prints a summary. Replies
// to existing GitHub comments are also posted (only for postable parents).
//
// Returns the process exit code so callers (and tests) can decide what to
// do without runPushLive itself terminating the process.
func runPushLive(ctx pushContext, b pushBuckets) int {
	exportPath := writePushOrphanExport(ctx, b)

	rewrite := stripBodyRewriter

	postable := len(b.Postable)
	posted, postFailed, postAuthFailed, commentIDs := runPushPostReview(ctx, b, rewrite)

	totalReplies := countNewReplies(ctx.cj)
	postedReplies := 0
	replyAuthFailed := false
	if !postFailed && !postAuthFailed {
		var allReplies []ghReplyForPush
		for _, cf := range ctx.cj.Files {
			allReplies = append(allReplies, collectNewRepliesForPush(cf, rewrite)...)
		}
		var replyIDs map[replyKey]int64
		replyIDs, postedReplies, replyAuthFailed = postPushReplies(ctx.prNumber, allReplies)
		if len(commentIDs) > 0 || len(replyIDs) > 0 {
			if uerr := updateCritJSONWithGitHubIDs(ctx.critPath, commentIDs, replyIDs); uerr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update review file with GitHub IDs: %v\n", uerr)
			}
		}
	}

	totalEdits := len(collectEditedForPush(ctx.cj))
	patched := 0
	editAuthFailed := false
	if !postAuthFailed && !replyAuthFailed {
		patched, editAuthFailed = pushEditedBodies(ctx)
	}

	totalDeletes := len(collectDeletesForPush(ctx.cj))
	deleted := 0
	deleteFailed := false
	deleteAuthFailed := false
	if !postAuthFailed && !replyAuthFailed && !editAuthFailed {
		deleted, deleteFailed, deleteAuthFailed = pushDeletedComments(ctx)
	}

	authAborted := postAuthFailed || replyAuthFailed || editAuthFailed || deleteAuthFailed
	if authAborted {
		k := posted + postedReplies + patched + deleted
		n := postable + totalReplies + totalEdits + totalDeletes
		fmt.Fprintf(os.Stderr,
			"Pushed %d of %d comments before auth failed. Run 'gh auth refresh' then re-run 'crit push' to post the rest.\n",
			k, n)
		return 1
	}

	printPushSummary(posted, patched, deleted, len(b.FullStack)+len(b.Unmapped), exportPath)

	if pushShouldExitFailure(posted, patched, deleted, exportPath, postFailed, deleteFailed) {
		return 1
	}
	return 0
}

// writePushOrphanExport writes the off-PR (full-stack + unmapped) bucket
// to a side file when needed. Split out of runPushLive purely to keep
// cyclomatic complexity inside Go Report Card limits.
func writePushOrphanExport(ctx pushContext, b pushBuckets) string {
	if len(b.FullStack)+len(b.Unmapped) == 0 {
		return ""
	}
	dir, err := exportsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve export dir: %v\n", err)
		return ""
	}
	path, werr := writeOrphanExport(ctx.prNumber, b, dir)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write orphan export: %v\n", werr)
		return ""
	}
	return path
}

// runPushPostReview posts the Postable bucket as a single GitHub review.
// rewrite preprocesses each comment body before it ships (image upload+swap
// in production; nil falls back to strip-with-placeholder).
// Returns the count posted, whether the call failed (any reason), whether
// the failure was specifically an auth-rotation 401, and the path-to-id
// mapping for body-hash bookkeeping.
func runPushPostReview(ctx pushContext, b pushBuckets, rewrite bodyRewriter) (int, bool, bool, map[string]int64) {
	if len(b.Postable) == 0 {
		return 0, false, false, nil
	}
	ghComments := bucketsToGHComments(b.Postable, rewrite)
	ids, err := createGHReview(ctx.prNumber, ghComments, ctx.flags.message, ctx.event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error posting review: %v\n", err)
		return 0, true, errors.Is(err, errGHAuthFailed), nil
	}
	return len(ghComments), false, false, ids
}

// countNewReplies counts replies that would be sent in this push (no
// GitHubID yet, parent already on GitHub). Mirrors collectNewRepliesForPush
// without allocating the full slice — used purely for the K-of-N total.
// nil rewriter is fine: the body content does not affect the count.
func countNewReplies(cj CritJSON) int {
	n := 0
	for _, cf := range cj.Files {
		n += len(collectNewRepliesForPush(cf, nil))
	}
	return n
}

// pushShouldExitFailure encodes the exit-code policy for `crit push`. The
// process should fail (exit 1) only when nothing meaningful landed and at
// least one operation failed. Failed per-ID deletes stay in
// PendingGitHubDeletes for the next push (existing retry semantics), so a
// partial delete failure must not mask successful posts/patches/drains.
func pushShouldExitFailure(posted, patched, deleted int, exportPath string, postFailed, deleteFailed bool) bool {
	anySuccess := posted > 0 || patched > 0 || deleted > 0 || exportPath != ""
	anyFailure := postFailed || deleteFailed
	return anyFailure && !anySuccess
}

// printPushSummary writes the one-line stdout summary describing what
// happened. Adapts wording to the actual outcome (no orphans, no posts, etc).
func printPushSummary(posted, patched, deleted, orphans int, exportPath string) {
	if posted == 0 && patched == 0 && deleted == 0 && orphans == 0 {
		fmt.Println("No comments to push.")
		return
	}
	parts := []string{fmt.Sprintf("Posted %d comments", posted)}
	if patched > 0 {
		parts = append(parts, fmt.Sprintf("edited %d", patched))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d", deleted))
	}
	line := strings.Join(parts, ", ") + "."
	if exportPath != "" {
		line += fmt.Sprintf(" %d comments exported to %s.", orphans, exportPath)
	}
	fmt.Println(line)
}

// pushEditedBodies PATCHes already-pushed comments/replies whose local body
// diverged from the recorded push-time hash. Returns the count of records
// successfully PATCHed and updated in the review file, plus an authFailed
// flag when an HTTP 401 aborted the batch. Non-auth failures log to stderr
// and are excluded from the count, so the next push will retry them.
func pushEditedBodies(ctx pushContext) (int, bool) {
	edits := collectEditedForPush(ctx.cj)
	if len(edits) == 0 {
		return 0, false
	}
	succeeded := make([]ghEditForPush, 0, len(edits))
	authFailed := false
	for _, e := range edits {
		if err := patchGHComment(e.GitHubID, e.Body); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to edit comment %d: %v\n", e.GitHubID, err)
			if errors.Is(err, errGHAuthFailed) {
				authFailed = true
				break
			}
			continue
		}
		succeeded = append(succeeded, e)
	}
	if uerr := updateCritJSONWithEditedBodies(ctx.critPath, succeeded); uerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update review file after edit push: %v\n", uerr)
	}
	return len(succeeded), authFailed
}

// pushDeletedComments issues DELETE for every GitHub comment ID queued in
// PendingGitHubDeletes. Returns the count of IDs whose DELETE was drained
// (200 / 204, plus 404 "already gone" and 403 "not the author") and whether
// any DELETE returned an error severe enough to surface a non-zero exit.
//
// 403 is logged but treated as drained — the GitHub API rejects deletes by
// non-authors, so retrying is futile and a stuck pending entry would block
// all future pushes for this review file.
func pushDeletedComments(ctx pushContext) (int, bool, bool) {
	pending := collectDeletesForPush(ctx.cj)
	if len(pending) == 0 {
		return 0, false, false
	}
	drained := make([]int64, 0, len(pending))
	failed := false
	authFailed := false
	for _, id := range pending {
		status, err := deleteGHComment(id)
		switch {
		case err != nil && errors.Is(err, errGHAuthFailed):
			fmt.Fprintf(os.Stderr, "Warning: failed to delete comment %d: %v\n", id, err)
			authFailed = true
		case err != nil:
			fmt.Fprintf(os.Stderr, "Warning: failed to delete comment %d: %v\n", id, err)
			failed = true
		case status >= 200 && status < 300, status == 404:
			drained = append(drained, id)
		case status == 403:
			fmt.Fprintf(os.Stderr, "Warning: cannot delete comment %d on GitHub (403; not the author) — dropping pending delete\n", id)
			drained = append(drained, id)
		default:
			fmt.Fprintf(os.Stderr, "Warning: unexpected status %d deleting comment %d\n", status, id)
			failed = true
		}
		if authFailed {
			break
		}
	}
	if uerr := updateCritJSONAfterDeletes(ctx.critPath, drained); uerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update review file after delete push: %v\n", uerr)
	}
	return len(drained), failed, authFailed
}

// fullStackPushGateMessage is the user-facing error string emitted when
// `crit push` is invoked while the active diff scope is the cumulative
// stack range. Comments authored under that scope carry line numbers that
// don't correspond to the PR's head diff, so the entire push is refused.
// The exact wording is asserted by test/test-diff.sh Instance 6.
const fullStackPushGateMessage = "Switch to Layer diff before posting a platform review"

// pushBlockedByFullStackScope reports whether the on-disk active diff scope
// requires `crit push` to abort with the gate message.
func pushBlockedByFullStackScope(activeScope string) bool {
	return activeScope == string(DiffScopeFullStack)
}

func runPush(args []string) {
	ctx := loadPushContext(args)

	if err := checkGitHubSyncAllowed(ctx.cj, "crit push"); err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		os.Exit(1)
	}

	// Full-stack push gate — see fullStackPushGateMessage.
	if pushBlockedByFullStackScope(ctx.cj.ActiveDiffScope) {
		fmt.Fprintln(os.Stderr, "Error: "+fullStackPushGateMessage)
		os.Exit(1)
	}

	inRange := ctx.cj.ActiveDiffScope != ""
	currentHead, err := resolveCurrentPRHead(ctx.prNumber, inRange, ctx.flags.dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	b := bucketCommentsForPush(ctx.cj, currentHead, inRange)

	if ctx.flags.dryRun {
		runPushDryRun(ctx, b)
		return
	}
	if code := runPushLive(ctx, b); code != 0 {
		os.Exit(code)
	}
}

type commentFlags struct {
	outputDir string
	author    string
	userID    string
	replyTo   string
	resolve   bool
	path      string
	json      bool
	file      string
	plan      string
	scope     commentFocusOverride
	args      []string
}

// requireFlagValue extracts the value following a flag at position i, exiting
// with an error message when the value is missing.
func requireFlagValue(args []string, i int, flag string) string {
	if i+1 >= len(args) {
		fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", flag)
		os.Exit(1)
	}
	return args[i+1]
}

func parseCommentFlags(args []string) commentFlags {
	var f commentFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--plan":
			f.plan = requireFlagValue(args, i, "--plan")
			i++
		case "--output", "-o":
			f.outputDir = requireFlagValue(args, i, arg)
			i++
		case "--author":
			f.author = requireFlagValue(args, i, "--author")
			i++
		case "--reply-to":
			f.replyTo = requireFlagValue(args, i, "--reply-to")
			i++
		case "--resolve":
			f.resolve = true
		case "--path":
			f.path = requireFlagValue(args, i, "--path")
			i++
		case "--json":
			f.json = true
		case "--file", "-f":
			f.file = requireFlagValue(args, i, arg)
			i++
		case "--scope":
			override, err := commentScopeOverrideFromFlag(requireFlagValue(args, i, "--scope"))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			f.scope = override
			i++
		default:
			f.args = append(f.args, arg)
		}
	}
	return f
}

func resolveCommentFlags(f *commentFlags) {
	// --plan resolves to --output for the plan storage directory
	if f.plan != "" {
		if f.outputDir != "" {
			fmt.Fprintln(os.Stderr, "Error: --plan and --output cannot be used together")
			os.Exit(1)
		}
		var planDirErr error
		f.outputDir, planDirErr = planStorageDir(slugify(f.plan))
		if planDirErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", planDirErr)
			os.Exit(1)
		}
	}

	// Resolve author: --author flag > config > VCS user.name.
	// Stamp AuthUserID alongside the author so authenticated comments
	// carry the user identity into the share payload.
	cfgDir := mustGetwd()
	if vcs := DetectVCS(""); vcs != nil {
		if root, err := vcs.RepoRoot(); err == nil {
			cfgDir = root
		}
	}
	cfg := LoadConfig(cfgDir)
	if f.author == "" {
		f.author = cfg.Author
	}
	if f.userID == "" {
		f.userID = cfg.AuthUserID
	}
}

// readCommentJSONInput reads bulk-comment JSON either from the given file path
// or from stdin. A path of "" or "-" reads stdin (POSIX convention). Errors
// include the source path so callers can spot file vs. stdin failures quickly.
func readCommentJSONInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "" || path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

// parseCommentJSONEntries unmarshals a bulk-comment JSON array, returning a
// readable, located error message when the input is malformed. The source
// label ("-" for stdin, or the file path) is included in the error so callers
// can tell file vs. stdin failures apart.
func parseCommentJSONEntries(data []byte, source string) ([]BulkCommentEntry, error) {
	var entries []BulkCommentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, formatJSONParseError(data, source, err)
	}
	return entries, nil
}

// formatJSONParseError builds a multi-line error describing where in the input
// JSON parsing failed: byte offset, line/column, a snippet around the offset
// with control characters made visible, and the original error message. The
// source label identifies the input ("-" for stdin, otherwise a file path).
func formatJSONParseError(data []byte, source string, err error) error {
	label := jsonSourceLabel(source)
	offset, hasOffset := jsonErrorOffset(err)
	if !hasOffset {
		return fmt.Errorf("Error parsing JSON from %s: %w", label, err)
	}
	line, col := lineColForOffset(data, offset)
	snippet := jsonSnippet(data, offset)
	return fmt.Errorf("Error parsing JSON from %s at byte %d (line %d, column %d):\n  %s\n  %w",
		label, offset, line, col, snippet, err)
}

// jsonSourceLabel renders a human-readable label for the JSON input source.
// Empty or "-" means stdin; anything else is treated as a file path.
func jsonSourceLabel(source string) string {
	if source == "" || source == "-" {
		return "stdin"
	}
	return source
}

// jsonErrorOffset extracts the byte offset from json package errors that carry
// one. Returns (0, false) if the error has no offset information.
func jsonErrorOffset(err error) (int64, bool) {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return syn.Offset, true
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		return typ.Offset, true
	}
	return 0, false
}

// lineColForOffset returns 1-based line and column for the given byte offset
// into data. Tolerates offsets at or past the end of input.
func lineColForOffset(data []byte, offset int64) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if int(offset) > len(data) {
		offset = int64(len(data))
	}
	line, col := 1, 1
	for i := int64(0); i < offset; i++ {
		if data[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

// jsonSnippet returns a single-line excerpt of data around offset with the
// offending position marked by >>>HERE<<<. Control bytes inside the snippet
// are rendered as their visible escape forms so e.g. a raw newline appears as
// the two characters \n rather than wrapping the output.
func jsonSnippet(data []byte, offset int64) string {
	const before, after = 40, 40
	if offset < 0 {
		offset = 0
	}
	if int(offset) > len(data) {
		offset = int64(len(data))
	}
	start := offset - before
	if start < 0 {
		start = 0
	}
	end := offset + after
	if int(end) > len(data) {
		end = int64(len(data))
	}
	left := visibleControl(data[start:offset])
	right := visibleControl(data[offset:end])
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if int(end) < len(data) {
		suffix = "..."
	}
	return prefix + left + ">>>HERE<<<" + right + suffix
}

// visibleControl replaces unprintable control bytes (newline, carriage return,
// tab) with their escape-sequence representation so they survive a single-line
// terminal print.
func visibleControl(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for _, c := range b {
		switch c {
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

func runCommentJSON(f commentFlags) {
	runCommentJSONScoped(f, inheritedScope{})
}

func runCommentJSONScoped(f commentFlags, scope inheritedScope) {
	data, err := readCommentJSONInput(f.file, os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	entries, err := parseCommentJSONEntries(data, f.file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := bulkAddCommentsToCritJSONScoped(entries, f.author, f.userID, f.outputDir, scope); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var comments, replies int
	for _, e := range entries {
		if e.ReplyTo != "" {
			replies++
		} else {
			comments++
		}
	}

	var parts []string
	if comments > 0 {
		parts = append(parts, fmt.Sprintf("%d comment%s", comments, plural(comments)))
	}
	if replies > 0 {
		word := "replies"
		if replies == 1 {
			word = "reply"
		}
		parts = append(parts, fmt.Sprintf("%d %s", replies, word))
	}
	fmt.Printf("Added %s\n", strings.Join(parts, " and "))
}

func runCommentReply(f commentFlags) {
	if len(f.args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: crit comment --reply-to <comment-id> [--resolve] <body>")
		os.Exit(1)
	}
	replyBody := strings.Join(f.args, " ")
	if err := addReplyToCritJSON(f.replyTo, replyBody, f.author, f.userID, f.resolve, f.outputDir, f.path); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if f.resolve {
		fmt.Printf("Replied to %s and marked resolved\n", f.replyTo)
	} else {
		fmt.Printf("Replied to %s\n", f.replyTo)
	}
}

func runCommentClear(outputDir string) {
	if err := clearCritJSON(outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Cleared review file")
}

func printCommentUsage() {
	fmt.Fprintln(os.Stderr, "Usage: crit comment [--output <dir>] [--author <name>] <body>                    Review-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] [--author <name>] <path> <body>             File-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] [--author <name>] <path>:<line[-end]> <body> Line-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment --reply-to <id> [--resolve] [--author <name>] <body>")
	fmt.Fprintln(os.Stderr, "       crit comment --json [--file <path>] [--author <name>] [--output <dir>]")
	fmt.Fprintln(os.Stderr, "                                                                  Bulk add comments from JSON (stdin by default; --file <path> or --file - for stdin)")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] --clear")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' 'Overall this looks good'")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' src/auth.go 'Restructure this file'")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' main.go:42 'Fix this bug'")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' src/auth.go:10-25 'This block needs refactoring'")
	fmt.Fprintln(os.Stderr, "  crit comment --reply-to c_a3f8b2 --resolve --author 'Claude' 'Split into two functions'")
	fmt.Fprintln(os.Stderr, "  crit comment --output /tmp/reviews main.go:42 'Fix this bug'")
	fmt.Fprintln(os.Stderr, "  echo '[{\"file\":\"main.go\",\"line\":42,\"body\":\"Fix this\"}]' | crit comment --json --author 'Claude'")
	fmt.Fprintln(os.Stderr, "  crit comment --json --file comments.json --author 'Claude'")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Tips:")
	fmt.Fprintln(os.Stderr, "  Use --author to identify who left the comment (recommended for AI agents)")
	fmt.Fprintln(os.Stderr, "  Use single quotes for the body to avoid shell interpretation of backticks")
	fmt.Fprintln(os.Stderr, "  Use --json for bulk operations (multiple comments/replies in one atomic write)")
	os.Exit(1)
}

func runCommentLineLevel(loc string, commentArgs []string, author, userID, outputDir string) {
	runCommentLineLevelScoped(loc, commentArgs, author, userID, outputDir, inheritedScope{})
}

func runCommentLineLevelScoped(loc string, commentArgs []string, author, userID, outputDir string, scope inheritedScope) {
	colonIdx := strings.LastIndex(loc, ":")
	lineSpec := loc[colonIdx+1:]
	filePath := loc[:colonIdx]
	var startLine, endLine int
	if dashIdx := strings.Index(lineSpec, "-"); dashIdx >= 0 {
		s, err := strconv.Atoi(lineSpec[:dashIdx])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid start line in %q\n", loc)
			os.Exit(1)
		}
		e, err := strconv.Atoi(lineSpec[dashIdx+1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid end line in %q\n", loc)
			os.Exit(1)
		}
		startLine, endLine = s, e
	} else {
		n, err := strconv.Atoi(lineSpec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid line number in %q\n", loc)
			os.Exit(1)
		}
		startLine, endLine = n, n
	}
	body := strings.Join(commentArgs[1:], " ")
	if critPath, err := resolveReviewPath(outputDir); err == nil {
		if guardErr := checkCommentCLIAllowed(critPath); guardErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", guardErr)
			os.Exit(1)
		}
	}
	if err := addCommentToCritJSONScoped(filePath, startLine, endLine, body, author, userID, outputDir, scope); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added comment on %s:%s\n", filePath, lineSpec)
}

func runComment(args []string) {
	f := parseCommentFlags(args)
	resolveCommentFlags(&f)

	scope, err := resolveCommentScope(f.scope, f.outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if f.json {
		runCommentJSONScoped(f, scope)
		return
	}

	if f.replyTo != "" {
		runCommentReply(f)
		return
	}

	if len(f.args) >= 1 && f.args[0] == "--clear" {
		runCommentClear(f.outputDir)
		return
	}

	if len(f.args) < 1 {
		printCommentUsage()
	}

	// 1 arg: review-level comment
	if len(f.args) == 1 {
		body := f.args[0]
		if err := addReviewCommentToCritJSONScoped(body, f.author, f.userID, f.outputDir, scope); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Added review comment")
		return
	}

	// 2+ args: check if first arg has a colon with valid line spec
	loc := f.args[0]
	colonIdx := strings.LastIndex(loc, ":")
	if colonIdx > 0 && looksLikeLineSpec(loc[colonIdx+1:]) {
		runCommentLineLevelScoped(loc, f.args, f.author, f.userID, f.outputDir, scope)
		return
	}

	// 2+ args without colon line spec: check if first arg is a file path
	if len(f.args) >= 2 {
		candidatePath := f.args[0]
		if fileExistsOnDiskOrSession(candidatePath, f.outputDir) {
			body := strings.Join(f.args[1:], " ")
			if err := addFileCommentToCritJSONScoped(candidatePath, body, f.author, f.userID, f.outputDir, scope); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Added file comment on %s\n", candidatePath)
			return
		}
	}

	if colonIdx < 0 {
		fmt.Fprintf(os.Stderr, "Error: invalid location %q — expected <path>:<line[-end]>, or a valid file path for file-level comments\n", loc)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Error: invalid line spec in %q\n", loc)
	os.Exit(1)
}

// looksLikeLineSpec returns true if s looks like a line number or range (e.g. "42", "10-25").
func looksLikeLineSpec(s string) bool {
	if s == "" {
		return false
	}
	if dashIdx := strings.Index(s, "-"); dashIdx >= 0 {
		_, err1 := strconv.Atoi(s[:dashIdx])
		_, err2 := strconv.Atoi(s[dashIdx+1:])
		return err1 == nil && err2 == nil
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

// fileExistsOnDiskOrSession checks if a path exists as a file on disk or in the review file.
func fileExistsOnDiskOrSession(path string, outputDir string) bool {
	// Check disk first (relative to cwd)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return true
	}
	// Check in repo root if we're in a VCS repo
	if vcs := DetectVCS(""); vcs != nil {
		if root, err := vcs.RepoRoot(); err == nil {
			absPath := filepath.Join(root, path)
			if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	// Check if it exists in the review file
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		return false
	}
	cj, err := loadCritJSON(critPath)
	if err != nil {
		return false
	}
	_, exists := cj.Files[path]
	return exists
}

// runReview always uses the daemon pattern: starts a background daemon if needed,
// connects as a review client, blocks for one review cycle, then exits.
// Used by `crit review` and by agents.
type planConfig struct {
	name          string
	filePath      string
	stdinExpected bool
	port          int
	host          string
	noOpen        bool
	quiet         bool
	shareURL      string
}

func resolvePlanConfig(args []string) planConfig {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	name := fs.String("name", "", "Plan name/slug for session identification")
	port := fs.Int("port", 0, "Port to listen on")
	fs.IntVar(port, "p", 0, "Port (shorthand)")
	host := fs.String("host", "", "Host to listen on")
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status (shorthand)")
	shareURL := fs.String("share-url", "", "Share service URL")
	fs.Parse(args)

	pc := planConfig{
		name:     *name,
		port:     *port,
		host:     *host,
		noOpen:   *noOpen,
		quiet:    *quiet,
		shareURL: *shareURL,
	}

	remaining := fs.Args()
	if len(remaining) > 0 {
		pc.filePath = remaining[0]
	} else {
		pc.stdinExpected = true
	}

	return pc
}

func readPlanContent(pc planConfig) []byte {
	var content []byte
	var err error

	if pc.filePath != "" {
		content, err = os.ReadFile(pc.filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", pc.filePath, err)
			os.Exit(1)
		}
	} else if pc.stdinExpected {
		if !isStdinPipe() {
			fmt.Fprintln(os.Stderr, "Error: no file specified and stdin is not a pipe")
			fmt.Fprintln(os.Stderr, "Usage: crit plan --name <slug> <file>  or  echo \"content\" | crit plan --name <slug>")
			os.Exit(1)
		}
		content, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
	}

	if len(strings.TrimSpace(string(content))) == 0 {
		fmt.Fprintln(os.Stderr, "Error: plan content is empty")
		os.Exit(1)
	}
	return content
}

func resolvePlanSlug(name string, content []byte) string {
	if name != "" {
		return slugify(name)
	}
	slug := resolveSlug(content)
	fmt.Fprintf(os.Stderr, "No --name provided, derived slug: %s\n", slug)
	return slug
}

// connectOrStartDaemon finds an alive session or starts a new daemon.
// Returns the session entry and whether we started a new daemon.
func connectOrStartDaemon(key string, args []string, noOpen bool) (sessionEntry, bool) {
	entry, alive := findAliveSession(key)
	if alive {
		fmt.Fprintf(os.Stderr, "Connected to crit daemon at %s\n", entry.baseURL())
		if !noOpen && !daemonHasBrowser(entry) {
			go openBrowser(entry.baseURL())
		}
		return entry, false
	}

	var err error
	entry, err = startDaemon(key, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Started crit daemon at %s (PID %d)\n", entry.baseURL(), entry.PID)
	hintMissingIntegrations()
	return entry, true
}

// hintMissingIntegrations prints a suggestion when AI tools are detected but
// no crit integration is installed. Skipped when any integration already exists
// or when CRIT_NO_INTEGRATION_CHECK is set.
func hintMissingIntegrations() {
	if os.Getenv("CRIT_NO_INTEGRATION_CHECK") != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	hintMissingIntegrationsFor(mustGetwd(), home)
}

func hintMissingIntegrationsFor(cwd, home string) {
	if len(installedAgents(cwd, home)) > 0 {
		return
	}
	if missing := checkMissingIntegrations(cwd, home); len(missing) > 0 {
		printMissingHints(missing)
	}
}

func installDaemonSignalHandler(pid int) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, terminationSignals()...)
	go func() {
		<-sigCh
		if proc, err := os.FindProcess(pid); err == nil {
			_ = terminateProcess(proc)
		}
		os.Exit(0)
	}()
}

func killDaemonOnApproval(approved bool, pid int) {
	if approved {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = terminateProcess(proc)
		}
	}
}

// backgroundCleanup silently removes stale review files and orphaned session
// files. It is intended to be called as a goroutine from review entry points
// so it adds zero perceived latency. All errors are swallowed — no output is
// written to stdout or stderr.
func backgroundCleanup() {
	revDir, err := reviewsDir()
	if err == nil {
		stale := findStaleReviews(revDir, 14)
		deleteStaleReviewsSilent(stale)
	}
	cleanOrphanedSessions()
}

// deleteStaleReviewsSilent is like deleteStaleReviews but swallows all errors.
// Used by backgroundCleanup to avoid any output to stderr.
func deleteStaleReviewsSilent(stale []staleReview) {
	sessDir, _ := sessionsDir()
	for _, s := range stale {
		if !removeStaleReviewPath(s.path) {
			continue
		}
		if sessDir != "" {
			os.Remove(filepath.Join(sessDir, s.key+".json"))
			os.Remove(filepath.Join(sessDir, s.key+".lock"))
			os.Remove(filepath.Join(sessDir, s.key+".log"))
		}
	}
}

// removeStaleReviewPath removes a review identity, supporting both the v4
// folder layout (RemoveAll) and v3 flat *.json files (Remove + sibling
// sidecar). MIGRATION-REMOVAL: the flat-file branch can be deleted once the
// migration shim is removed.
func removeStaleReviewPath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return os.RemoveAll(path) == nil
	}
	// MIGRATION-REMOVAL: v3 flat-file fallback.
	if err := os.Remove(path); err != nil {
		return false
	}
	_ = os.Remove(path + ".snapshots.json")
	return true
}

// cleanupOnApproval deletes the review folder (review.json, snapshots.json,
// and any attachments) when the review is approved and cleanup is enabled.
func cleanupOnApproval(approved bool, reviewPath string, cleanupEnabled bool) {
	if !(approved && cleanupEnabled && reviewPath != "") {
		return
	}
	// In v4 the review identity is a folder; remove it whole. The
	// MIGRATION-REMOVAL fallback handles v3 flat reviews still on disk.
	_ = removeStaleReviewPath(reviewPath)
}

func runPlan(args []string) {
	go backgroundCleanup()

	pc := resolvePlanConfig(args)
	content := readPlanContent(pc)

	slug := resolvePlanSlug(pc.name, content)
	storageDir, err := planStorageDir(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ver, err := savePlanVersion(storageDir, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error saving plan: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Plan '%s' saved as v%03d (%d bytes)\n", slug, ver, len(content))

	cwd, _ := resolvedCWD()
	key := planSessionKey(cwd, slug)
	currentPath := filepath.Join(storageDir, "current.md")
	cfg := LoadConfig(cwd)
	daemonArgs := buildPlanDaemonArgs(currentPath, storageDir, slug, commonDaemonFlags{
		port:     resolvePort(pc.port, cfg.Port),
		host:     resolveHost(pc.host, cfg.Host),
		noOpen:   pc.noOpen || cfg.NoOpen,
		quiet:    pc.quiet || cfg.Quiet,
		shareURL: resolveShareURL(pc.shareURL, cfg, ""),
	})

	entry, weStartedDaemon := connectOrStartDaemon(key, daemonArgs, pc.noOpen)

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved := runReviewClient(entry, key)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, LoadConfig(cwd).CleanupOnApproveEnabled())
}

type planHookEvent struct {
	SessionID string `json:"session_id"`
	ToolInput struct {
		Plan string `json:"plan"`
	} `json:"tool_input"`
}

type codexStopHookEvent struct {
	SessionID            string  `json:"session_id"`
	TurnID               string  `json:"turn_id"`
	TranscriptPath       *string `json:"transcript_path"`
	PermissionMode       string  `json:"permission_mode"`
	StopHookActive       bool    `json:"stop_hook_active"`
	LastAssistantMessage *string `json:"last_assistant_message"`
}

func resolveHookSlug(sessionID string, content []byte) string {
	if sessionID != "" {
		if existing, ok := lookupPlanSlug(sessionID); ok {
			return existing
		}
		slug := resolveSlug(content)
		if err := savePlanSlug(sessionID, slug); err != nil {
			fmt.Fprintf(os.Stderr, "crit plan-hook: warning: could not save slug mapping: %v\n", err)
		}
		return slug
	}
	return resolveSlug(content)
}

func emitHookDecision(approved bool, prompt string) {
	if approved {
		out, _ := json.Marshal(map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName": "PermissionRequest",
				"decision":      map[string]any{"behavior": "allow"},
			},
		})
		fmt.Println(string(out))
		return
	}

	if prompt == "" {
		prompt = "Review comments pending — address them before proceeding."
	}
	out, _ := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision": map[string]any{
				"behavior": "deny",
				"message":  prompt,
			},
		},
	})
	fmt.Println(string(out))
}

// Codex Stop hooks expose permission_mode, last_assistant_message, and
// transcript_path, but not a structured tool_input.plan like Claude Code's
// ExitPlanMode hook. Until Codex adds that payload, Crit uses its explicit
// <proposed_plan> tag as the activation signal and accepts either a closed tag
// or EOF, which matches how partial final streamed assistant messages can land
// in the Stop hook payload/transcript.
//
// References:
// - https://developers.openai.com/codex/hooks#stop
// - https://raw.githubusercontent.com/openai/codex/main/codex-rs/hooks/schema/generated/stop.command.input.schema.json
var proposedPlanBlockRE = regexp.MustCompile(`(?s)<proposed_plan>\s*(.*?)(?:\s*</proposed_plan>|$)`)

const (
	codexTranscriptInitialTokenBuffer = 64 * 1024
	codexTranscriptMaxTokenSize       = 64 * 1024 * 1024
)

func extractProposedPlan(message string) (string, bool) {
	matches := proposedPlanBlockRE.FindAllStringSubmatch(message, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if plan := strings.TrimSpace(matches[i][1]); plan != "" {
			return plan, true
		}
	}
	return "", false
}

func extractProposedPlanFromCodexTranscript(path, turnID string) (string, bool) {
	if strings.TrimSpace(turnID) == "" {
		return "", false
	}

	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook --mode codex: could not read transcript %s: %v\n", path, err)
		return "", false
	}
	defer file.Close()

	var latestAssistantMessage string
	inTargetTurn := false
	scanner := newCodexTranscriptScanner(file)
	for scanner.Scan() {
		var line struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type == "turn_context" {
			var payload struct {
				TurnID string `json:"turn_id"`
			}
			if err := json.Unmarshal(line.Payload, &payload); err == nil {
				inTargetTurn = payload.TurnID == turnID
			}
			continue
		}
		if !inTargetTurn || line.Type != "response_item" {
			continue
		}

		var payload struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(line.Payload, &payload); err != nil {
			continue
		}
		if payload.Type != "message" || payload.Role != "assistant" {
			continue
		}
		var combined strings.Builder
		for _, item := range payload.Content {
			if item.Type == "output_text" {
				combined.WriteString(item.Text)
			}
		}
		latestAssistantMessage = combined.String()
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook --mode codex: could not scan transcript %s: %v\n", path, err)
	}
	return extractProposedPlan(latestAssistantMessage)
}

func newCodexTranscriptScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	// Scanner reuses this initial token buffer for the whole transcript scan.
	// The max is higher because Codex JSONL response_item records can contain
	// a large assistant message on one line.
	initialTokenBuffer := make([]byte, codexTranscriptInitialTokenBuffer)
	scanner.Buffer(initialTokenBuffer, codexTranscriptMaxTokenSize)
	return scanner
}

func proposedPlanFromCodexEvent(event codexStopHookEvent) (string, bool) {
	if event.LastAssistantMessage != nil {
		if plan, ok := extractProposedPlan(*event.LastAssistantMessage); ok {
			return plan, true
		}
	}
	if event.TranscriptPath != nil && strings.TrimSpace(*event.TranscriptPath) != "" {
		return extractProposedPlanFromCodexTranscript(*event.TranscriptPath, event.TurnID)
	}
	return "", false
}

func emitCodexStopDecision(approved bool, prompt string) {
	if approved {
		return
	}
	if prompt == "" {
		prompt = "Review comments pending — address them before proceeding."
	}
	out, _ := json.Marshal(map[string]any{
		"decision": "block",
		"reason":   prompt,
	})
	fmt.Println(string(out))
}

var runCodexPlanReviewHook = func(sessionID string, content []byte) {
	runPlanReviewHook("crit plan-hook --mode codex", sessionID, content, emitCodexStopDecision)
}

func runPlanReviewHook(logPrefix, sessionID string, content []byte, emitDecision func(bool, string)) {
	slug := resolveHookSlug(sessionID, content)

	storageDir, err := planStorageDir(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: error resolving storage dir: %v\n", logPrefix, err)
		emitDecision(false, fmt.Sprintf("Crit could not prepare plan storage: %v", err))
		return
	}

	ver, err := savePlanVersion(storageDir, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: error saving plan: %v\n", logPrefix, err)
		emitDecision(false, fmt.Sprintf("Crit could not save the proposed plan: %v", err))
		return
	}
	fmt.Fprintf(os.Stderr, "%s: plan '%s' saved as v%03d\n", logPrefix, slug, ver)

	cwd, _ := resolvedCWD()
	key := planSessionKey(cwd, slug)
	currentPath := filepath.Join(storageDir, "current.md")
	daemonArgs := buildPlanDaemonArgs(currentPath, storageDir, slug, commonDaemonFlags{})

	entry, alive := findAliveSession(key)
	weStartedDaemon := false

	if alive {
		fmt.Fprintf(os.Stderr, "%s: connected to daemon at %s\n", logPrefix, entry.baseURL())
		if !daemonHasBrowser(entry) {
			go openBrowser(entry.baseURL())
		}
	} else {
		entry, err = startDaemon(key, daemonArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: error starting daemon: %v\n", logPrefix, err)
			emitDecision(false, fmt.Sprintf("Crit could not start the review UI: %v", err))
			return
		}
		fmt.Fprintf(os.Stderr, "%s: started daemon at %s (PID %d)\n", logPrefix, entry.baseURL(), entry.PID)
		weStartedDaemon = true
	}

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved, prompt := runReviewClientRaw(entry, key)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, LoadConfig(cwd).CleanupOnApproveEnabled())
	emitDecision(approved, prompt)
}

// runPlanHook is the PermissionRequest hook handler for ExitPlanMode.
// It reads the hook event JSON from stdin, extracts the plan content,
// opens a crit review session, and writes a hookSpecificOutput JSON
// decision (allow/deny) to stdout.
func runPlanHook() {
	go backgroundCleanup()

	var event planHookEvent
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not parse stdin: %v\n", err)
		emitHookDecision(false, "Crit could not parse the plan hook input; plan was not reviewed.")
		return
	}
	if strings.TrimSpace(event.ToolInput.Plan) == "" {
		return
	}

	runPlanReviewHook("crit plan-hook", event.SessionID, []byte(event.ToolInput.Plan), emitHookDecision)
}

// runCodexPlanHook is the Stop hook handler for Codex proposed-plan review.
// Codex has no ExitPlanMode permission hook, so this recovers the raw
// proposed-plan block from the Stop payload/transcript and blocks the stop
// when Crit returns comments.
func runCodexPlanHook() {
	var event codexStopHookEvent
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook --mode codex: could not parse stdin: %v\n", err)
		emitCodexStopDecision(false, "Crit could not parse the Codex hook input; plan was not reviewed.")
		return
	}
	plan, ok := proposedPlanFromCodexEvent(event)
	if !ok {
		return
	}

	go backgroundCleanup()
	runCodexPlanReviewHook(event.SessionID, []byte(plan))
}

// waitForDaemonReady polls the daemon's /api/session endpoint until it stops
// returning 503 Service Unavailable (session not yet initialized). Returns the
// last response status code and body, or an error if the daemon is unreachable
// or the 5-minute deadline expires.
//
// On connection errors (e.g. "connection refused"), the daemon log is consulted
// to surface the actual init failure instead of the misleading network error.
func waitForDaemonReady(client *http.Client, host string, port int, sessionKey string) (statusCode int, body []byte, err error) {
	connHost := host
	if connHost == "" {
		connHost = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s:%d", connHost, port)
	deadline := time.Now().Add(5 * time.Minute)
	for {
		resp, reqErr := client.Get(base + "/api/session")
		if reqErr != nil {
			if sessionKey != "" {
				if msg := readDaemonLog(sessionKey); msg != "" {
					return 0, nil, fmt.Errorf("%s", msg)
				}
			}
			return 0, nil, fmt.Errorf("could not reach daemon on port %d: %w", port, reqErr)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			return resp.StatusCode, respBody, nil
		}
		if time.Now().After(deadline) {
			return 0, nil, fmt.Errorf("daemon did not become ready within 5 minutes")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// runReviewClientRaw is like runReviewClient but returns (approved, prompt)
// without writing to stdout — used by runPlanHook to construct hookSpecificOutput.
func runReviewClientRaw(entry sessionEntry, sessionKey string) (approved bool, prompt string) {
	client := &http.Client{Timeout: 24 * time.Hour}

	// Wait for the server to finish initializing before calling review-cycle.
	if _, _, err := waitForDaemonReady(client, entry.Host, entry.Port, sessionKey); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: %v\n", err)
		return false, "crit daemon was unreachable; plan was not reviewed."
	}

	resp, err := client.Post(
		entry.connURL()+"/api/review-cycle",
		"application/json",
		nil,
	)
	if err != nil {
		// Daemon died (graceful shutdown, crash, or `crit stop`) while we
		// were waiting. Deny rather than silently auto-approve — an
		// unreachable daemon means no human signed off on the plan.
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not reach daemon: %v\n", err)
		return false, "crit daemon became unreachable before review was finished."
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not read daemon response: %v\n", err)
		return false, "crit daemon response could not be read."
	}

	// Both 200 (finish) and 503 (server-shutdown) carry a structured
	// {approved, prompt} body. Other statuses mean infrastructure failure —
	// deny in that case rather than allowing through.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		fmt.Fprintf(os.Stderr, "crit plan-hook: daemon returned %d\n", resp.StatusCode)
		return false, "crit daemon returned an unexpected status."
	}

	var result struct {
		Approved bool   `json:"approved"`
		Prompt   string `json:"prompt"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: malformed daemon response: %v\n", err)
		return false, "crit daemon returned a malformed response."
	}
	return result.Approved, result.Prompt
}

// runPR is the `crit pr <num|url>` subcommand. Thin shim that forwards to
// runReview with a synthesized --pr flag so the daemon path is shared.
func runPR(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: crit pr <num|url>")
		os.Exit(1)
	}
	runReview([]string{"--pr", args[0]})
}

// focusKeyArgs returns the args slice used to key the daemon session for a
// PR/range focus. PR-keyed daemons reuse the same review file across head
// changes; range-keyed daemons are unique per (base, head) pair.
//
// Crucially, DiffScope is NOT part of the key — the picker must let users
// toggle scopes within a single session.
func focusKeyArgs(sc *serverConfig) []string {
	if sc == nil || sc.focus == nil || sc.focus.Kind != FocusRange {
		return sc.files
	}
	if sc.focus.PRNumber > 0 {
		return []string{fmt.Sprintf("pr:%d", sc.focus.PRNumber)}
	}
	return []string{fmt.Sprintf("range:%s..%s", sc.focus.BaseSHA, sc.focus.HeadSHA)}
}

func runReview(args []string) {
	go backgroundCleanup()

	// Parse args to extract file args (stripping flags like --port, --no-open).
	// The session key must use only file args to match what runServe computes.
	sc, err := resolveServerConfig(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if sc == nil {
		return // --version
	}

	cwd, _ := resolvedCWD()
	branch := ""
	if vcs := DetectVCS(sc.vcsOverride); vcs != nil {
		branch = vcs.CurrentBranch()
	}
	key := sessionKey(cwd, branch, focusKeyArgs(sc))

	// Check for running daemon with the same session key
	entry, alive := findAliveSession(key)
	weStartedDaemon := false

	if alive {
		fmt.Fprintf(os.Stderr, "Connected to crit daemon at %s\n", entry.baseURL())
		// Re-open browser if no browser tab is connected (user closed it)
		if !sc.noOpen && !daemonHasBrowser(entry) {
			go openBrowser(entry.baseURL())
		}
	} else {
		// Pre-flight: in default git mode (no files, no focus, no plan), surface
		// errors up front instead of letting the daemon spawn, signal readiness,
		// then crash on init — which leaves the user with a misleading
		// "could not reach daemon / connection refused" error. See #438, #593.
		if len(sc.files) == 0 && sc.focus == nil && sc.planDir == "" {
			if msg := preflightCheck(sc); msg != "" {
				fmt.Fprint(os.Stderr, msg)
				os.Exit(1)
			}
		}
		// Pass raw args to startDaemon — the _serve process parses them itself
		entry, err = startDaemon(key, args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Started crit daemon at %s (PID %d)\n", entry.baseURL(), entry.PID)
		if dirs := dirArgs(sc.files); len(dirs) > 0 {
			fmt.Fprintf(os.Stderr, "\nNote: scanning %s — file paths are intended for reviewing a small set of\n"+
				"documents or plans. To review code changes, run `crit` with no arguments\n"+
				"on a feature branch.\n\n", strings.Join(dirs, ", "))
		}
		if !sc.noIntegrationCheck {
			hintMissingIntegrations()
		}
		weStartedDaemon = true
	}

	// If we started the daemon, clean it up on Ctrl+C
	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved := runReviewClient(entry, key)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, LoadConfig(cwd).CleanupOnApproveEnabled())
}

// readReviewCycleResponse reads and closes the response body, returning an
// error for non-success status codes. This avoids exitAfterDefer by ensuring
// the body is closed before the caller decides to os.Exit.
func readReviewCycleResponse(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGatewayTimeout {
		return nil, fmt.Errorf("timeout waiting for review")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return body, nil
}

// runReviewClient connects to a running daemon/server, blocks until the user
// finishes reviewing, prints feedback to stdout, and returns whether the
// review was approved (no unresolved comments).
func runReviewClient(entry sessionEntry, sessionKey string) (approved bool) {
	client := &http.Client{Timeout: 24 * time.Hour}

	// Wait for the server to finish initializing before calling review-cycle.
	statusCode, body, err := waitForDaemonReady(client, entry.Host, entry.Port, sessionKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if statusCode == http.StatusInternalServerError {
		var status struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &status) == nil && status.Message != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", status.Message)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", body)
		}
		os.Exit(1)
	}

	resp, err := client.Post(
		entry.connURL()+"/api/review-cycle",
		"application/json",
		nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not reach crit daemon on port %d: %v\n", entry.Port, err)
		os.Exit(1)
	}

	body, err = readReviewCycleResponse(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Print feedback to stdout
	os.Stdout.Write(body)

	// Check if the review was approved (no unresolved comments).
	var result struct {
		Approved    bool   `json:"approved"`
		NextCommand string `json:"next_command"`
		Stats       *struct {
			Duration int `json:"duration_seconds"`
			Files    int `json:"files_reviewed"`
			Comments int `json:"comments_submitted"`
		} `json:"stats"`
	}
	if json.Unmarshal(body, &result) == nil {
		// Print the exact command for the next round so the agent can
		// re-invoke crit without reconstructing args. Skip on approve —
		// the loop is over. Prepend a newline because the JSON body may
		// or may not end with one.
		if !result.Approved && result.NextCommand != "" {
			fmt.Fprintf(os.Stdout, "\nNext round: %s\n", result.NextCommand)
		}
		if result.Approved && result.Stats != nil {
			printSessionSummary(result.Stats)
		}
		return result.Approved
	}
	return false
}

func printSessionSummary(s *struct {
	Duration int `json:"duration_seconds"`
	Files    int `json:"files_reviewed"`
	Comments int `json:"comments_submitted"`
}) {
	if s.Files == 0 && s.Comments == 0 {
		return
	}
	var parts []string
	if s.Files > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", s.Files, pluralize(s.Files, "file", "files")))
	}
	if s.Comments > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", s.Comments, pluralize(s.Comments, "comment", "comments")))
	}
	parts = append(parts, formatDuration(s.Duration))
	fmt.Fprintf(os.Stderr, "\nDone reviewing — %s\n", strings.Join(parts, " · "))
}

// dirArgs returns the subset of paths that are directories.
func dirArgs(paths []string) []string {
	var dirs []string
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			dirs = append(dirs, p)
		}
	}
	return dirs
}

// TODO: runStop, runStatus, and other subcommands use DetectVCS("") for auto-detection.
// The --vcs flag from the main server command is not threaded through to these subcommands yet.
// This is acceptable for v1 since subcommands primarily need to locate the daemon, not run VCS ops.
func runStop(args []string) {
	all := false
	var fileArgs []string
	for _, arg := range args {
		if arg == "--all" {
			all = true
		} else {
			fileArgs = append(fileArgs, arg)
		}
	}

	cwd, _ := resolvedCWD()

	if all {
		stopAllDaemonsForCWD(cwd)
		fmt.Println("All daemons stopped.")
		return
	}

	branch := ""
	if vcs := DetectVCS(""); vcs != nil {
		branch = vcs.CurrentBranch()
	}

	// If file args were given, use the exact key (user knows which session).
	if len(fileArgs) > 0 {
		key := sessionKey(cwd, branch, fileArgs)
		if err := stopDaemon(key); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Daemon stopped.")
		return
	}

	// No file args: try exact key first (git-mode session with no args).
	key := sessionKey(cwd, branch, nil)
	if entry, _ := readSessionFile(key); entry.PID > 0 {
		if err := stopDaemon(key); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Daemon stopped.")
		return
	}

	// Exact key not found — fall back to scanning by cwd + branch.
	_, foundKey, matchCount := findSessionForCWDBranch(cwd, branch)
	if matchCount > 1 {
		fmt.Fprintf(os.Stderr, "Error: multiple daemons running on branch %q. Use 'crit stop --all' or specify file args.\n", branch)
		os.Exit(1)
	}
	if matchCount == 0 {
		fmt.Fprintln(os.Stderr, "Error: no running daemon found for current directory and branch.")
		os.Exit(1)
	}

	if err := stopDaemon(foundKey); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Daemon stopped.")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// mustGetwd returns the current working directory or aborts the process with a
// clear diagnostic. Used in CLI paths where every fallback (config dir, repo
// root) has already failed; if we cannot read the cwd, we genuinely cannot
// continue.
func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit: unable to determine current working directory: %v\n", err)
		os.Exit(1)
	}
	return wd
}

func runStatus(args []string) {
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
		}
	}

	cwd, err := resolvedCWD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	vcsName := ""
	branch := ""
	if vcs := DetectVCS(""); vcs != nil {
		vcsName = vcs.Name()
		branch = vcs.CurrentBranch()
	}

	sessions, _ := listSessionsForCWD(cwd)
	var matchedSession *sessionEntry
	for i, s := range sessions {
		if s.Branch == branch || (branch == "" && len(sessions) == 1) {
			matchedSession = &sessions[i]
			break
		}
	}

	var revPath string
	if matchedSession != nil && matchedSession.ReviewPath != "" {
		revPath = matchedSession.ReviewPath
	} else {
		key := sessionKey(cwd, branch, nil)
		revPath, _ = reviewFilePath(key)
	}

	revExists := false
	if _, statErr := os.Stat(reviewPathsFor(revPath).Review); statErr == nil {
		revExists = true
	}

	if jsonOutput {
		printStatusJSON(vcsName, branch, revPath, revExists, matchedSession)
		return
	}

	printStatusHuman(vcsName, branch, revPath, revExists, matchedSession)
}

func printStatusJSON(vcsName, branch, revPath string, revExists bool, session *sessionEntry) {
	result := map[string]interface{}{
		"vcs":                vcsName,
		"branch":             branch,
		"review_file":        reviewPathsFor(revPath).Review,
		"review_file_exists": revExists,
	}
	daemon := map[string]interface{}{"running": false}
	if session != nil {
		daemon["running"] = true
		daemon["pid"] = session.PID
		daemon["port"] = session.Port
	}
	result["daemon"] = daemon

	if revExists {
		addReviewStats(result, revPath)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func addReviewStats(result map[string]interface{}, revPath string) {
	data, err := os.ReadFile(reviewPathsFor(revPath).Review)
	if err != nil {
		return
	}
	var cj CritJSON
	if json.Unmarshal(data, &cj) != nil {
		return
	}
	result["round"] = cj.ReviewRound
	if cj.ReviewType != "" {
		result["review_type"] = cj.ReviewType
	}
	if cj.Origin != "" {
		result["origin"] = cj.Origin
	}
	unresolved, resolved := countComments(cj)
	result["comments"] = map[string]int{
		"unresolved": unresolved,
		"resolved":   resolved,
	}
}

func printStatusHuman(vcsName, branch, revPath string, revExists bool, session *sessionEntry) {
	if vcsName != "" {
		fmt.Printf("VCS:         %s\n", vcsName)
	}
	if branch != "" {
		fmt.Printf("Branch:      %s\n", branch)
	}
	fmt.Printf("Review file: %s\n", reviewPathsFor(revPath).Review)
	if session != nil {
		fmt.Printf("Daemon:      running (PID %d, port %d)\n", session.PID, session.Port)
	} else {
		fmt.Println("Daemon:      not running")
	}
	if !revExists {
		return
	}
	data, err := os.ReadFile(reviewPathsFor(revPath).Review)
	if err != nil {
		return
	}
	var cj CritJSON
	if json.Unmarshal(data, &cj) != nil {
		return
	}
	if cj.ReviewType == "live" {
		fmt.Printf("Mode:        live\n")
		if cj.Origin != "" {
			fmt.Printf("Origin:      %s\n", cj.Origin)
		}
	}
	fmt.Printf("Round:       %d\n", cj.ReviewRound)
	unresolved, resolved := countComments(cj)
	fmt.Printf("Comments:    %d unresolved, %d resolved\n", unresolved, resolved)
}

func countComments(cj CritJSON) (unresolved, resolved int) {
	for _, f := range cj.Files {
		for _, c := range f.Comments {
			if c.Resolved {
				resolved++
			} else {
				unresolved++
			}
		}
	}
	for _, c := range cj.ReviewComments {
		if c.Resolved {
			resolved++
		} else {
			unresolved++
		}
	}
	return
}

func runCleanup(args []string) {
	days := 7
	force := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--days":
			if i+1 < len(args) {
				i++
				d, err := strconv.Atoi(args[i])
				if err != nil || d < 0 {
					fmt.Fprintf(os.Stderr, "Error: invalid --days value\n")
					os.Exit(1)
				}
				days = d
			}
		case "--force":
			force = true
		default:
			fmt.Fprintf(os.Stderr, "Usage: crit cleanup [--days N] [--force]\n")
			os.Exit(1)
		}
	}

	revDir, err := reviewsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	stale := findStaleReviews(revDir, days)
	if len(stale) == 0 {
		fmt.Println("No stale review files found.")
		return
	}

	fmt.Printf("Found %d stale review file%s:\n", len(stale), plural(len(stale)))
	for _, s := range stale {
		fmt.Printf("  %s  (%s%d days old, %d comment%s)\n", s.path, s.metaLabel(), int(s.age.Hours()/24), s.comments, plural(s.comments))
	}

	if !force {
		fmt.Print("Delete all? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return
		}
	}

	deleted := deleteStaleReviews(stale)
	fmt.Printf("Deleted %d review file%s.\n", deleted, plural(deleted))
}

type staleReview struct {
	key        string
	path       string
	branch     string
	reviewType string
	origin     string
	age        time.Duration
	comments   int
}

func (s staleReview) metaLabel() string {
	if s.reviewType == "live" {
		if s.origin != "" {
			return "live: " + s.origin + ", "
		}
		return "live, "
	}
	if s.branch != "" {
		return s.branch + ", "
	}
	return ""
}

func findStaleReviews(revDir string, days int) []staleReview {
	entries, err := os.ReadDir(revDir)
	if err != nil {
		return nil
	}

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	activeSessions := buildActiveSessionSet()

	var stale []staleReview
	for _, de := range entries {
		name := de.Name()

		if de.IsDir() {
			// MIGRATION-REMOVAL: pre-fix early-v4 folders kept a stray .json
			// extension on the folder name. Strip it for the session-key
			// lookup; the standard load path will rename the folder on access.
			key := strings.TrimSuffix(name, ".json")
			if activeSessions[key] {
				continue
			}
			if sr, ok := checkStaleReviewFolder(revDir, de, key, cutoff); ok {
				stale = append(stale, sr)
			}
			continue
		}

		// MIGRATION-REMOVAL: legacy v3 flat *.json file. Treat as a stale
		// candidate so cleanup wipes it (and any sibling sidecar) the next
		// time crit runs. After the migration removal release this branch
		// goes away.
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		key := strings.TrimSuffix(name, ".json")
		if activeSessions[key] {
			continue
		}
		if sr, ok := checkStaleReview(revDir, de, key, cutoff); ok {
			stale = append(stale, sr)
		}
	}
	return stale
}

// checkStaleReviewFolder evaluates a directory entry inside the reviews dir.
// It is a v4-native staleness check for folder-form reviews. Three possible
// outcomes:
//
//  1. The folder contains review.json: read it, parse UpdatedAt, fall back
//     to file mtime if missing. Stale if the timestamp is before cutoff.
//  2. The folder lacks review.json but contains snapshots.json: it's an
//     orphan-snapshots folder (e.g. a crashed migration or a deleted review
//     left behind a sidecar). Stale if folder mtime is before cutoff.
//  3. Empty / unrecognized contents: skip.
func checkStaleReviewFolder(revDir string, de os.DirEntry, key string, cutoff time.Time) (staleReview, bool) {
	folder := filepath.Join(revDir, de.Name())
	reviewPath := filepath.Join(folder, "review.json")

	if data, readErr := os.ReadFile(reviewPath); readErr == nil {
		var cj CritJSON
		var updatedAt time.Time
		var branch string
		var reviewType string
		var origin string
		var commentCount int
		if json.Unmarshal(data, &cj) == nil {
			branch = cj.Branch
			reviewType = cj.ReviewType
			origin = cj.Origin
			if t, parseErr := time.Parse(time.RFC3339, cj.UpdatedAt); parseErr == nil {
				updatedAt = t
			}
			for _, f := range cj.Files {
				commentCount += len(f.Comments)
			}
			commentCount += len(cj.ReviewComments)
		}
		if updatedAt.IsZero() {
			if info, statErr := os.Stat(reviewPath); statErr == nil {
				updatedAt = info.ModTime()
			}
		}
		if !updatedAt.Before(cutoff) {
			return staleReview{}, false
		}
		return staleReview{
			key:        key,
			path:       folder,
			branch:     branch,
			reviewType: reviewType,
			origin:     origin,
			age:        time.Since(updatedAt),
			comments:   commentCount,
		}, true
	}

	// review.json missing — check for orphan snapshots folder.
	if _, err := os.Stat(filepath.Join(folder, "snapshots.json")); err != nil {
		return staleReview{}, false
	}
	info, err := de.Info()
	if err != nil {
		return staleReview{}, false
	}
	if !info.ModTime().Before(cutoff) {
		return staleReview{}, false
	}
	return staleReview{
		key:  key,
		path: folder,
		age:  time.Since(info.ModTime()),
	}, true
}

func buildActiveSessionSet() map[string]bool {
	sessDir, _ := sessionsDir()
	active := make(map[string]bool)
	sessEntries, err := os.ReadDir(sessDir)
	if err != nil {
		return active
	}
	for _, se := range sessEntries {
		if !strings.HasSuffix(se.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(se.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(sessDir, se.Name()))
		if err != nil {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal(data, &entry) == nil && isDaemonAlive(entry) {
			active[key] = true
		}
	}
	return active
}

func checkStaleReview(revDir string, de os.DirEntry, key string, cutoff time.Time) (staleReview, bool) {
	path := filepath.Join(revDir, de.Name())
	info, err := de.Info()
	if err != nil {
		return staleReview{}, false
	}

	var updatedAt time.Time
	var branch string
	var commentCount int
	if data, readErr := os.ReadFile(path); readErr == nil {
		var cj CritJSON
		if json.Unmarshal(data, &cj) == nil {
			branch = cj.Branch
			if t, parseErr := time.Parse(time.RFC3339, cj.UpdatedAt); parseErr == nil {
				updatedAt = t
			}
			for _, f := range cj.Files {
				commentCount += len(f.Comments)
			}
			commentCount += len(cj.ReviewComments)
		}
	}
	if updatedAt.IsZero() {
		updatedAt = info.ModTime()
	}

	if !updatedAt.Before(cutoff) {
		return staleReview{}, false
	}
	return staleReview{
		key:      key,
		path:     path,
		branch:   branch,
		age:      time.Since(updatedAt),
		comments: commentCount,
	}, true
}

func deleteStaleReviews(stale []staleReview) int {
	sessDir, _ := sessionsDir()
	deleted := 0
	for _, s := range stale {
		if !removeStaleReviewPath(s.path) {
			fmt.Fprintf(os.Stderr, "Error deleting %s: directory not empty or path missing\n", s.path)
			continue
		}
		deleted++
		if sessDir != "" {
			os.Remove(filepath.Join(sessDir, s.key+".json"))
			os.Remove(filepath.Join(sessDir, s.key+".lock"))
			os.Remove(filepath.Join(sessDir, s.key+".log"))
		}
	}
	return deleted
}
