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
	outputDir  string
	svcURL     string
	showQR     bool
	org        string
	visibility string
	preview    string
	files      []string
}

func postPreviewShare(htmlPath, svcURL, authToken string) (string, error) {
	files, err := session.CrawlPreview(htmlPath)
	if err != nil {
		return "", fmt.Errorf("crawling preview assets: %w", err)
	}

	payload := BuildSharePayload(files, nil, 1, nil, "", "", "preview")
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

	client := &http.Client{Timeout: 30 * time.Second}
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
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				return sf, clicmd.Usage(fmt.Sprintf("Error: %s requires a value", arg))
			}
			i++
			sf.outputDir = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				return sf, clicmd.Usage("Error: --share-url requires a value")
			}
			i++
			sf.svcURL = args[i]
		case arg == "--preview":
			if i+1 >= len(args) {
				return sf, clicmd.Usage("Error: --preview requires an HTML file path")
			}
			i++
			sf.preview = args[i]
		case arg == "--qr":
			sf.showQR = true
		case arg == "--org":
			if i+1 >= len(args) {
				return sf, clicmd.Usage("Error: --org requires a value")
			}
			i++
			sf.org = args[i]
		case arg == "--visibility":
			if i+1 >= len(args) {
				return sf, clicmd.Usage("Error: --visibility requires a value")
			}
			i++
			sf.visibility = args[i]
		default:
			sf.files = append(sf.files, arg)
		}
	}
	return sf, nil
}

func runSharePreview(sf shareFlags) error {
	if len(sf.files) > 0 {
		return clicmd.Usage("Error: --preview cannot be combined with file arguments")
	}
	cfg := LoadShareConfig()
	svcURL := ResolveShareURL(sf.svcURL, cfg, config.DefaultShareURL)
	url, err := postPreviewShare(sf.preview, svcURL, ResolveAuthToken(cfg))
	if err != nil {
		return err
	}
	fmt.Println(url)
	return nil
}

func shareUsageError() error {
	fmt.Fprintln(os.Stderr, "Usage: crit share [--output <dir>] [--share-url <url>] [--org <slug>] [--visibility <level>] [--qr] <file> [file...]")
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
		fmt.Println()
		qrterminal.GenerateWithConfig(url, qrterminal.Config{
			Level:      qrterminal.L,
			Writer:     os.Stdout,
			HalfBlocks: true,
			QuietZone:  1,
		})
	}
}

func handleShareAuthError() {
	auth.ClearAuthIdentity()
	fmt.Fprintln(os.Stderr, "Auth token rejected by server; cleared local credentials. Run 'crit auth login' to re-authenticate.")
}

func runShareExisting(existingCfg session.CritJSON, critPath string, files []ShareFile, sharePaths []string, authToken, fallbackAuthor string, showQR bool) error {
	localIDs := BuildLocalIDSet(existingCfg)
	localFingerprints, localFingerprintIDs := BuildLocalFingerprintIndex(existingCfg)
	if fetched, err := FetchWebComments(existingCfg.ShareURL, localIDs, localFingerprints, localFingerprintIDs, authToken); err != nil {
		if errors.Is(err, ErrShareUnauthorized) {
			handleShareAuthError()
			return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
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
			handleShareAuthError()
		}
		return err
	}

	if err := UpdateShareState(critPath, ComputeShareHash(files, allComments), result.ReviewRound); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state: %v\n", err)
	}
	if result.Changed {
		fmt.Printf("Updated (round %d): %s\n", result.ReviewRound, result.URL)
	} else {
		fmt.Println(existingCfg.ShareURL)
	}

	printQR(result.URL, showQR)
	return nil
}

func runShareNew(critPath string, files []ShareFile, filePaths []string, svcURL, authToken, fallbackAuthor, org, visibility string, showQR bool) error {
	res, err := ShareReviewFiles(critPath, files, filePaths, svcURL, authToken, fallbackAuthor, org, visibility, "")
	if err != nil {
		if errors.Is(err, ErrShareUnauthorized) {
			handleShareAuthError()
		}
		return err
	}

	if err := PersistShareState(critPath, res.URL, res.DeleteToken, ShareScope(filePaths), org, "", visibility); err != nil {
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

	flagURL := sf.svcURL != ""

	cfg := LoadShareConfig()
	sf.svcURL = ResolveShareURL(sf.svcURL, cfg, config.DefaultShareURL)
	cfg.AuthToken = ResolveAuthToken(cfg)
	auth.LazyBackfillAuthUserID(&cfg, sf.svcURL)
	authToken := cfg.AuthToken

	files, err := loadShareFiles(sf.files)
	if err != nil {
		return err
	}

	critPath, err := review.ResolveReviewPath(sf.outputDir)
	if err != nil {
		return err
	}
	if err := CheckShareAllowed(critPath); err != nil {
		return err
	}

	sharePaths := make([]string, len(files))
	for i, f := range files {
		sharePaths[i] = f.Path
	}

	existingCfg, ok, err := LoadExistingShareCfg(critPath, sharePaths)
	if err != nil {
		return err
	}
	if !ok && config.NeedsShareConsent(cfg, sf.svcURL) {
		if !promptShareConsent(os.Stderr, os.Stdin) {
			return nil
		}
		if err := config.SaveGlobalConfig(func(m map[string]json.RawMessage) error {
			m["share_consented"] = json.RawMessage("true")
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not save consent: %v\n", err)
		}
		cfg.ShareConsented = true
	}
	if flagURL && term.IsTerminal(int(os.Stdin.Fd())) {
		if !promptShareURLConfirm(os.Stderr, os.Stdin, sf.svcURL) {
			return nil
		}
	}
	if ok {
		return runShareExisting(existingCfg, critPath, files, sharePaths, authToken, cfg.Author, sf.showQR)
	}

	return runShareNew(critPath, files, sharePaths, sf.svcURL, authToken, cfg.Author, sf.org, sf.visibility, sf.showQR)
}

func parseFetchOutputDir(args []string) (string, error) {
	outputDir := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				return "", clicmd.Usage(fmt.Sprintf("Error: %s requires a value", arg))
			}
			i++
			outputDir = args[i]
		default:
			fmt.Fprintln(os.Stderr, "Usage: crit fetch [--output <dir>]")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Fetches comments added on crit-web into the review file.")
			fmt.Fprintln(os.Stderr, "Requires a prior `crit share` so a share URL is recorded.")
			return "", clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
	}
	return outputDir, nil
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
	outputDir, err := parseFetchOutputDir(args)
	if err != nil {
		return err
	}

	critPath, err := review.ResolveReviewPath(outputDir)
	if err != nil {
		return err
	}

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

	authToken := ResolveAuthToken(LoadShareConfig())
	localIDs := BuildLocalIDSet(cj)
	localFingerprints, localFingerprintIDs := BuildLocalFingerprintIndex(cj)

	fetched, err := FetchWebComments(cj.ShareURL, localIDs, localFingerprints, localFingerprintIDs, authToken)
	if err != nil {
		if errors.Is(err, ErrShareUnauthorized) {
			handleShareAuthError()
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

// RunUnpublish removes a shared review from crit-web.
func RunUnpublish(args []string) error {
	unpubOutputDir := ""
	unpubSvcURL := ""
	var unpubFiles []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				return clicmd.Usage(fmt.Sprintf("Error: %s requires a value", arg))
			}
			i++
			unpubOutputDir = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				return clicmd.Usage("Error: --share-url requires a value")
			}
			i++
			unpubSvcURL = args[i]
		default:
			unpubFiles = append(unpubFiles, arg)
		}
	}

	unpubCfg := LoadShareConfig()
	unpubSvcURL = ResolveShareURL(unpubSvcURL, unpubCfg, config.DefaultShareURL)
	unpubAuthToken := ResolveAuthToken(unpubCfg)

	critPath, err := review.ResolveReviewPathWithArgs(unpubOutputDir, unpubFiles)
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

	if err := UnpublishFromWeb(unpubSvcURL, cj.DeleteToken, unpubAuthToken); err != nil {
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
