package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	qrterminal "github.com/mdp/qrterminal/v3"
)

//go:embed frontend/*
var frontendFS embed.FS

//go:embed integrations/*
var integrationsFS embed.FS

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// commandDispatch maps subcommand names to handler functions.
var commandDispatch = map[string]func([]string){
	"help":      func([]string) { printHelp() },
	"--help":    func([]string) { printHelp() },
	"-h":        func([]string) { printHelp() },
	"--version": func([]string) { printVersion() },
	"-v":        func([]string) { printVersion() },
	"share":     runShare,
	"fetch":     runFetch,
	"unpublish": runUnpublish,
	"install":   runInstall,
	"config":    runConfig,
	"check":     func([]string) { runCheck() },
	"pull":      runPull,
	"push":      runPush,
	"comment":   runComment,
	"review":    runReview,
	"plan":      runPlan,
	"plan-hook": func([]string) { runPlanHook() },
	"auth":      runAuth,
	"stop":      runStop,
	"status":    runStatus,
	"cleanup":   runCleanup,
	"_serve":    runServe,
}

func main() {
	if len(os.Args) < 2 {
		runReview(nil)
		return
	}
	if handler, ok := commandDispatch[os.Args[1]]; ok {
		handler(os.Args[2:])
		return
	}
	runReview(os.Args[1:])
}

type shareFlags struct {
	outputDir string
	svcURL    string
	showQR    bool
	files     []string
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
		case arg == "--qr":
			sf.showQR = true
		default:
			sf.files = append(sf.files, arg)
		}
	}
	return sf
}

func printShareUsage() {
	fmt.Fprintln(os.Stderr, "Usage: crit share [--output <dir>] [--share-url <url>] [--qr] <file> [file...]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Shares files to crit-web and prints the review URL.")
	fmt.Fprintln(os.Stderr, "Comments from .crit.json are included automatically.")
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
			if wd, err := os.Getwd(); err == nil {
				if rel, err := filepath.Rel(wd, path); err == nil {
					relPath = rel
				}
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

func runShareExisting(existingCfg CritJSON, critPath string, files []shareFile, sharePaths []string, authToken string, showQR bool) {
	localIDs := buildLocalIDSet(existingCfg)
	localFingerprints := buildLocalFingerprints(existingCfg)
	if webComments, err := fetchNewWebComments(existingCfg.ShareURL, localIDs, localFingerprints, authToken); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not pull remote comments: %v\n", err)
	} else if len(webComments) > 0 {
		if err := mergeWebComments(critPath, webComments); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not merge remote comments: %v\n", err)
		}
	}

	allComments, _ := loadCommentsForUpsert(critPath, sharePaths)

	result, err := upsertShareToWeb(existingCfg, files, allComments, authToken)
	if err != nil {
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

func runShareNew(critPath string, files []shareFile, filePaths []string, svcURL, authToken string, showQR bool) {
	comments, reviewRound := loadCommentsForShare(critPath, filePaths)

	url, deleteToken, err := shareFilesToWeb(files, comments, svcURL, reviewRound, authToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := persistShareState(critPath, url, deleteToken, shareScope(filePaths)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state to .crit.json: %v\n", err)
	}

	initialComments, _ := loadCommentsForUpsert(critPath, filePaths)
	_ = updateShareState(critPath, computeShareHash(files, initialComments), reviewRound)

	fmt.Println(url)
	printQR(url, showQR)

	if authToken == "" {
		showLoginHint()
	}
}

func runShare(args []string) {
	sf := parseShareFlags(args)

	if len(sf.files) == 0 {
		printShareUsage()
	}

	cfg := loadShareConfig()
	sf.svcURL = resolveShareURL(sf.svcURL, cfg, defaultShareURL)
	authToken := resolveAuthToken(cfg)

	files := loadShareFiles(sf.files)

	critPath, err := resolveReviewPath(sf.outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	sharePaths := make([]string, len(files))
	for i, f := range files {
		sharePaths[i] = f.Path
	}

	if existingCfg, ok := loadExistingShareCfg(critPath, sharePaths); ok {
		runShareExisting(existingCfg, critPath, files, sharePaths, authToken, sf.showQR)
		return
	}

	runShareNew(critPath, files, sharePaths, sf.svcURL, authToken, sf.showQR)
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
			fmt.Fprintln(os.Stderr, "Fetches comments added on crit-web into .crit.json.")
			fmt.Fprintln(os.Stderr, "Requires a prior `crit share` so a share URL is recorded.")
			os.Exit(1)
		}
	}
	return outputDir
}

func printFetchedComments(webComments []webComment) {
	fmt.Printf("Fetched %d new comment(s) into .crit.json\n", len(webComments))
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

// highestWebIndex returns the highest numeric suffix among "web-N" comment IDs
// in a CritJSON structure. This ensures new web comment IDs are globally unique.
func highestWebIndex(cj CritJSON) int {
	max := 0
	for _, f := range cj.Files {
		for _, c := range f.Comments {
			if strings.HasPrefix(c.ID, "web-") {
				if n, err := strconv.Atoi(strings.TrimPrefix(c.ID, "web-")); err == nil && n > max {
					max = n
				}
			}
		}
	}
	for _, c := range cj.ReviewComments {
		if strings.HasPrefix(c.ID, "web-") {
			if n, err := strconv.Atoi(strings.TrimPrefix(c.ID, "web-")); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}

func runFetch(args []string) {
	outputDir := parseFetchOutputDir(args)

	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	data, readErr := os.ReadFile(critPath)
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
	localFingerprints := buildLocalFingerprints(cj)

	webComments, err := fetchNewWebComments(cj.ShareURL, localIDs, localFingerprints, authToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching remote comments: %v\n", err)
		os.Exit(1)
	}

	if len(webComments) == 0 {
		fmt.Println("No new comments.")
		return
	}

	// Merge web comments into the review file
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
	}
	webCount := highestWebIndex(cj)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, wc := range webComments {
		webCount++
		c := Comment{
			ID:          fmt.Sprintf("web-%d", webCount),
			StartLine:   wc.StartLine,
			EndLine:     wc.EndLine,
			Body:        wc.Body,
			Quote:       wc.Quote,
			Author:      wc.AuthorDisplayName,
			Scope:       wc.Scope,
			ReviewRound: wc.ReviewRound,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if wc.Scope == "review" {
			cj.ReviewComments = append(cj.ReviewComments, c)
		} else {
			entry := cj.Files[wc.FilePath]
			entry.Comments = append(entry.Comments, c)
			cj.Files[wc.FilePath] = entry
		}
	}
	cj.UpdatedAt = now
	if err := saveCritJSON(critPath, cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving review file: %v\n", err)
		os.Exit(1)
	}

	printFetchedComments(webComments)
}

func runUnpublish(args []string) {
	unpubOutputDir := ""
	unpubSvcURL := ""
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
			fmt.Fprintf(os.Stderr, "Usage: crit unpublish [--output <dir>] [--share-url <url>]\n")
			os.Exit(1)
		}
	}

	unpubCfg := loadShareConfig()
	unpubSvcURL = resolveShareURL(unpubSvcURL, unpubCfg, defaultShareURL)
	unpubAuthToken := resolveAuthToken(unpubCfg)

	critPath, err := resolveReviewPath(unpubOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(critPath)
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

func runInstall(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: crit install <agent>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Available agents:")
		for _, a := range availableIntegrations() {
			fmt.Fprintf(os.Stderr, "  %s\n", a)
		}
		fmt.Fprintln(os.Stderr, "  all")
		os.Exit(1)
	}

	force := false
	for _, arg := range args[1:] {
		if arg == "--force" || arg == "-f" {
			force = true
		}
	}

	target := args[0]
	if target == "all" {
		for _, name := range availableIntegrations() {
			installIntegration(name, force)
		}
	} else {
		installIntegration(target, force)
	}
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
	if IsGitRepo() {
		configDir, _ = RepoRoot()
	}
	if configDir == "" {
		configDir, _ = os.Getwd()
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

func runPull(args []string) {
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

	ghComments, err := fetchPRComments(prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	critPath, err := resolveReviewPath(f.outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var cj CritJSON
	if data, readErr := os.ReadFile(critPath); readErr == nil {
		if jsonErr := json.Unmarshal(data, &cj); jsonErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: existing review file is invalid, starting fresh: %v\n", jsonErr)
		}
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
		cj.Branch = CurrentBranch()
		cfg := LoadConfig("")
		base := cfg.BaseBranch
		if base == "" {
			base = DefaultBranch()
		}
		cj.BaseRef, _ = MergeBase(base)
		cj.ReviewRound = 1
	}

	added := mergeGHComments(&cj, ghComments)

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

func displayPushDryRun(ghComments []map[string]interface{}, allReplies []ghReplyForPush, prNumber int, event, message string) {
	displayEvent := strings.ToLower(strings.ReplaceAll(event, "_", "-"))
	fmt.Printf("Would post %d comments to PR #%d (event: %s):\n\n", len(ghComments), prNumber, displayEvent)
	if message != "" {
		fmt.Printf("  Review body: %s\n\n", message)
	}
	for _, c := range ghComments {
		path := c["path"].(string)
		line := c["line"].(int)
		body := c["body"].(string)
		if sl, ok := c["start_line"]; ok {
			fmt.Printf("  %s:%d-%d\n", path, sl.(int), line)
		} else {
			fmt.Printf("  %s:%d\n", path, line)
		}
		fmt.Printf("    %s\n\n", body)
	}
	for _, reply := range allReplies {
		fmt.Printf("  Would reply to GitHub comment %d: %.60s\n", reply.ParentGHID, reply.Body)
	}
}

func postPushReplies(prNumber int, allReplies []ghReplyForPush) map[replyKey]int64 {
	replyCount := 0
	replyIDs := make(map[replyKey]int64)
	for _, reply := range allReplies {
		replyID, err := postGHReply(prNumber, reply.ParentGHID, reply.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to post reply: %v\n", err)
		} else {
			replyCount++
			if replyID != 0 {
				replyIDs[replyKey{ParentGHID: reply.ParentGHID, BodyPrefix: truncateStr(reply.Body, 60)}] = replyID
			}
		}
	}
	if replyCount > 0 {
		fmt.Printf("Posted %d replies\n", replyCount)
	}
	return replyIDs
}

func runPush(args []string) {
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
	data, err := os.ReadFile(critPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no review file found. Run a crit review first.\n")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid review file: %v\n", err)
		os.Exit(1)
	}

	ghComments := critJSONToGHComments(cj)
	if len(ghComments) == 0 && event == "COMMENT" {
		fmt.Println("No unresolved comments to push.")
		return
	}

	var allReplies []ghReplyForPush
	for _, cf := range cj.Files {
		allReplies = append(allReplies, collectNewRepliesForPush(cf)...)
	}

	if f.dryRun {
		displayPushDryRun(ghComments, allReplies, prNumber, event, f.message)
		return
	}

	displayEvent := strings.ToLower(strings.ReplaceAll(event, "_", "-"))
	fmt.Printf("Pushing %d comments to PR #%d (%s)...\n", len(ghComments), prNumber, displayEvent)
	commentIDs, err := createGHReview(prNumber, ghComments, f.message, event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Posted %d review comments to PR #%d (%s)\n", len(ghComments), prNumber, displayEvent)

	replyIDs := postPushReplies(prNumber, allReplies)

	if err := updateCritJSONWithGitHubIDs(critPath, commentIDs, replyIDs); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update .crit.json with GitHub IDs: %v\n", err)
	}
}

type commentFlags struct {
	outputDir string
	author    string
	replyTo   string
	resolve   bool
	path      string
	json      bool
	plan      string
	args      []string
}

func parseCommentFlags(args []string) commentFlags {
	var f commentFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--plan" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --plan requires a slug\n")
				os.Exit(1)
			}
			i++
			f.plan = args[i]
		} else if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			f.outputDir = args[i]
		} else if arg == "--author" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --author requires a value\n")
				os.Exit(1)
			}
			i++
			f.author = args[i]
		} else if arg == "--reply-to" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --reply-to requires a comment ID\n")
				os.Exit(1)
			}
			i++
			f.replyTo = args[i]
		} else if arg == "--resolve" {
			f.resolve = true
		} else if arg == "--path" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --path requires a value\n")
				os.Exit(1)
			}
			i++
			f.path = args[i]
		} else if arg == "--json" {
			f.json = true
		} else {
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

	// Resolve author: --author flag > config > git user.name
	if f.author == "" {
		cfgDir, _ := os.Getwd()
		if IsGitRepo() {
			cfgDir, _ = RepoRoot()
		}
		cfg := LoadConfig(cfgDir)
		f.author = cfg.Author
	}
}

func runCommentJSON(f commentFlags) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	var entries []BulkCommentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
		os.Exit(1)
	}

	if err := bulkAddCommentsToCritJSON(entries, f.author, f.outputDir); err != nil {
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
		parts = append(parts, fmt.Sprintf("%d repl%s", replies, pluralReply(replies)))
	}
	fmt.Printf("Added %s\n", strings.Join(parts, " and "))
}

func runCommentReply(f commentFlags) {
	if len(f.args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: crit comment --reply-to <comment-id> [--resolve] <body>")
		os.Exit(1)
	}
	replyBody := strings.Join(f.args, " ")
	if err := addReplyToCritJSON(f.replyTo, replyBody, f.author, f.resolve, f.outputDir, f.path); err != nil {
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
	fmt.Println("Cleared .crit.json")
}

func printCommentUsage() {
	fmt.Fprintln(os.Stderr, "Usage: crit comment [--output <dir>] [--author <name>] <body>                    Review-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] [--author <name>] <path> <body>             File-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] [--author <name>] <path>:<line[-end]> <body> Line-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment --reply-to <id> [--resolve] [--author <name>] <body>")
	fmt.Fprintln(os.Stderr, "       crit comment --json [--author <name>] [--output <dir>]    Read comments from stdin as JSON")
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
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Tips:")
	fmt.Fprintln(os.Stderr, "  Use --author to identify who left the comment (recommended for AI agents)")
	fmt.Fprintln(os.Stderr, "  Use single quotes for the body to avoid shell interpretation of backticks")
	fmt.Fprintln(os.Stderr, "  Use --json for bulk operations (multiple comments/replies in one atomic write)")
	os.Exit(1)
}

func runCommentLineLevel(loc string, commentArgs []string, author, outputDir string) {
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
	if err := addCommentToCritJSON(filePath, startLine, endLine, body, author, outputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added comment on %s:%s\n", filePath, lineSpec)
}

func runComment(args []string) {
	f := parseCommentFlags(args)
	resolveCommentFlags(&f)

	if f.json {
		runCommentJSON(f)
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
		if err := addReviewCommentToCritJSON(body, f.author, f.outputDir); err != nil {
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
		runCommentLineLevel(loc, f.args, f.author, f.outputDir)
		return
	}

	// 2+ args without colon line spec: check if first arg is a file path
	if len(f.args) >= 2 {
		candidatePath := f.args[0]
		if fileExistsOnDiskOrSession(candidatePath, f.outputDir) {
			body := strings.Join(f.args[1:], " ")
			if err := addFileCommentToCritJSON(candidatePath, body, f.author, f.outputDir); err != nil {
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

// fileExistsOnDiskOrSession checks if a path exists as a file on disk or in .crit.json.
func fileExistsOnDiskOrSession(path string, outputDir string) bool {
	// Check disk first (relative to cwd)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return true
	}
	// Check in repo root if we're in a git repo
	if IsGitRepo() {
		if root, err := RepoRoot(); err == nil {
			absPath := filepath.Join(root, path)
			if info, err := os.Stat(absPath); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	// Check if it exists in .crit.json
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
	noOpen        bool
	quiet         bool
}

func resolvePlanConfig(args []string) planConfig {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	name := fs.String("name", "", "Plan name/slug for session identification")
	port := fs.Int("port", 0, "Port to listen on")
	fs.IntVar(port, "p", 0, "Port (shorthand)")
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status (shorthand)")
	fs.Parse(args)

	pc := planConfig{
		name:   *name,
		port:   *port,
		noOpen: *noOpen,
		quiet:  *quiet,
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
		fmt.Fprintf(os.Stderr, "Connected to crit daemon on port %d\n", entry.Port)
		if !noOpen && !daemonHasBrowser(entry) {
			go openBrowser(fmt.Sprintf("http://localhost:%d", entry.Port))
		}
		return entry, false
	}

	var err error
	entry, err = startDaemon(key, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Started crit daemon on port %d (PID %d)\n", entry.Port, entry.PID)
	return entry, true
}

func installDaemonSignalHandler(pid int) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if proc, err := os.FindProcess(pid); err == nil {
			proc.Signal(syscall.SIGTERM)
		}
		os.Exit(0)
	}()
}

func killDaemonOnApproval(approved bool, pid int) {
	if approved {
		if proc, err := os.FindProcess(pid); err == nil {
			proc.Signal(syscall.SIGTERM)
		}
	}
}

// cleanupOnApproval deletes the review file when the review is approved
// and cleanup is enabled.
func cleanupOnApproval(approved bool, reviewPath string, cleanupEnabled bool) {
	if approved && cleanupEnabled && reviewPath != "" {
		os.Remove(reviewPath)
	}
}

func runPlan(args []string) {
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
	daemonArgs := buildPlanDaemonArgs(currentPath, storageDir, slug, pc.port, pc.noOpen, pc.quiet)

	entry, weStartedDaemon := connectOrStartDaemon(key, daemonArgs, pc.noOpen)

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved := runReviewClient(entry)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, LoadConfig(cwd).CleanupOnApproveEnabled())
}

type planHookEvent struct {
	SessionID string `json:"session_id"`
	ToolInput struct {
		Plan string `json:"plan"`
	} `json:"tool_input"`
}

func resolveHookSlug(event planHookEvent, content []byte) string {
	if event.SessionID != "" {
		if existing, ok := lookupPlanSlug(event.SessionID); ok {
			return existing
		}
		slug := resolveSlug(content)
		if err := savePlanSlug(event.SessionID, slug); err != nil {
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

// runPlanHook is the PermissionRequest hook handler for ExitPlanMode.
// It reads the hook event JSON from stdin, extracts the plan content,
// opens a crit review session, and writes a hookSpecificOutput JSON
// decision (allow/deny) to stdout.
func runPlanHook() {
	var event planHookEvent
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not parse stdin: %v\n", err)
		return
	}
	if strings.TrimSpace(event.ToolInput.Plan) == "" {
		return
	}

	content := []byte(event.ToolInput.Plan)
	slug := resolveHookSlug(event, content)

	storageDir, err := planStorageDir(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: error resolving storage dir: %v\n", err)
		return
	}

	ver, err := savePlanVersion(storageDir, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: error saving plan: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "crit plan-hook: plan '%s' saved as v%03d\n", slug, ver)

	cwd, _ := resolvedCWD()
	key := planSessionKey(cwd, slug)
	currentPath := filepath.Join(storageDir, "current.md")
	daemonArgs := buildPlanDaemonArgs(currentPath, storageDir, slug, 0, false, false)

	entry, alive := findAliveSession(key)
	weStartedDaemon := false

	if alive {
		fmt.Fprintf(os.Stderr, "crit plan-hook: connected to daemon on port %d\n", entry.Port)
		if !daemonHasBrowser(entry) {
			go openBrowser(fmt.Sprintf("http://localhost:%d", entry.Port))
		}
	} else {
		entry, err = startDaemon(key, daemonArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "crit plan-hook: error starting daemon: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "crit plan-hook: started daemon on port %d (PID %d)\n", entry.Port, entry.PID)
		weStartedDaemon = true
	}

	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved, prompt := runReviewClientRaw(entry)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, LoadConfig(cwd).CleanupOnApproveEnabled())
	emitHookDecision(approved, prompt)
}

// waitForDaemonReady polls the daemon's /api/session endpoint until it stops
// returning 503 Service Unavailable (session not yet initialized). Returns the
// last response status code and body, or an error if the daemon is unreachable
// or the 5-minute deadline expires.
func waitForDaemonReady(client *http.Client, port int) (statusCode int, body []byte, err error) {
	deadline := time.Now().Add(5 * time.Minute)
	for {
		resp, reqErr := client.Get(fmt.Sprintf("http://localhost:%d/api/session", port))
		if reqErr != nil {
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
func runReviewClientRaw(entry sessionEntry) (approved bool, prompt string) {
	client := &http.Client{Timeout: 24 * time.Hour}

	// Wait for the server to finish initializing before calling review-cycle.
	if _, _, err := waitForDaemonReady(client, entry.Port); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: %v\n", err)
		return true, ""
	}

	resp, err := client.Post(
		fmt.Sprintf("http://localhost:%d/api/review-cycle", entry.Port),
		"application/json",
		nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not reach daemon: %v\n", err)
		return true, "" // allow through on error
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "crit plan-hook: daemon returned %d\n", resp.StatusCode)
		return true, "" // allow through on infrastructure error
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return true, ""
	}

	var result struct {
		Approved bool   `json:"approved"`
		Prompt   string `json:"prompt"`
	}
	json.Unmarshal(body, &result)
	return result.Approved, result.Prompt
}

func runReview(args []string) {
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
	if IsGitRepo() {
		branch = CurrentBranch()
	}
	key := sessionKey(cwd, branch, sc.files)

	// Check for running daemon with the same session key
	entry, alive := findAliveSession(key)
	weStartedDaemon := false

	if alive {
		fmt.Fprintf(os.Stderr, "Connected to crit daemon on port %d\n", entry.Port)
		// Re-open browser if no browser tab is connected (user closed it)
		if !sc.noOpen && !daemonHasBrowser(entry) {
			go openBrowser(fmt.Sprintf("http://localhost:%d", entry.Port))
		}
	} else {
		// Pass raw args to startDaemon — the _serve process parses them itself
		entry, err = startDaemon(key, args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Started crit daemon on port %d (PID %d)\n", entry.Port, entry.PID)
		weStartedDaemon = true
	}

	// If we started the daemon, clean it up on Ctrl+C
	if weStartedDaemon {
		installDaemonSignalHandler(entry.PID)
	}

	approved := runReviewClient(entry)
	killDaemonOnApproval(approved, entry.PID)
	cleanupOnApproval(approved, entry.ReviewPath, LoadConfig(cwd).CleanupOnApproveEnabled())
}

// runReviewClient connects to a running daemon/server, blocks until the user
// finishes reviewing, prints feedback to stdout, and returns whether the
// review was approved (no unresolved comments).
func runReviewClient(entry sessionEntry) (approved bool) {
	client := &http.Client{Timeout: 24 * time.Hour}

	// Wait for the server to finish initializing before calling review-cycle.
	statusCode, body, err := waitForDaemonReady(client, entry.Port)
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
		fmt.Sprintf("http://localhost:%d/api/review-cycle", entry.Port),
		"application/json",
		nil,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not reach crit daemon on port %d: %v\n", entry.Port, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGatewayTimeout {
		fmt.Fprintln(os.Stderr, "Timeout waiting for review")
		os.Exit(1)
	}

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		os.Exit(1)
	}

	// Print feedback to stdout
	os.Stdout.Write(body)

	// Check if the review was approved (no unresolved comments).
	var result struct {
		Approved bool `json:"approved"`
	}
	if json.Unmarshal(body, &result) == nil {
		return result.Approved
	}
	return false
}

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
	if IsGitRepo() {
		branch = CurrentBranch()
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

func pluralReply(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// serverConfig holds the resolved configuration for running the server.
// It combines CLI flags, environment variables, and config file settings.
type serverConfig struct {
	port               int
	noOpen             bool
	quiet              bool
	shareURL           string
	authToken          string
	outputDir          string
	author             string
	ignorePatterns     []string
	files              []string // explicit file arguments (empty = git mode)
	noIntegrationCheck bool
	noUpdateCheck      bool
	agentCmd           string
	planDir            string // managed storage directory for plan mode
	planName           string // display name for plan content
	cfg                Config // full resolved config for the settings panel
}

// serverFlagSet holds the parsed flag values before config resolution.
type serverFlagSet struct {
	port        int
	noOpen      bool
	showVersion bool
	shareURL    string
	outputDir   string
	quiet       bool
	noIgnore    bool
	baseBranch  string
	planDir     string
	planName    string
	fileArgs    []string
}

func parseServerFlags(args []string) serverFlagSet {
	fs := flag.NewFlagSet("crit", flag.ExitOnError)
	port := fs.Int("port", 0, "Port to listen on (default: random available port)")
	fs.IntVar(port, "p", 0, "Port to listen on (shorthand)")
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	showVersion := fs.Bool("version", false, "Print version and exit")
	fs.BoolVar(showVersion, "v", false, "Print version and exit (shorthand)")
	shareURL := fs.String("share-url", "", "Base URL of hosted Crit service for sharing reviews (overrides CRIT_SHARE_URL env var)")
	outputDir := fs.String("output", "", "Output directory for .crit.json (default: repo root or file directory)")
	fs.StringVar(outputDir, "o", "", "Output directory for .crit.json (shorthand)")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status output (shorthand)")
	noIgnore := fs.Bool("no-ignore", false, "Disable all ignore patterns from config files")
	baseBranch := fs.String("base-branch", "", "Base branch to diff against (overrides auto-detection)")
	planDir := fs.String("plan-dir", "", "")
	planName := fs.String("name", "", "")
	fs.Usage = func() {
		printHelp()
	}
	fs.Parse(args)

	return serverFlagSet{
		port:        *port,
		noOpen:      *noOpen,
		showVersion: *showVersion,
		shareURL:    *shareURL,
		outputDir:   *outputDir,
		quiet:       *quiet,
		noIgnore:    *noIgnore,
		baseBranch:  *baseBranch,
		planDir:     *planDir,
		planName:    *planName,
		fileArgs:    fs.Args(),
	}
}

func resolvePort(flagPort, cfgPort int) int {
	if flagPort != 0 {
		return flagPort
	}
	if envPort := os.Getenv("CRIT_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			return p
		}
	}
	return cfgPort
}

func applyConfigDefaults(sf *serverFlagSet, cfg Config) {
	sf.port = resolvePort(sf.port, cfg.Port)
	if !sf.noOpen && cfg.NoOpen {
		sf.noOpen = true
	}
	sf.shareURL = resolveShareURL(sf.shareURL, cfg, "")
	if !sf.quiet && cfg.Quiet {
		sf.quiet = true
	}
	if sf.outputDir == "" && cfg.Output != "" {
		sf.outputDir = cfg.Output
	}
	if sf.baseBranch == "" && cfg.BaseBranch != "" {
		sf.baseBranch = cfg.BaseBranch
	}
	if sf.baseBranch != "" {
		setDefaultBranchOverride(sf.baseBranch)
	}
}

// resolveServerConfig parses flags, loads config files, and resolves the
// final server configuration from all sources (CLI > env > config > defaults).
// Returns nil when the command should exit early (e.g. --version).
func resolveServerConfig(args []string) (*serverConfig, error) {
	sf := parseServerFlags(args)

	if sf.showVersion {
		printVersion()
		return nil, nil
	}

	configDir := ""
	if IsGitRepo() {
		configDir, _ = RepoRoot()
	}
	if configDir == "" {
		configDir, _ = os.Getwd()
	}
	cfg := LoadConfig(configDir)

	applyConfigDefaults(&sf, cfg)

	var ignorePatterns []string
	if !sf.noIgnore {
		ignorePatterns = cfg.IgnorePatterns
	}

	return &serverConfig{
		port:               sf.port,
		noOpen:             sf.noOpen,
		quiet:              sf.quiet,
		shareURL:           sf.shareURL,
		authToken:          cfg.AuthToken,
		outputDir:          sf.outputDir,
		author:             cfg.Author,
		ignorePatterns:     ignorePatterns,
		noIntegrationCheck: cfg.NoIntegrationCheck,
		noUpdateCheck:      cfg.NoUpdateCheck,
		agentCmd:           cfg.AgentCmd,
		files:              sf.fileArgs,
		planDir:            sf.planDir,
		planName:           sf.planName,
		cfg:                cfg,
	}, nil
}

func createSession(sc *serverConfig) (*Session, error) {
	if len(sc.files) == 0 {
		if !IsGitRepo() {
			return nil, fmt.Errorf("not in a git repository and no files specified")
		}
		return NewSessionFromGit(sc.ignorePatterns)
	}
	return NewSessionFromFiles(sc.files, sc.ignorePatterns)
}

func applySessionOverrides(session *Session, sc *serverConfig) {
	if sc.planDir != "" {
		applyPlanOverrides(session, sc.planDir, sc.planName)
		for _, f := range session.Files {
			f.Comments = []Comment{}
		}
		session.reviewComments = nil
		session.loadCritJSON()
	}
	if sc.outputDir != "" {
		abs, _ := filepath.Abs(sc.outputDir)
		session.OutputDir = abs
	}
}

func bindListener(port int) (net.Listener, error) {
	var listener net.Listener
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, nil
		}
		if port == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, err
}

func serveSessionKey(sc *serverConfig) string {
	cwd, _ := resolvedCWD()
	if sc.planDir != "" {
		return planSessionKey(cwd, sc.planName)
	}
	branch := ""
	if IsGitRepo() {
		branch = CurrentBranch()
	}
	return sessionKey(cwd, branch, sc.files)
}

func checkStaleIntegrations(sc *serverConfig, srv *Server, cwd string) {
	if sc.noIntegrationCheck || os.Getenv("CRIT_NO_INTEGRATION_CHECK") != "" {
		return
	}
	if home, err := os.UserHomeDir(); err == nil {
		stale := checkInstalledIntegrations(cwd, home)
		srv.staleIntegrations = stale
		if len(stale) > 0 {
			go printStaleWarnings(stale)
		}
	}
}

func runIdleTimeoutChecker(ctx context.Context, stop context.CancelFunc, idleMu *sync.Mutex, lastActivity *time.Time) {
	const idleTimeout = 1 * time.Hour
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			idleMu.Lock()
			idle := time.Since(*lastActivity)
			idleMu.Unlock()
			if idle >= idleTimeout {
				stop()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func runServe(args []string) {
	pipe := openReadyPipe()

	sc, err := resolveServerConfig(args)
	if err != nil {
		daemonFatal(pipe, "Error: %v", err)
	}
	if sc == nil {
		return
	}
	sc.quiet = true

	listener, err := bindListener(sc.port)
	if err != nil {
		daemonFatal(pipe, "Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	srv, err := NewServer(nil, frontendFS, sc.shareURL, sc.authToken, sc.author, version, addr.Port, sc.agentCmd)
	if err != nil {
		daemonFatal(pipe, "Error creating server: %v", err)
	}

	// Set config-dependent fields for the settings panel
	srv.cfg = sc.cfg
	cwd, _ := resolvedCWD()
	srv.projectDir = cwd
	if home, err := os.UserHomeDir(); err == nil {
		srv.homeDir = home
	}
	key := serveSessionKey(sc)
	branch := ""
	if IsGitRepo() {
		branch = CurrentBranch()
	}
	var reviewPath string
	if sc.outputDir != "" {
		abs, _ := filepath.Abs(sc.outputDir)
		reviewPath = filepath.Join(abs, ".crit.json")
	} else {
		reviewPath, _ = reviewFilePath(key)
	}
	srv.reviewPath = reviewPath
	if err := writeSessionFile(key, sessionEntry{
		PID:        os.Getpid(),
		Port:       addr.Port,
		CWD:        cwd,
		Args:       sc.files,
		Branch:     branch,
		ReviewPath: reviewPath,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		daemonFatal(pipe, "Error writing session file: %v", err)
	}

	var idleMu sync.Mutex
	lastActivity := time.Now()
	resetActivity := func() {
		idleMu.Lock()
		lastActivity = time.Now()
		idleMu.Unlock()
	}

	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resetActivity()
			srv.ServeHTTP(w, r)
		}),
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	go func() {
		if err := httpServer.Serve(listener); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
			stop()
		}
	}()

	signalReadiness(pipe, addr.Port)

	if !sc.noOpen {
		go openBrowser(fmt.Sprintf("http://localhost:%d", addr.Port))
	}

	go runIdleTimeoutChecker(ctx, stop, &idleMu, &lastActivity)

	type sessionResult struct {
		session *Session
		err     error
	}
	ch := make(chan sessionResult, 1)
	// NOTE: On timeout, the createSession goroutine will leak until its git
	// operations finish (no context is threaded into the git calls). This is
	// acceptable because the timeout path sets initErr, which triggers a full
	// server shutdown and process exit shortly after, cleaning up all goroutines.
	go func() {
		s, err := createSession(sc)
		ch <- sessionResult{s, err}
	}()

	var session *Session
	var initErr error
	select {
	case res := <-ch:
		session, initErr = res.session, res.err
	case <-time.After(2 * time.Minute):
		initErr = fmt.Errorf("session initialization timed out after 2 minutes")
	}
	if initErr != nil {
		log.Printf("Error: %v", initErr)
		srv.SetInitErr(initErr)
		<-ctx.Done()
		removeSessionFile(key)
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		return
	}
	applySessionOverrides(session, sc)
	session.CLIArgs = sc.files

	// Set centralized review file path
	if sc.outputDir == "" {
		session.ReviewFilePath = reviewPath
	}

	checkStaleIntegrations(sc, srv, cwd)

	if !sc.noUpdateCheck && os.Getenv("CRIT_NO_UPDATE_CHECK") == "" {
		go srv.CheckForUpdates()
	}
	srv.SetSession(session)

	if session.Mode == "git" {
		go func() {
			if prInfo := detectPRInfo(); prInfo != nil {
				srv.SetPRInfo(prInfo)
			}
		}()
	}

	watchStop := make(chan struct{})
	go session.Watch(watchStop)

	<-ctx.Done()
	close(watchStop)

	removeSessionFile(key)
	session.Shutdown()
	session.WriteFiles()

	if session.ReviewFilePath != "" {
		fmt.Fprintf(os.Stderr, "Review file: %s\n", session.ReviewFilePath)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)
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

	branch := ""
	if IsGitRepo() {
		branch = CurrentBranch()
	}

	sessions, keys := listSessionsForCWD(cwd)
	var matchedSession *sessionEntry
	for i, s := range sessions {
		if s.Branch == branch || (branch == "" && len(sessions) == 1) {
			matchedSession = &sessions[i]
			_ = keys[i]
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
	if _, statErr := os.Stat(revPath); statErr == nil {
		revExists = true
	}

	if jsonOutput {
		printStatusJSON(branch, revPath, revExists, matchedSession)
		return
	}

	printStatusHuman(branch, revPath, revExists, matchedSession)
}

func printStatusJSON(branch, revPath string, revExists bool, session *sessionEntry) {
	result := map[string]interface{}{
		"branch":             branch,
		"review_file":        revPath,
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
	data, err := os.ReadFile(revPath)
	if err != nil {
		return
	}
	var cj CritJSON
	if json.Unmarshal(data, &cj) != nil {
		return
	}
	result["round"] = cj.ReviewRound
	unresolved, resolved := countComments(cj)
	result["comments"] = map[string]int{
		"unresolved": unresolved,
		"resolved":   resolved,
	}
}

func printStatusHuman(branch, revPath string, revExists bool, session *sessionEntry) {
	if branch != "" {
		fmt.Printf("Branch:      %s\n", branch)
	}
	fmt.Printf("Review file: %s\n", revPath)
	if session != nil {
		fmt.Printf("Daemon:      running (PID %d, port %d)\n", session.PID, session.Port)
	} else {
		fmt.Println("Daemon:      not running")
	}
	if !revExists {
		return
	}
	data, err := os.ReadFile(revPath)
	if err != nil {
		return
	}
	var cj CritJSON
	if json.Unmarshal(data, &cj) != nil {
		return
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
		ageDays := int(s.age.Hours() / 24)
		branchInfo := ""
		if s.branch != "" {
			branchInfo = s.branch + ", "
		}
		fmt.Printf("  %s  (%s%d days old, %d comment%s)\n", s.path, branchInfo, ageDays, s.comments, plural(s.comments))
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
	key      string
	path     string
	branch   string
	age      time.Duration
	comments int
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
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(de.Name(), ".json")
		if activeSessions[key] {
			continue
		}
		if sr, ok := checkStaleReview(revDir, de, key, cutoff); ok {
			stale = append(stale, sr)
		}
	}
	return stale
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
		if err := os.Remove(s.path); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting %s: %v\n", s.path, err)
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

func printHelp() {
	fmt.Fprintf(os.Stderr, `crit — inline code review for AI agent workflows

Usage:
  crit                                       Auto-detect changed files via git
  crit <file|dir> [...]                      Review specific files or directories
  crit stop [files...]                       Stop the daemon for current directory (and args)
  crit stop --all                            Stop all daemons for current directory
  crit comment <path>:<line[-end]> <body>    Add a review comment to .crit.json
  crit comment --reply-to <id> [--resolve] [--author <name>] <body>  Reply to a comment
  crit comment --json [--author <name>] [--output <dir>]    Read comments from stdin as JSON
  crit comment --clear                       Remove all comments from .crit.json
  crit share <file> [file...]                Share files to crit-web and print the URL
  crit fetch [--output <dir>]               Fetch comments from crit-web into .crit.json
  crit unpublish                             Remove a shared review from crit-web
  crit pull [--output <dir>] [pr-number]     Fetch GitHub PR comments to .crit.json
  crit push [--dry-run] [--event <type>] [-m <msg>] [-o <dir>] [pr-number]  Post .crit.json comments to a GitHub PR
  crit plan --name <slug> <file>             Review a plan file (manages versioned copies)
  crit plan --name <slug>                    Read plan from stdin
  crit auth login                            Log in to crit-web via browser
  crit auth logout                           Log out and revoke token
  crit auth whoami                           Show current user info
  crit install <agent>                       Install integration files for an AI coding tool
  crit status [--json]                        Print session info (review file, daemon, comments)
  crit cleanup [--days N] [--force]           Delete stale review files (default: 7 days)
  crit check                                 Check if installed integrations are up to date
  crit config [--generate]                    Show resolved configuration
  crit help                                  Show this help message

  Agents:
    claude-code, cursor, opencode, windsurf, github-copilot, cline, all

Options:
  -p, --port <port>           Port to listen on (default: random)
  -o, --output <dir>          Output directory for .crit.json
      --no-open               Don't auto-open browser
      --no-ignore             Disable all file ignore patterns
  -q, --quiet                 Suppress status output
      --share-url <url>       Share service URL (e.g. https://crit.md or self-hosted)
      --base-branch <branch>  Base branch to diff against (overrides auto-detection)
      --qr                    Print QR code of share URL (with crit share)
  -v, --version               Print version

Environment:
  CRIT_SHARE_URL              Override the share service URL
  CRIT_PORT                   Override the default port
  CRIT_NO_UPDATE_CHECK        Disable update check on startup
  CRIT_AUTH_TOKEN              Override the auth token (skip login)
  CRIT_NO_INTEGRATION_CHECK   Disable integration staleness check

Configuration:
  Global config:   ~/.crit.config.json
  Project config:  .crit.config.json (in repo root)
  agent_cmd        Shell command to send comments to an AI agent (e.g. "claude -p")
  Run 'crit config' to see resolved configuration.

Learn more: https://crit.md
`)
}

func printConfigHelp() {
	fmt.Fprintf(os.Stderr, `crit config — show resolved configuration

Prints the merged configuration from global and project config files as JSON.
CLI flags and environment variables are not reflected in this output.

Config files:
  ~/.crit.config.json          Global config (applies to all projects)
  .crit.config.json            Project config (in repo root)

Precedence (highest to lowest):
  1. CLI flags / env vars
  2. Project config
  3. Global config
  4. Built-in defaults

Available keys:
  port              int       Port to listen on (default: random)
  no_open           bool      Don't auto-open browser (default: false)
  share_url         string    Share service URL
  quiet             bool      Suppress status output (default: false)
  output            string    Output directory for .crit.json
  author            string    Your name for comments (default: git config user.name)
  base_branch       string    Base branch to diff against (overrides auto-detection)
  ignore_patterns        []string  Gitignore-style patterns to exclude files from review
  no_integration_check   bool      Skip integration staleness check (default: false)
  agent_cmd              string    Shell command to send comments to an AI agent (e.g. "claude -p")
  auth_token             string    Authentication token for crit-web share service

Note: agent_cmd and auth_token are global-only (~/.crit.config.json).
Project-level .crit.config.json cannot override them for security reasons.

Ignore pattern syntax:
  *.lock            Match files by extension (anywhere in tree)
  vendor/           Match all files under a directory
  package-lock.json Match exact filename (anywhere in tree)
  generated/*.pb.go Match with path prefix (filepath.Match syntax)

Example config:
  {
    "port": 3456,
    "share_url": "https://crit.md",
    "ignore_patterns": ["*.lock", "*.min.js", "vendor/", "generated/"]
  }
`)
}

func printVersion() {
	line := "crit " + version
	var details []string
	if date != "unknown" {
		details = append(details, date)
	}
	if commit != "unknown" {
		short := commit
		if len(short) > 7 {
			short = short[:7]
		}
		details = append(details, short)
	}
	if len(details) > 0 {
		line += " (" + strings.Join(details, ", ") + ")"
	}
	fmt.Println(line)
	fmt.Println("Inline code review for AI agent workflows")
}

type integration struct {
	source string // path inside integrations/ embed
	dest   string // destination relative to cwd
	hint   string // usage hint printed after install
}

var integrationMap = map[string][]integration{
	"claude-code": {
		{source: "integrations/claude-code/skills/crit/SKILL.md", dest: ".claude/skills/crit/SKILL.md", hint: "Run /crit in Claude Code to start a review loop"},
		{source: "integrations/claude-code/skills/crit-cli/SKILL.md", dest: ".claude/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Claude Code agents when needed"},
	},
	"cursor": {
		{source: "integrations/cursor/skills/crit/SKILL.md", dest: ".cursor/skills/crit/SKILL.md", hint: "Run /crit in Cursor to start a review loop"},
		{source: "integrations/cursor/skills/crit-cli/SKILL.md", dest: ".cursor/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Cursor agents when needed"},
	},
	"opencode": {
		{source: "integrations/opencode/crit.md", dest: ".opencode/commands/crit.md", hint: "Run /crit in OpenCode to start a review loop"},
		{source: "integrations/opencode/SKILL.md", dest: ".opencode/skills/crit/SKILL.md", hint: "The crit skill is available to OpenCode agents when needed"},
	},
	"windsurf": {
		{source: "integrations/windsurf/crit.md", dest: ".windsurf/rules/crit.md", hint: "Windsurf will suggest Crit when writing plans"},
	},
	"github-copilot": {
		{source: "integrations/github-copilot/skills/crit/SKILL.md", dest: ".github/skills/crit/SKILL.md", hint: "Run /crit in GitHub Copilot to start a review loop"},
		{source: "integrations/github-copilot/skills/crit-cli/SKILL.md", dest: ".github/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to GitHub Copilot agents when needed"},
	},
	"cline": {
		{source: "integrations/cline/crit.md", dest: ".clinerules/crit.md", hint: "Cline will suggest Crit when writing plans"},
	},
	"codex": {
		{source: "integrations/codex/skills/crit/SKILL.md", dest: ".agents/skills/crit/SKILL.md", hint: "Use $crit in Codex to start a review loop"},
		{source: "integrations/codex/skills/crit-cli/SKILL.md", dest: ".agents/skills/crit-cli/SKILL.md", hint: "The crit-cli skill is available to Codex agents when needed"},
	},
}

func availableIntegrations() []string {
	return []string{"claude-code", "codex", "cursor", "opencode", "windsurf", "github-copilot", "cline"}
}

func installIntegration(name string, force bool) {
	files, ok := integrationMap[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown agent: %s\n\nAvailable agents:\n", name)
		for _, a := range availableIntegrations() {
			fmt.Fprintf(os.Stderr, "  %s\n", a)
		}
		os.Exit(1)
	}

	var hints []string
	for _, f := range files {
		if !force {
			if _, err := os.Stat(f.dest); err == nil {
				fmt.Printf("  Skipped:   %s (already exists, use --force to overwrite)\n", f.dest)
				if f.hint != "" {
					hints = append(hints, f.hint)
				}
				continue
			}
		}

		data, err := integrationsFS.ReadFile(f.source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading embedded file %s: %v\n", f.source, err)
			os.Exit(1)
		}

		dir := filepath.Dir(f.dest)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory %s: %v\n", dir, err)
			os.Exit(1)
		}

		if err := os.WriteFile(f.dest, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", f.dest, err)
			os.Exit(1)
		}

		fmt.Printf("  Installed: %s\n", f.dest)
		if f.hint != "" {
			hints = append(hints, f.hint)
		}
	}
	seenHints := make(map[string]bool)
	for _, hint := range hints {
		if seenHints[hint] {
			continue
		}
		seenHints[hint] = true
		fmt.Printf("  %s\n", hint)
	}
	fmt.Println()
}

func openBrowser(url string) {
	time.Sleep(200 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Run()
}
