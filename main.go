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

func main() {
	if len(os.Args) < 2 {
		runReview(nil)
		return
	}

	switch os.Args[1] {
	case "help", "--help", "-h":
		printHelp()
	case "--version", "-v":
		printVersion()
	case "share":
		runShare(os.Args[2:])
	case "fetch":
		runFetch(os.Args[2:])
	case "unpublish":
		runUnpublish(os.Args[2:])
	case "install":
		runInstall(os.Args[2:])
	case "config":
		runConfig(os.Args[2:])
	case "check":
		runCheck()
	case "pull":
		runPull(os.Args[2:])
	case "push":
		runPush(os.Args[2:])
	case "comment":
		runComment(os.Args[2:])
	case "review":
		runReview(os.Args[2:])
	case "plan":
		runPlan(os.Args[2:])
	case "plan-hook":
		runPlanHook()
	case "stop":
		runStop(os.Args[2:])
	case "_serve":
		runServe(os.Args[2:])
	default:
		runReview(os.Args[1:])
	}
}

func runShare(args []string) {
	shareOutputDir := ""
	shareSvcURL := ""
	showQR := false
	var shareArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--output" || arg == "-o":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			shareOutputDir = args[i]
		case arg == "--share-url":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --share-url requires a value\n")
				os.Exit(1)
			}
			i++
			shareSvcURL = args[i]
		case arg == "--qr":
			showQR = true
		default:
			shareArgs = append(shareArgs, arg)
		}
	}

	if len(shareArgs) == 0 {
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

	cfg := loadShareConfig()
	shareSvcURL = resolveShareURL(shareSvcURL, cfg)
	authToken := resolveAuthToken(cfg)

	var files []shareFile
	for _, path := range shareArgs {
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

	critDir, err := resolveCritDir(shareOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	sharePaths := make([]string, len(files))
	for i, f := range files {
		sharePaths[i] = f.Path
	}

	// Existing share: pull remote comments, then upsert (PUT if changed).
	if existingCfg, ok := loadExistingShareCfg(critDir, sharePaths); ok {
		// 1. Pull any comments added by web reviewers
		localIDs := buildLocalIDSet(existingCfg)
		localFingerprints := buildLocalFingerprints(existingCfg)
		if webComments, err := fetchNewWebComments(existingCfg.ShareURL, localIDs, localFingerprints, authToken); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not pull remote comments: %v\n", err)
		} else if len(webComments) > 0 {
			if err := mergeWebComments(critDir, webComments); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not merge remote comments: %v\n", err)
			}
		}

		// 2. Load full comment state (including resolved) for the upsert payload
		allComments, _ := loadAllCommentsForShare(critDir, sharePaths)

		// 3. Upsert — PUT if anything changed, no-op if identical
		result, err := upsertShareToWeb(existingCfg, files, allComments, authToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := updateShareState(critDir, computeShareHash(files, allComments), result.ReviewRound); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save share state: %v\n", err)
		}
		if result.Changed {
			fmt.Printf("Updated (round %d): %s\n", result.ReviewRound, result.URL)
		} else {
			fmt.Println(existingCfg.ShareURL)
		}

		if showQR {
			fmt.Println()
			qrterminal.GenerateWithConfig(result.URL, qrterminal.Config{
				Level:      qrterminal.L,
				Writer:     os.Stdout,
				HalfBlocks: true,
				QuietZone:  1,
			})
		}
		return
	}

	// New share: POST to create the review.
	filePaths := make([]string, len(files))
	for i, f := range files {
		filePaths[i] = f.Path
	}
	comments, reviewRound := loadCommentsForShare(critDir, filePaths)

	url, deleteToken, err := shareFilesToWeb(files, comments, shareSvcURL, reviewRound, authToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := persistShareState(critDir, url, deleteToken, shareScope(filePaths)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save share state to .crit.json: %v\n", err)
	}

	// Record initial hash so the next `crit share` can detect changes
	initialComments, _ := loadAllCommentsForShare(critDir, filePaths)
	_ = updateShareState(critDir, computeShareHash(files, initialComments), reviewRound)

	fmt.Println(url)
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

func runFetch(args []string) {
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

	critDir, err := resolveCritDir(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	critPath := filepath.Join(critDir, ".crit.json")
	data, err := os.ReadFile(critPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: no .crit.json found. Run `crit share` first.")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid .crit.json: %v\n", err)
		os.Exit(1)
	}
	if cj.ShareURL == "" {
		fmt.Fprintln(os.Stderr, "Error: no share URL in .crit.json. Run `crit share` first.")
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

	if err := mergeWebComments(critDir, webComments); err != nil {
		fmt.Fprintf(os.Stderr, "Error merging comments: %v\n", err)
		os.Exit(1)
	}

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
	unpubSvcURL = resolveShareURL(unpubSvcURL, unpubCfg)
	unpubAuthToken := resolveAuthToken(unpubCfg)

	critDir, err := resolveCritDir(unpubOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	critPath := filepath.Join(critDir, ".crit.json")
	data, err := os.ReadFile(critPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: no .crit.json found. Nothing to unpublish.")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid .crit.json: %v\n", err)
		os.Exit(1)
	}
	if cj.DeleteToken == "" {
		fmt.Fprintln(os.Stderr, "No shared review found in .crit.json — nothing to unpublish.")
		return
	}

	if err := unpublishFromWeb(unpubSvcURL, cj.DeleteToken, unpubAuthToken); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := clearShareState(critDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not clear share state from .crit.json: %v\n", err)
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

func runPull(args []string) {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prFlag := 0
	pullOutputDir := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			pullOutputDir = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit pull [--output <dir>] [pr-number]\n")
			os.Exit(1)
		}
		prFlag = n
	}

	prNumber, err := detectPR(prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ghComments, err := fetchPRComments(prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Load existing .crit.json or create new
	critDir, err := resolveCritDir(pullOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	var cj CritJSON
	if data, err := os.ReadFile(filepath.Join(critDir, ".crit.json")); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: existing .crit.json is invalid, starting fresh: %v\n", err)
		}
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
		cj.Branch = CurrentBranch()
		cfg := LoadConfig(critDir)
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

	if err := writeCritJSON(cj, pullOutputDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Pulled %d comments from PR #%d into .crit.json\n", added, prNumber)
	fmt.Println("Run 'crit' to view them in the browser.")
}

func runPush(args []string) {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prFlag := 0
	dryRun := false
	message := ""
	pushOutputDir := ""
	eventFlag := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--dry-run" {
			dryRun = true
			continue
		}
		if arg == "--message" || arg == "-m" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --message requires a value\n")
				os.Exit(1)
			}
			i++
			message = args[i]
			continue
		}
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --output requires a value\n")
				os.Exit(1)
			}
			i++
			pushOutputDir = args[i]
			continue
		}
		if arg == "--event" || arg == "-e" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --event requires a value (comment, approve, request-changes)\n")
				os.Exit(1)
			}
			i++
			eventFlag = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit push [--dry-run] [--event <type>] [--message <msg>] [--output <dir>] [pr-number]\n")
			os.Exit(1)
		}
		prFlag = n
	}

	event, err := parsePushEvent(eventFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if event == "REQUEST_CHANGES" && message == "" {
		fmt.Fprintf(os.Stderr, "Error: --event request-changes requires --message\n")
		os.Exit(1)
	}

	prNumber, err := detectPR(prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Read .crit.json
	critDir, err := resolveCritDir(pushOutputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(filepath.Join(critDir, ".crit.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no .crit.json found. Run a crit review first.\n")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid .crit.json: %v\n", err)
		os.Exit(1)
	}

	ghComments := critJSONToGHComments(cj)
	if len(ghComments) == 0 && event == "COMMENT" {
		fmt.Println("No unresolved comments to push.")
		return
	}

	// Collect replies to push
	var allReplies []ghReplyForPush
	for _, cf := range cj.Files {
		allReplies = append(allReplies, collectNewRepliesForPush(cf)...)
	}

	if dryRun {
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
		return
	}

	displayEvent := strings.ToLower(strings.ReplaceAll(event, "_", "-"))
	fmt.Printf("Pushing %d comments to PR #%d (%s)...\n", len(ghComments), prNumber, displayEvent)
	commentIDs, err := createGHReview(prNumber, ghComments, message, event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Posted %d review comments to PR #%d (%s)\n", len(ghComments), prNumber, displayEvent)

	// Phase 2: Post new replies individually
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

	// Write GitHub IDs back to .crit.json for idempotent re-push
	critPath := filepath.Join(critDir, ".crit.json")
	if err := updateCritJSONWithGitHubIDs(critPath, commentIDs, replyIDs); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update .crit.json with GitHub IDs: %v\n", err)
	}
}

func runComment(args []string) {
	commentOutputDir := ""
	commentAuthor := ""
	commentReplyTo := ""
	commentResolve := false
	commentPath := ""
	commentJSON := false
	commentPlan := ""
	var commentArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--plan" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --plan requires a slug\n")
				os.Exit(1)
			}
			i++
			commentPlan = args[i]
		} else if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
				os.Exit(1)
			}
			i++
			commentOutputDir = args[i]
		} else if arg == "--author" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --author requires a value\n")
				os.Exit(1)
			}
			i++
			commentAuthor = args[i]
		} else if arg == "--reply-to" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --reply-to requires a comment ID\n")
				os.Exit(1)
			}
			i++
			commentReplyTo = args[i]
		} else if arg == "--resolve" {
			commentResolve = true
		} else if arg == "--path" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --path requires a value\n")
				os.Exit(1)
			}
			i++
			commentPath = args[i]
		} else if arg == "--json" {
			commentJSON = true
		} else {
			commentArgs = append(commentArgs, arg)
		}
	}

	// --plan resolves to --output for the plan storage directory
	if commentPlan != "" {
		if commentOutputDir != "" {
			fmt.Fprintln(os.Stderr, "Error: --plan and --output cannot be used together")
			os.Exit(1)
		}
		var planDirErr error
		commentOutputDir, planDirErr = planStorageDir(slugify(commentPlan))
		if planDirErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", planDirErr)
			os.Exit(1)
		}
	}

	// Resolve author: --author flag > config > git user.name
	if commentAuthor == "" {
		commentCfgDir, _ := os.Getwd()
		if IsGitRepo() {
			commentCfgDir, _ = RepoRoot()
		}
		commentCfg := LoadConfig(commentCfgDir)
		commentAuthor = commentCfg.Author
	}

	// JSON bulk mode: crit comment --json < comments.json
	if commentJSON {
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

		if err := bulkAddCommentsToCritJSON(entries, commentAuthor, commentOutputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Count new comments vs replies
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
		return
	}

	// Reply mode: crit comment --reply-to <id> [--resolve] <body>
	if commentReplyTo != "" {
		if len(commentArgs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: crit comment --reply-to <comment-id> [--resolve] <body>")
			os.Exit(1)
		}
		replyBody := strings.Join(commentArgs, " ")
		if err := addReplyToCritJSON(commentReplyTo, replyBody, commentAuthor, commentResolve, commentOutputDir, commentPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if commentResolve {
			fmt.Printf("Replied to %s and marked resolved\n", commentReplyTo)
		} else {
			fmt.Printf("Replied to %s\n", commentReplyTo)
		}
		return
	}

	// Handle --clear flag
	if len(commentArgs) >= 1 && commentArgs[0] == "--clear" {
		if err := clearCritJSON(commentOutputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Cleared .crit.json")
		return
	}

	if len(commentArgs) < 1 {
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
		fmt.Fprintln(os.Stderr, "  crit comment --reply-to c1 --resolve --author 'Claude' 'Split into two functions'")
		fmt.Fprintln(os.Stderr, "  crit comment --output /tmp/reviews main.go:42 'Fix this bug'")
		fmt.Fprintln(os.Stderr, "  echo '[{\"file\":\"main.go\",\"line\":42,\"body\":\"Fix this\"}]' | crit comment --json --author 'Claude'")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Tips:")
		fmt.Fprintln(os.Stderr, "  Use --author to identify who left the comment (recommended for AI agents)")
		fmt.Fprintln(os.Stderr, "  Use single quotes for the body to avoid shell interpretation of backticks")
		fmt.Fprintln(os.Stderr, "  Use --json for bulk operations (multiple comments/replies in one atomic write)")
		os.Exit(1)
	}

	// Determine comment scope based on argument count and format:
	// 1 arg: review-level comment (just body)
	// 2 args, first contains ":" with valid line spec: line-level comment (existing)
	// 2 args, first is a file path (exists on disk or in .crit.json): file-level comment
	// 2+ args, first contains ":": line-level comment (existing)
	if len(commentArgs) == 1 {
		// Review-level comment: crit comment <body>
		body := commentArgs[0]
		if err := addReviewCommentToCritJSON(body, commentAuthor, commentOutputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Added review comment")
		return
	}

	// 2+ args: check if first arg has a colon with valid line spec
	loc := commentArgs[0]
	colonIdx := strings.LastIndex(loc, ":")
	if colonIdx > 0 {
		// Check if the part after colon looks like a line spec (number or number-number)
		lineSpec := loc[colonIdx+1:]
		if looksLikeLineSpec(lineSpec) {
			// Line-level comment: crit comment <path>:<line[-end]> <body>
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
			if err := addCommentToCritJSON(filePath, startLine, endLine, body, commentAuthor, commentOutputDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Added comment on %s:%s\n", filePath, lineSpec)
			return
		}
	}

	// 2 args without colon line spec: check if first arg is a file path
	if len(commentArgs) >= 2 {
		candidatePath := commentArgs[0]
		if fileExistsOnDiskOrSession(candidatePath, commentOutputDir) {
			// File-level comment: crit comment <path> <body>
			body := strings.Join(commentArgs[1:], " ")
			if err := addFileCommentToCritJSON(candidatePath, body, commentAuthor, commentOutputDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Added file comment on %s\n", candidatePath)
			return
		}
	}

	// Fallback: if nothing matched, treat as the old syntax (error on missing colon)
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
	root, err := resolveCritDir(outputDir)
	if err != nil {
		return false
	}
	critPath := filepath.Join(root, ".crit.json")
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

func runPlan(args []string) {
	pc := resolvePlanConfig(args)

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

	// Resolve slug: explicit --name takes priority, otherwise derive from content heading + date
	var slug string
	if pc.name != "" {
		slug = slugify(pc.name)
	} else {
		slug = resolveSlug(content)
		fmt.Fprintf(os.Stderr, "No --name provided, derived slug: %s\n", slug)
	}
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

	entry, alive := findAliveSession(key)
	weStartedDaemon := false

	if alive {
		fmt.Fprintf(os.Stderr, "Connected to crit daemon on port %d\n", entry.Port)
		if !pc.noOpen && !daemonHasBrowser(entry) {
			go openBrowser(fmt.Sprintf("http://localhost:%d", entry.Port))
		}
	} else {
		entry, err = startDaemon(key, daemonArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Started crit daemon on port %d (PID %d)\n", entry.Port, entry.PID)
		weStartedDaemon = true
	}

	if weStartedDaemon {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			if proc, err := os.FindProcess(entry.PID); err == nil {
				proc.Signal(syscall.SIGTERM)
			}
			os.Exit(0)
		}()
	}

	approved := runReviewClient(entry)

	if approved {
		if proc, err := os.FindProcess(entry.PID); err == nil {
			proc.Signal(syscall.SIGTERM)
		}
	}
}

// runPlanHook is the PermissionRequest hook handler for ExitPlanMode.
// It reads the hook event JSON from stdin, extracts the plan content,
// opens a crit review session, and writes a hookSpecificOutput JSON
// decision (allow/deny) to stdout.
func runPlanHook() {
	var event struct {
		SessionID string `json:"session_id"`
		ToolInput struct {
			Plan string `json:"plan"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: could not parse stdin: %v\n", err)
		return // allow through on parse error
	}
	if strings.TrimSpace(event.ToolInput.Plan) == "" {
		return // no plan content — allow through
	}

	content := []byte(event.ToolInput.Plan)

	// Pin slug to session_id so heading changes don't break the session.
	var slug string
	if event.SessionID != "" {
		if existing, ok := lookupPlanSlug(event.SessionID); ok {
			slug = existing
		} else {
			slug = resolveSlug(content)
			if err := savePlanSlug(event.SessionID, slug); err != nil {
				fmt.Fprintf(os.Stderr, "crit plan-hook: warning: could not save slug mapping: %v\n", err)
			}
		}
	} else {
		slug = resolveSlug(content)
	}
	storageDir, err := planStorageDir(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: error resolving storage dir: %v\n", err)
		return // allow through on error
	}

	ver, err := savePlanVersion(storageDir, content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit plan-hook: error saving plan: %v\n", err)
		return // allow through on error
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
			return // allow through on error
		}
		fmt.Fprintf(os.Stderr, "crit plan-hook: started daemon on port %d (PID %d)\n", entry.Port, entry.PID)
		weStartedDaemon = true
	}

	if weStartedDaemon {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			if proc, err := os.FindProcess(entry.PID); err == nil {
				proc.Signal(syscall.SIGTERM)
			}
			os.Exit(0)
		}()
	}

	approved, prompt := runReviewClientRaw(entry)

	if approved {
		if proc, err := os.FindProcess(entry.PID); err == nil {
			proc.Signal(syscall.SIGTERM)
		}
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

// runReviewClientRaw is like runReviewClient but returns (approved, prompt)
// without writing to stdout — used by runPlanHook to construct hookSpecificOutput.
func runReviewClientRaw(entry sessionEntry) (approved bool, prompt string) {
	client := &http.Client{Timeout: 24 * time.Hour}
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
	key := sessionKey(cwd, sc.files)

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
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			if proc, err := os.FindProcess(entry.PID); err == nil {
				proc.Signal(syscall.SIGTERM)
			}
			os.Exit(0)
		}()
	}

	approved := runReviewClient(entry)

	// Approve (no unresolved comments) — stop the daemon to free the port
	// for future sessions. The daemon is no longer needed since the agent
	// won't reinvoke crit for this review.
	if approved {
		if proc, err := os.FindProcess(entry.PID); err == nil {
			proc.Signal(syscall.SIGTERM)
		}
	}
}

// runReviewClient connects to a running daemon/server, blocks until the user
// finishes reviewing, prints feedback to stdout, and returns whether the
// review was approved (no unresolved comments).
func runReviewClient(entry sessionEntry) (approved bool) {
	client := &http.Client{Timeout: 24 * time.Hour}
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

	body, err := io.ReadAll(resp.Body)
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

	key := sessionKey(cwd, fileArgs)
	if err := stopDaemon(key); err != nil {
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
	agentCmd           string
	planDir            string // managed storage directory for plan mode
	planName           string // display name for plan content
}

// resolveServerConfig parses flags, loads config files, and resolves the
// final server configuration from all sources (CLI > env > config > defaults).
// Returns nil when the command should exit early (e.g. --version).
func resolveServerConfig(args []string) (*serverConfig, error) {
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

	if *showVersion {
		printVersion()
		return nil, nil
	}

	// Load configuration
	configDir := ""
	if IsGitRepo() {
		configDir, _ = RepoRoot()
	}
	if configDir == "" {
		configDir, _ = os.Getwd()
	}
	cfg := LoadConfig(configDir)

	// CRIT_PORT env var (precedence: CLI flag > env var > config > default)
	if *port == 0 {
		if envPort := os.Getenv("CRIT_PORT"); envPort != "" {
			if p, err := strconv.Atoi(envPort); err == nil {
				*port = p
			}
		}
	}

	// Apply config defaults (CLI flags and env vars override)
	if *port == 0 && cfg.Port != 0 {
		*port = cfg.Port
	}
	if !*noOpen && cfg.NoOpen {
		*noOpen = true
	}
	// Share URL precedence: CLI flag > env var > config > runtime default
	if *shareURL == "" {
		if envShare, ok := os.LookupEnv("CRIT_SHARE_URL"); ok {
			*shareURL = envShare
		} else if cfg.ShareURL != "" {
			*shareURL = cfg.ShareURL
		}
	}
	if !*quiet && cfg.Quiet {
		*quiet = true
	}
	if *outputDir == "" && cfg.Output != "" {
		*outputDir = cfg.Output
	}
	// Base branch: CLI flag > config > auto-detect
	if *baseBranch == "" && cfg.BaseBranch != "" {
		*baseBranch = cfg.BaseBranch
	}
	if *baseBranch != "" {
		defaultBranchOverride = *baseBranch
	}

	var ignorePatterns []string
	if !*noIgnore {
		ignorePatterns = cfg.IgnorePatterns
	}

	return &serverConfig{
		port:               *port,
		noOpen:             *noOpen,
		quiet:              *quiet,
		shareURL:           *shareURL,
		authToken:          cfg.AuthToken,
		outputDir:          *outputDir,
		author:             cfg.Author,
		ignorePatterns:     ignorePatterns,
		noIntegrationCheck: cfg.NoIntegrationCheck,
		agentCmd:           cfg.AgentCmd,
		files:              fs.Args(),
		planDir:            *planDir,
		planName:           *planName,
	}, nil
}

func runServe(args []string) {
	sc, err := resolveServerConfig(args)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	if sc == nil {
		return
	}

	// Force quiet mode — daemon runs in background, no terminal output
	sc.quiet = true

	var session *Session
	if len(sc.files) == 0 {
		if !IsGitRepo() {
			fmt.Fprintln(os.Stderr, "Error: not in a git repository and no files specified")
			os.Exit(1)
		}
		session, err = NewSessionFromGit(sc.ignorePatterns)
	} else {
		session, err = NewSessionFromFiles(sc.files, sc.ignorePatterns)
	}
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	if sc.planDir != "" {
		applyPlanOverrides(session, sc.planDir, sc.planName)
		// Clear stale comments loaded from cwd's .crit.json by NewSessionFromFiles,
		// then re-load from the plan storage dir (which may not have a .crit.json yet).
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

	var prInfo *PRInfo
	if session.Mode == "git" {
		prInfo = detectPRInfo()
	}

	// Bind the listener with retry for explicit ports (handles transient EADDRINUSE)
	var listener net.Listener
	for attempt := 0; attempt < 3; attempt++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", sc.port))
		if err == nil {
			break
		}
		if sc.port == 0 {
			break // OS-assigned port won't benefit from retry
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	session.CLIArgs = sc.files

	srv, err := NewServer(session, frontendFS, sc.shareURL, sc.authToken, prInfo, sc.author, version, addr.Port, sc.agentCmd)
	if err != nil {
		log.Fatalf("Error creating server: %v", err)
	}

	// Write session file so clients can discover us
	cwd, _ := resolvedCWD()
	var key string
	if sc.planDir != "" {
		key = planSessionKey(cwd, sc.planName)
	} else {
		key = sessionKey(cwd, sc.files)
	}
	if err := writeSessionFile(key, sessionEntry{
		PID:       os.Getpid(),
		Port:      addr.Port,
		CWD:       cwd,
		Args:      sc.files,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		log.Fatalf("Error writing session file: %v", err)
	}

	// Check for stale integrations (unless disabled)
	if !sc.noIntegrationCheck && os.Getenv("CRIT_NO_INTEGRATION_CHECK") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			stale := checkInstalledIntegrations(cwd, home)
			srv.staleIntegrations = stale
			if len(stale) > 0 {
				go printStaleWarnings(stale)
			}
		}
	}

	// Idle timeout: exit after 1 hour of no HTTP activity
	const idleTimeout = 1 * time.Hour
	var idleMu sync.Mutex
	lastActivity := time.Now()
	resetActivity := func() {
		idleMu.Lock()
		lastActivity = time.Now()
		idleMu.Unlock()
	}

	// Wrap handler to track activity
	httpServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resetActivity()
			srv.ServeHTTP(w, r)
		}),
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	if !sc.noOpen {
		go openBrowser(fmt.Sprintf("http://localhost:%d", addr.Port))
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	// Idle timeout checker
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				idleMu.Lock()
				idle := time.Since(lastActivity)
				idleMu.Unlock()
				if idle >= idleTimeout {
					stop()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	watchStop := make(chan struct{})
	go session.Watch(watchStop)

	go func() {
		if err := httpServer.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Signal readiness to the parent process via FD 3 (the readiness pipe).
	// The listener is already bound so the server can accept connections.
	// Only attempt if _CRIT_READY_FD is set (startDaemon sets this env var).
	if os.Getenv("_CRIT_READY_FD") == "3" {
		os.Unsetenv("_CRIT_READY_FD") // prevent leaking to agent_cmd children
		if readyPipe := os.NewFile(3, "ready-pipe"); readyPipe != nil {
			fmt.Fprintf(readyPipe, "%d\n", addr.Port)
			readyPipe.Close()
		}
	}

	<-ctx.Done()
	close(watchStop)

	// Cleanup session file
	removeSessionFile(key)

	session.Shutdown()
	session.WriteFiles()

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)
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
  crit install <agent>                       Install integration files for an AI coding tool
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
		{source: "integrations/claude-code/commands/crit.md", dest: ".claude/commands/crit.md", hint: "Run /crit in Claude Code to start a review loop"},
		{source: "integrations/claude-code/skills/crit-cli/SKILL.md", dest: ".claude/skills/crit-cli/SKILL.md", hint: "The crit skill is available to Claude Code agents when needed"},
	},
	"cursor": {
		{source: "integrations/cursor/commands/crit.md", dest: ".cursor/commands/crit.md", hint: "Run /crit in Cursor to start a review loop"},
		{source: "integrations/cursor/skills/crit-cli/SKILL.md", dest: ".cursor/skills/crit-cli/SKILL.md", hint: "The crit skill is available to Cursor agents when needed"},
	},
	"opencode": {
		{source: "integrations/opencode/crit.md", dest: ".opencode/commands/crit.md", hint: "Run /crit in OpenCode to start a review loop"},
		{source: "integrations/opencode/SKILL.md", dest: ".opencode/skills/crit/SKILL.md", hint: "The crit skill is available to OpenCode agents when needed"},
	},
	"windsurf": {
		{source: "integrations/windsurf/crit.md", dest: ".windsurf/rules/crit.md", hint: "Windsurf will suggest Crit when writing plans"},
	},
	"github-copilot": {
		{source: "integrations/github-copilot/commands/crit.prompt.md", dest: ".github/prompts/crit.prompt.md", hint: "Run /crit in GitHub Copilot to start a review loop"},
		{source: "integrations/github-copilot/skills/crit-cli/SKILL.md", dest: ".github/skills/crit-cli/SKILL.md", hint: "The crit skill is available to GitHub Copilot agents when needed"},
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
