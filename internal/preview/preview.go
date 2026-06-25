package preview

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tomasz-tomczyk/crit/internal/browser"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

// PreviewSessionKey returns the session key for preview mode.
func PreviewSessionKey(cwd, absPath string) string {
	h := sha256.New()
	h.Write([]byte(cwd))
	h.Write([]byte("\x00preview\x00"))
	h.Write([]byte(absPath))
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// LooksLikePreviewArgs returns true when args is a single .html/.htm file path.
func LooksLikePreviewArgs(args []string) bool {
	if len(args) != 1 {
		return false
	}
	ext := filepath.Ext(args[0])
	if ext != ".html" && ext != ".htm" {
		return false
	}
	info, err := os.Stat(args[0])
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func connectToPreviewDaemon(key string, noOpen bool, openCmd string) bool {
	entry, alive := daemon.FindAliveSession(key)
	if !alive {
		return false
	}
	fmt.Fprintf(os.Stderr, "[crit] connected to preview daemon at %s\n", entry.BaseURL())
	fmt.Fprintf(os.Stderr, "[crit] open %s/preview\n", entry.BaseURL())
	if !noOpen && !daemon.DaemonHasBrowser(entry) {
		go browser.OpenBrowserWithCommand(entry.BaseURL()+"/preview", openCmd)
	}
	daemon.RunReviewClient(entry, key)
	return true
}

// RunPreview starts a preview-mode review of a local HTML file.
func RunPreview(args []string) {
	fs := flag.NewFlagSet("preview", flag.ExitOnError)
	noOpen := fs.Bool("no-open", false, "Don't auto-open browser")
	port := fs.Int("port", 0, "Port to listen on")
	fs.IntVar(port, "p", 0, "Port (shorthand)")
	host := fs.String("host", "", "Host to listen on")
	publicURL := fs.String("public-url", "", "Advertised base URL (overrides CRIT_PUBLIC_URL)")
	quiet := fs.Bool("quiet", false, "Suppress status output")
	fs.BoolVar(quiet, "q", false, "Suppress status (shorthand)")
	shareURL := fs.String("share-url", "", "Share service URL")
	fs.Parse(args)

	rawPath := ""
	for _, a := range fs.Args() {
		if len(a) > 0 && a[0] != '-' {
			rawPath = a
			break
		}
	}

	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cfg := config.LoadConfig(cwd)
	noOpenResolved := *noOpen || cfg.NoOpen

	if rawPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: crit preview <file.html>")
		os.Exit(1)
	}

	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crit preview: cannot resolve path %q: %v\n", rawPath, err)
		os.Exit(1)
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		fmt.Fprintf(os.Stderr, "crit preview: %q is not a file\n", rawPath)
		os.Exit(1)
	}

	key := PreviewSessionKey(cwd, absPath)
	if connectToPreviewDaemon(key, noOpenResolved, cfg.OpenCmd) {
		return
	}

	daemonArgs := buildPreviewStartArgs(absPath, *port, *host, *publicURL, noOpenResolved, *quiet || cfg.Quiet, *shareURL, cfg)
	entry, err := daemon.StartDaemon(key, daemonArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not start preview daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[crit] preview mode: %s\n", filepath.Base(absPath))
	fmt.Fprintf(os.Stderr, "[crit] open %s/preview\n", entry.BaseURL())

	installDaemonSignalHandler(entry.PID)

	if !noOpenResolved {
		go browser.OpenBrowserWithCommand(entry.BaseURL()+"/preview", cfg.OpenCmd)
	}

	daemon.RunReviewClient(entry, key)
}

func buildPreviewStartArgs(absPath string, port int, host, publicURL string, noOpen, quiet bool, shareURL string, cfg config.Config) []string {
	daemonArgs := []string{"--preview-file", absPath}
	return daemon.AppendCommonDaemonFlags(daemonArgs, daemon.CommonDaemonFlags{
		Port:      config.ResolvePort(port, cfg.Port),
		Host:      config.ResolveHost(host, cfg.Host),
		PublicURL: config.ResolvePublicURL(publicURL, cfg),
		NoOpen:    noOpen,
		Quiet:     quiet,
		ShareURL:  config.ResolveShareURL(shareURL, cfg, config.DefaultShareURL),
	})
}

func installDaemonSignalHandler(pid int) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		proc, err := os.FindProcess(pid)
		if err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
		os.Exit(0)
	}()
}
