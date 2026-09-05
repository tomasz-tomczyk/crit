package share

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/tomasz-tomczyk/crit/internal/auth"
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"golang.org/x/term"
)

type shareFlags struct {
	outputDir        string
	configuredOutput string
	sessionID        string
	svcURL           string
	svcURLSet        bool
	showQR           bool
	org              string
	visibility       string
	preview          string
	files            []string
}

type unpublishFlags struct {
	outputDir        string
	configuredOutput string
	sessionID        string
	svcURL           string
	svcURLSet        bool
	files            []string
}

func postPreviewShare(htmlPath, svcURL, authToken string) (string, error) {
	files, err := session.CrawlPreview(htmlPath)
	if err != nil {
		return "", fmt.Errorf("crawling preview assets: %w", err)
	}

	payload := BuildSharePayload(files, nil, 1, []string{"preview", htmlPath}, "", "", "preview")
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling preview payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, svcURL+"/api/reviews", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	SetBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: config.SameOriginRedirectPolicy}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("posting preview to share service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrShareUnauthorized
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("preview share failed with status %d", resp.StatusCode)
	}

	var result struct {
		URL         string `json:"url"`
		DeleteToken string `json:"delete_token"`
	}
	if err := DecodeJSONOrHTMLHint(resp, &result); err != nil {
		return "", err
	}
	return result.URL, nil
}

func parseShareFlags(args []string) (shareFlags, error) {
	var sf shareFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--qr" {
			sf.showQR = true
			continue
		}
		if dest, ok := shareFlagDest(&sf, arg); ok {
			val, err := clicmd.RequireFlagValue(args, i, arg)
			if err != nil {
				return sf, err
			}
			*dest = val
			if arg == "--share-url" {
				sf.svcURLSet = true
			}
			i++
			continue
		}
		sf.files = append(sf.files, arg)
	}
	return sf, nil
}

func shareFlagDest(sf *shareFlags, arg string) (*string, bool) {
	switch arg {
	case "--output", "-o":
		return &sf.outputDir, true
	case "--session":
		return &sf.sessionID, true
	case "--share-url":
		return &sf.svcURL, true
	case "--preview":
		return &sf.preview, true
	case "--org":
		return &sf.org, true
	case "--visibility":
		return &sf.visibility, true
	default:
		return nil, false
	}
}

func applyShareConfigDefaults(sf *shareFlags, cfg config.Config) {
	sf.configuredOutput = cfg.Output
}

func runSharePreview(sf shareFlags) error {
	if len(sf.files) > 0 {
		return clicmd.Usage("Error: --preview cannot be combined with file arguments")
	}
	cfg := LoadShareConfig()
	target, ok, err := config.SelectShareTarget(sf.svcURL, sf.svcURLSet, cfg)
	if err != nil {
		return err
	}
	if !ok {
		return clicmd.Usage("Error: sharing is disabled; configure a share target or pass --share-url")
	}
	if target.ProxyAuth {
		return proxyAuthCLIError("crit share")
	}
	url, err := postPreviewShare(sf.preview, target.URL, target.Auth.Token)
	if err != nil {
		return err
	}
	fmt.Println(url)
	return nil
}

func shareUsageError() error {
	fmt.Fprintln(os.Stderr, "Usage: crit share [--session <id>] [--output <dir>] [--share-url <url>] [--org <slug>] [--visibility <level>] [--qr] <file> [file...]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Shares files to crit-web and prints the review URL.")
	fmt.Fprintln(os.Stderr, "Comments from the review file are included automatically.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  crit share plan.md")
	fmt.Fprintln(os.Stderr, "  crit share plan.md src/main.go")
	fmt.Fprintln(os.Stderr, "  crit share --qr plan.md")
	return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
}

func loadShareFiles(paths []string) ([]ShareFile, error) {
	var files []ShareFile
	wd, err := mustGetwd()
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		relPath := path
		if filepath.IsAbs(path) {
			if rel, err := filepath.Rel(wd, path); err == nil {
				relPath = rel
			}
		}
		files = append(files, ShareFile{Path: relPath, Content: string(content)})
	}
	return files, nil
}

func printQR(url string, showQR bool) {
	if showQR {
		fmt.Fprintln(os.Stderr)
		qrterminal.GenerateWithConfig(url, qrterminal.Config{
			Level:      qrterminal.L,
			Writer:     os.Stderr,
			HalfBlocks: true,
			QuietZone:  1,
		})
	}
}

// noteStoryNotShared prints a one-line notice (spec §10 "crit share interplay")
// when the review has a story: crit-web has no story surface yet, so a shared
// review silently lacks it. Best-effort — a missing/unreadable review file is
// not an error here.
func noteStoryNotShared(critPath string) {
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		return
	}
	if cj.Story != nil {
		fmt.Fprintln(os.Stderr, "note: the story is not included in the shared view (crit-web story support is planned)")
	}
}

func handleShareAuthError(targetURL string) {
	auth.ClearTargetAuth(targetURL)
	fmt.Fprintln(os.Stderr, "Auth token rejected by server; cleared local credentials. Run 'crit auth login' to re-authenticate.")
}

func runShareExisting(existingCfg session.CritJSON, critPath string, files []ShareFile, sharePaths []string, svcURL, authToken, fallbackAuthor, org, visibility string, showQR bool) error {
	localIDs := BuildLocalIDSet(existingCfg)
	localFingerprints, localFingerprintIDs := BuildLocalFingerprintIndex(existingCfg)
	if fetched, err := FetchWebCommentsFromTarget(existingCfg.ShareURL, svcURL, localIDs, localFingerprints, localFingerprintIDs, authToken); err != nil {
		if errors.Is(err, ErrShareUnauthorized) {
			handleShareAuthError(svcURL)
			return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
		if errors.Is(err, ErrShareNotFound) {
			fmt.Fprintln(os.Stderr, "warning: previous shared review no longer exists; creating a new share")
			if err := ClearShareState(critPath); err != nil {
				return err
			}
			return runShareNew(critPath, files, sharePaths, svcURL, authToken, fallbackAuthor, org, visibility, showQR)
		}
		fmt.Fprintf(os.Stderr, "warning: could not pull remote comments: %v\n", err)
	} else if len(fetched.NewComments) > 0 || len(fetched.ReplyUpdates) > 0 {
		if err := MergeWebComments(critPath, fetched.NewComments, fetched.ReplyUpdates); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not merge remote comments: %v\n", err)
		}
	}

	allComments, _ := LoadCommentsForShare(critPath, sharePaths, fallbackAuthor)

	result, err := UpsertShareToWeb(existingCfg, files, allComments, authToken)
	if err != nil {
		if errors.Is(err, ErrShareUnauthorized) {
			handleShareAuthError(svcURL)
		}
		if errors.Is(err, ErrShareNotFound) {
			fmt.Fprintln(os.Stderr, "warning: previous shared review no longer exists; creating a new share")
			if err := ClearShareState(critPath); err != nil {
				return err
			}
			return runShareNew(critPath, files, sharePaths, svcURL, authToken, fallbackAuthor, org, visibility, showQR)
		}
		return err
	}

	if err := UpdateShareState(critPath, ComputeShareHash(files, allComments), result.ReviewRound); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state: %v\n", err)
	}
	if existingCfg.ShareBaseURL == "" {
		_ = bindShareBaseURL(critPath, svcURL)
	}
	if result.Changed {
		fmt.Fprintf(os.Stderr, "updated round %d\n", result.ReviewRound)
	}
	fmt.Println(result.URL)

	printQR(result.URL, showQR)
	return nil
}

func runShareNew(critPath string, files []ShareFile, filePaths []string, svcURL, authToken, fallbackAuthor, org, visibility string, showQR bool) error {
	res, err := ShareReviewFiles(critPath, files, filePaths, svcURL, authToken, fallbackAuthor, org, visibility, "")
	if err != nil {
		if errors.Is(err, ErrShareUnauthorized) {
			handleShareAuthError(svcURL)
		}
		return err
	}

	if err := PersistShareStateForTarget(critPath, res.URL, svcURL, res.DeleteToken, ShareScope(filePaths), org, "", visibility); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state to review file: %v\n", err)
	}

	initialComments, _ := LoadCommentsForShare(critPath, filePaths, fallbackAuthor)
	_ = UpdateShareState(critPath, ComputeShareHash(files, initialComments), res.ReviewRound)

	fmt.Println(res.URL)
	printQR(res.URL, showQR)

	if authToken == "" {
		auth.ShowLoginHint()
	}
	return nil
}

func promptShareConsent(out io.Writer, in io.Reader) bool {
	fmt.Fprintln(out, "  Your review will be securely uploaded to crit.md.")
	fmt.Fprintln(out, "  You'll get a private link — share it with whoever you choose.")
	fmt.Fprintln(out, "  You won't be asked again after confirming.")
	fmt.Fprint(out, "\n  Continue? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(strings.ToLower(answer)) == "y"
}

func promptShareURLConfirm(out io.Writer, in io.Reader, shareURL string) bool {
	fmt.Fprintf(out, "  Sharing to %s — continue? [y/N] ", shareURL)
	answer, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(strings.ToLower(answer)) == "y"
}

// RunShare uploads files to crit-web and prints the review URL.
func RunShare(args []string) error { //nolint:gocyclo // CLI dispatcher
	sf, err := parseShareFlags(args)
	if err != nil {
		return err
	}
	if sf.preview != "" {
		return runSharePreview(sf)
	}

	if len(sf.files) == 0 {
		return shareUsageError()
	}

	flagURL := sf.svcURLSet

	cfg := LoadShareConfig()
	applyShareConfigDefaults(&sf, cfg)

	files, err := loadShareFiles(sf.files)
	if err != nil {
		return err
	}

	critPath, err := review.ResolveCommandReviewPathWithSession(sf.sessionID, sf.outputDir, sf.configuredOutput)
	if err != nil {
		return err
	}
	if err := CheckShareAllowed(critPath); err != nil {
		return err
	}
	noteStoryNotShared(critPath)

	sharePaths := make([]string, len(files))
	for i, f := range files {
		sharePaths[i] = f.Path
	}

	existing, existingOK, err := LoadExistingShareCfg(critPath, sharePaths)
	if err != nil {
		return err
	}
	target, err := resolveOperationTarget(cfg, sf.svcURL, sf.svcURLSet, existing, existingOK)
	if err != nil {
		return err
	}
	if target.ProxyAuth {
		return proxyAuthCLIError("crit share")
	}
	sf.svcURL = target.URL
	auth.LazyBackfillTargetAuth(target.URL)
	if refreshed, found, _ := config.FindShareTarget(LoadShareConfig(), target.URL); found {
		target = refreshed
	}
	authToken := target.Auth.Token
	if !existingOK && target.NeedsShareConsent() {
		if !promptShareConsent(os.Stderr, os.Stdin) {
			return nil
		}
		if err := config.SaveTargetConsent(target.URL); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not save consent: %v\n", err)
		}
	}
	if flagURL && term.IsTerminal(int(os.Stdin.Fd())) {
		if !promptShareURLConfirm(os.Stderr, os.Stdin, sf.svcURL) {
			return nil
		}
	}

	err = session.WithShareLock(critPath, func() error {
		// An explicit configured target is also confirmation of an ambiguous
		// legacy review's origin. Persist that local repair independently of the
		// remote operation so an offline instance does not prevent migration.
		if existingOK && existing.ShareBaseURL == "" && sf.svcURLSet {
			if bindErr := bindShareBaseURL(critPath, target.URL); bindErr != nil {
				return bindErr
			}
		}
		return runShareUnderLock(critPath, files, sharePaths, target.URL, authToken, cfg.Author, sf.org, sf.visibility, sf.showQR)
	})
	if err != nil {
		return err
	}
	if _, configured, _ := config.FindShareTarget(cfg, target.URL); configured {
		if migrateErr := config.MutateShareTargets(func(_ *[]config.ShareTarget) error { return nil }); migrateErr != nil {
			fmt.Fprintf(os.Stderr, "warning: share succeeded but legacy config migration failed: %v\n", migrateErr)
		}
	}
	return nil
}

func resolveOperationTarget(cfg config.Config, explicit string, explicitSet bool, cj session.CritJSON, existing bool) (config.ShareTarget, error) { //nolint:gocyclo // Existing-review affinity and new-share precedence are handled together.
	if !existing {
		target, ok, err := config.SelectShareTarget(explicit, explicitSet, cfg)
		if err != nil {
			return config.ShareTarget{}, err
		}
		if !ok {
			return config.ShareTarget{}, clicmd.Usage("Error: sharing is disabled; configure a share target or pass --share-url")
		}
		return target, nil
	}
	base := cj.ShareBaseURL
	if base == "" {
		targets, err := config.ResolveShareTargets(cfg)
		if err != nil {
			return config.ShareTarget{}, err
		}
		if inferred, ok := config.InferShareBaseURL(cj.ShareURL, targets); ok {
			base = inferred
		}
	}
	if base == "" {
		if !explicitSet {
			return config.ShareTarget{}, errors.New("cannot identify the originating Crit instance; re-add or confirm its base URL with --share-url")
		}
		selected, configured, selectErr := config.FindShareTarget(cfg, explicit)
		if selectErr != nil {
			return config.ShareTarget{}, selectErr
		}
		if !configured {
			return config.ShareTarget{}, errors.New("legacy review target confirmation requires a configured share target")
		}
		return selected, nil
	}
	if explicitSet {
		selected, ok, err := config.SelectShareTarget(explicit, true, cfg)
		if err != nil {
			return config.ShareTarget{}, err
		}
		if !ok || selected.URL != base {
			return config.ShareTarget{}, errors.New("this review is already shared to another target; unpublish it first")
		}
	}
	target, ok, err := config.FindShareTarget(cfg, base)
	if err != nil {
		return config.ShareTarget{}, err
	}
	if !ok {
		// Legacy root-host review files can still perform anonymous/delete-token
		// operations without borrowing credentials or transport from a default.
		// Resolve it as an explicit target so CRIT_AUTH_TOKEN still applies to the
		// already-bound instance, even when that instance is not in config.
		selected, selectedOK, selectErr := config.SelectShareTarget(base, true, cfg)
		if selectErr != nil {
			return config.ShareTarget{}, selectErr
		}
		if !selectedOK {
			return config.ShareTarget{}, errors.New("cannot select the originating Crit instance")
		}
		return selected, nil
	}
	return target, nil
}

func proxyAuthCLIError(command string) error {
	return fmt.Errorf("%s is unavailable for a target with proxy_auth enabled; use Crit's browser interface", command)
}

func runShareUnderLock(critPath string, files []ShareFile, sharePaths []string, svcURL, authToken, author, org, visibility string, showQR bool) error {
	lockedCfg, lockedOK, err := LoadExistingShareCfg(critPath, sharePaths)
	if err != nil {
		return err
	}
	if lockedOK {
		return runShareExisting(lockedCfg, critPath, files, sharePaths, svcURL, authToken, author, org, visibility, showQR)
	}
	return runShareNew(critPath, files, sharePaths, svcURL, authToken, author, org, visibility, showQR)
}

func parseFetchOutputDir(args []string) (outputDir, sessionID string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				return "", "", clicmd.Usage(fmt.Sprintf("Error: %s requires a value", arg))
			}
			i++
			outputDir = args[i]
		case arg == "--session":
			if i+1 >= len(args) {
				return "", "", clicmd.Usage("Error: --session requires a value")
			}
			i++
			sessionID = args[i]
		default:
			fmt.Fprintln(os.Stderr, "Usage: crit fetch [--session <id>] [--output <dir>]")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Fetches comments added on crit-web into the review file.")
			fmt.Fprintln(os.Stderr, "Requires a prior `crit share` so a share URL is recorded.")
			return "", "", clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
	}
	return outputDir, sessionID, nil
}

func resolveFetchReviewPath(args []string) (string, error) {
	outputDir, sessionID, err := parseFetchOutputDir(args)
	if err != nil {
		return "", err
	}
	cfg := LoadShareConfig()
	return review.ResolveCommandReviewPathWithSession(sessionID, outputDir, cfg.Output)
}

func printFetchedComments(webComments []WebComment) {
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

// RunFetch pulls remote comments from crit-web into the review file.
func RunFetch(args []string) error {
	if err := checkProxyAuthCLIAllowed("crit fetch"); err != nil {
		return err
	}
	critPath, err := resolveFetchReviewPath(args)
	if err != nil {
		return err
	}

	return session.WithShareLock(critPath, func() error {
		return runFetchUnderLock(critPath)
	})
}

func runFetchUnderLock(critPath string) error {
	data, readErr := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if readErr != nil {
		return clicmd.Usage("Error: no review file found. Run `crit share` first.")
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return fmt.Errorf("invalid review file: %w", err)
	}
	if cj.ShareURL == "" {
		return clicmd.Usage("Error: no share URL in review file. Run `crit share` first.")
	}

	target, err := resolveOperationTarget(LoadShareConfig(), "", false, cj, true)
	if err != nil {
		return err
	}
	if target.ProxyAuth {
		return proxyAuthCLIError("crit fetch")
	}
	authToken := target.Auth.Token
	localIDs := BuildLocalIDSet(cj)
	localFingerprints, localFingerprintIDs := BuildLocalFingerprintIndex(cj)

	fetched, err := FetchWebCommentsFromTarget(cj.ShareURL, target.URL, localIDs, localFingerprints, localFingerprintIDs, authToken)
	if err != nil {
		if errors.Is(err, ErrShareUnauthorized) {
			handleShareAuthError(target.URL)
			return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
		return fmt.Errorf("fetching remote comments: %w", err)
	}

	if len(fetched.NewComments) == 0 && len(fetched.ReplyUpdates) == 0 {
		fmt.Println("No new comments.")
		fmt.Printf("Review file: %s\n", session.ReviewPathsFor(critPath).Review)
		return nil
	}

	if err := MergeWebComments(critPath, fetched.NewComments, fetched.ReplyUpdates); err != nil {
		return fmt.Errorf("saving review file: %w", err)
	}
	if cj.ShareBaseURL == "" {
		_ = bindShareBaseURL(critPath, target.URL)
	}

	printFetchedComments(fetched.NewComments)
	if len(fetched.ReplyUpdates) > 0 {
		replyCount := 0
		for _, replies := range fetched.ReplyUpdates {
			replyCount += len(replies)
		}
		fmt.Printf("Updated %d comment(s) with %d new reply(ies).\n", len(fetched.ReplyUpdates), replyCount)
	}
	fmt.Printf("Review file: %s\n", session.ReviewPathsFor(critPath).Review)
	return nil
}

func parseUnpublishFlags(args []string) (unpublishFlags, error) {
	var f unpublishFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				return f, clicmd.Usage(fmt.Sprintf("Error: %s requires a value", arg))
			}
			i++
			f.outputDir = args[i]
		case arg == "--session":
			if i+1 >= len(args) {
				return f, clicmd.Usage("Error: --session requires a value")
			}
			i++
			f.sessionID = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				return f, clicmd.Usage("Error: --share-url requires a value")
			}
			i++
			f.svcURL = args[i]
			f.svcURLSet = true
		default:
			f.files = append(f.files, arg)
		}
	}
	return f, nil
}

func applyUnpublishConfigDefaults(f *unpublishFlags, cfg config.Config) {
	f.configuredOutput = cfg.Output
}

// RunUnpublish removes a shared review from crit-web.
func RunUnpublish(args []string) error {
	if err := checkProxyAuthCLIAllowed("crit unpublish"); err != nil {
		return err
	}
	f, err := parseUnpublishFlags(args)
	if err != nil {
		return err
	}

	unpubCfg := LoadShareConfig()
	applyUnpublishConfigDefaults(&f, unpubCfg)

	critPath, err := review.ResolveCommandReviewPathWithSessionArgs(f.sessionID, f.outputDir, f.configuredOutput, f.files)
	if err != nil {
		return err
	}
	data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if err != nil {
		return clicmd.Usage("Error: no review file found. Nothing to unpublish.")
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return fmt.Errorf("invalid review file: %w", err)
	}
	if cj.DeleteToken == "" {
		fmt.Fprintln(os.Stderr, "No shared review found — nothing to unpublish.")
		return nil
	}
	target, err := resolveOperationTarget(unpubCfg, f.svcURL, f.svcURLSet, cj, true)
	if err != nil {
		return err
	}
	if target.ProxyAuth {
		return proxyAuthCLIError("crit unpublish")
	}
	f.svcURL = target.URL
	unpubAuthToken := target.Auth.Token

	if err := UnpublishFromWeb(f.svcURL, cj.DeleteToken, unpubAuthToken); err != nil {
		return err
	}

	if err := ClearShareState(critPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clear share state: %v\n", err)
	}

	fmt.Println("Review unpublished.")
	return nil
}

func mustGetwd() (string, error) {
	return clicmd.MustGetwd()
}
