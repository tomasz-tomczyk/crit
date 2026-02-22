package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed frontend/*
var frontendFS embed.FS

var version = "dev"

func main() {
	// Handle "crit wait [port]" subcommand — long-polls until review is done, prints prompt, exits
	if len(os.Args) >= 2 && os.Args[1] == "wait" {
		port := "3000"
		if len(os.Args) >= 3 {
			port = os.Args[2]
		}
		baseURL := "http://127.0.0.1:" + port

		result, err := doWait(baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if result.Prompt != "" {
			fmt.Println(result.Prompt)
		}
		os.Exit(0)
	}

	// Handle "crit go [--wait] [port]" subcommand — signals round-complete to a running crit server
	if len(os.Args) >= 2 && os.Args[1] == "go" {
		goFlags := flag.NewFlagSet("go", flag.ExitOnError)
		wait := goFlags.Bool("wait", false, "Wait for review to finish and print prompt")
		goFlags.BoolVar(wait, "w", false, "Wait for review to finish and print prompt")
		goFlags.Parse(os.Args[2:])

		port := "3000"
		if goFlags.NArg() > 0 {
			port = goFlags.Arg(0)
		}
		baseURL := "http://127.0.0.1:" + port

		if *wait {
			result, err := doGoWait(baseURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if result.Prompt != "" {
				fmt.Println(result.Prompt)
			}
			os.Exit(0)
		}

		// Original non-wait behavior
		resp, err := http.Post(baseURL+"/api/round-complete", "application/json", nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not reach crit on port %s: %v\n", port, err)
			os.Exit(1)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			fmt.Println("Round complete — crit will reload.")
		} else {
			fmt.Fprintf(os.Stderr, "Unexpected status: %d\n", resp.StatusCode)
			os.Exit(1)
		}
		os.Exit(0)
	}

	port := flag.Int("port", 0, "Port to listen on (default: random available port)")
	flag.IntVar(port, "p", 0, "Port to listen on (shorthand)")
	outputDir := flag.String("output", "", "Output directory for review files (default: same dir as input file)")
	flag.StringVar(outputDir, "o", "", "Output directory (shorthand)")
	noOpen := flag.Bool("no-open", false, "Don't auto-open browser")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "v", false, "Print version and exit (shorthand)")
	shareURL := flag.String("share-url", "", "Base URL of hosted Crit service for sharing reviews (overrides CRIT_SHARE_URL env var)")
	waitFlag := flag.Bool("wait", false, "Block until reviewer clicks Finish, then print prompt to stdout")
	flag.BoolVar(waitFlag, "w", false, "Block until reviewer clicks Finish (shorthand)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: crit [options] <file.md>\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	filePath := flag.Arg(0)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		log.Fatalf("Error resolving path: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	if info.IsDir() {
		log.Fatalf("Error: %s is a directory, not a file", absPath)
	}

	outDir := *outputDir
	if outDir == "" {
		outDir = filepath.Dir(absPath)
	}

	doc, err := NewDocument(absPath, outDir)
	if err != nil {
		log.Fatalf("Error loading document: %v", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	if *shareURL == "" {
		*shareURL = os.Getenv("CRIT_SHARE_URL")
	}
	if *shareURL == "" {
		*shareURL = "https://crit.live"
	}

	srv, err := NewServer(doc, frontendFS, *shareURL, version, addr.Port)
	if err != nil {
		log.Fatalf("Error creating server: %v", err)
	}
	if os.Getenv("CRIT_NO_UPDATE_CHECK") == "" {
		go srv.checkForUpdates()
	}
	httpServer := &http.Server{
		Handler:     srv,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
		// No WriteTimeout — SSE connections need to stay open
	}

	status := newStatus(os.Stdout)
	srv.status = status
	doc.status = status

	url := fmt.Sprintf("http://localhost:%d", addr.Port)
	status.Listening(url)

	if !*noOpen {
		go openBrowser(url)
	}

	if *waitFlag {
		go func() {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/await-review", addr.Port))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error waiting for review: %v\n", err)
				return
			}
			defer resp.Body.Close()
			var result ReviewResult
			json.NewDecoder(resp.Body).Decode(&result)
			if result.Prompt != "" {
				fmt.Println(result.Prompt)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watchStop := make(chan struct{})
	go doc.WatchFile(watchStop)

	go func() {
		if err := httpServer.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	close(watchStop)
	fmt.Println()

	doc.Shutdown()
	doc.WriteFiles()

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)
}

// doWait long-polls /api/await-review until the review is finished.
// Returns the review result with the prompt for the agent.
func doWait(baseURL string) (ReviewResult, error) {
	displayURL := strings.Replace(baseURL, "127.0.0.1", "localhost", 1)
	fmt.Fprintf(os.Stderr, "Waiting for review at %s — click Finish when done.\n", displayURL)
	resp, err := http.Get(baseURL + "/api/await-review")
	if err != nil {
		return ReviewResult{}, fmt.Errorf("error waiting for review: %w", err)
	}
	defer resp.Body.Close()

	var result ReviewResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ReviewResult{}, fmt.Errorf("error reading review result: %w", err)
	}
	return result, nil
}

// doGoWait signals round-complete and waits for the review to finish.
// Returns the review result with the prompt for the agent.
func doGoWait(baseURL string) (ReviewResult, error) {
	resp, err := http.Post(baseURL+"/api/round-complete", "application/json", nil)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("could not reach crit: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return ReviewResult{}, fmt.Errorf("round-complete returned status %d", resp.StatusCode)
	}
	displayURL := strings.Replace(baseURL, "127.0.0.1", "localhost", 1)
	fmt.Fprintf(os.Stderr, "Round complete — open %s to review the changes, then click Finish.\n", displayURL)

	resp, err = http.Get(baseURL + "/api/await-review")
	if err != nil {
		return ReviewResult{}, fmt.Errorf("error waiting for review: %w", err)
	}
	defer resp.Body.Close()

	var result ReviewResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ReviewResult{}, fmt.Errorf("error reading review result: %w", err)
	}
	return result, nil
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
