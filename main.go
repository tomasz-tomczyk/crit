package main

import (
	"context"
	"embed"
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
	"syscall"
	"time"
)

//go:embed frontend/*
var frontendFS embed.FS

var version = "dev"

func main() {
	// Handle "crit go [port]" subcommand — signals round-complete to a running crit server
	if len(os.Args) >= 2 && os.Args[1] == "go" {
		port := "3000" // default
		if len(os.Args) >= 3 {
			port = os.Args[2]
		}
		resp, err := http.Post("http://localhost:"+port+"/api/round-complete", "application/json", nil)
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

	srv := NewServer(doc, frontendFS, *shareURL, version, addr.Port)
	if os.Getenv("CRIT_NO_UPDATE_CHECK") == "" {
		go srv.checkForUpdates()
	}
	httpServer := &http.Server{
		Handler:     srv,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
		// No WriteTimeout — SSE connections need to stay open
	}

	url := fmt.Sprintf("http://localhost:%d", addr.Port)
	fmt.Printf("Crit serving %s\n", filepath.Base(absPath))
	fmt.Printf("Open %s in your browser\n", url)

	if !*noOpen {
		go openBrowser(url)
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
	fmt.Println("\nShutting down...")

	doc.Shutdown()
	doc.WriteFiles()

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)

	reviewPath := doc.reviewFilePath()
	if len(doc.GetComments()) > 0 {
		prompt := fmt.Sprintf(
			"I've left review comments in %s — please address each comment and update the plan accordingly. "+
				"Mark each resolved comment in %s by setting \"resolved\": true (optionally add \"resolution_note\" and \"resolution_lines\" pointing to relevant lines in the updated file). "+
				"When done, run: crit go %d",
			reviewPath, doc.commentsFilePath(), addr.Port)
		fmt.Println()
		fmt.Println(prompt)
		fmt.Println()
	} else {
		fmt.Println("No comments. Goodbye!")
	}
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
