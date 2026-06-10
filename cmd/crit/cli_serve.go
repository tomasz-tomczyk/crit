package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/server"
)

// liveSessionArgsTag is the leading element of sessionEntry.Args for a
// live daemon: ["live", "<origin>"].
const liveSessionArgsTag = "live"

func serveSessionKey(sc *server.DaemonCLIConfig) string {
	cwd, _ := resolvedCWD()
	if sc.PlanDir != "" {
		return planSessionKey(cwd, sc.PlanName)
	}
	if sc.LiveOrigin != "" {
		return liveSessionKey(cwd, sc.LiveOrigin)
	}
	if sc.PreviewFile != "" {
		return previewSessionKey(cwd, sc.PreviewFile)
	}
	branch := ""
	if vcs := DetectVCS(sc.VCSOverride); vcs != nil {
		branch = vcs.CurrentBranch()
	}
	return sessionKey(cwd, branch, server.FocusKeyArgs(sc))
}

func checkStaleIntegrations(sc *server.DaemonCLIConfig, srv *Server, cwd string) {
	if sc.NoIntegrationCheck || os.Getenv("CRIT_NO_INTEGRATION_CHECK") != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	stale := checkInstalledIntegrations(cwd, home)
	warnings := make([]StaleIntegration, len(stale))
	for i, sf := range stale {
		warnings[i] = StaleIntegration{
			Agent:    sf.agent,
			Location: sf.location,
			Hint:     sf.updateHint(),
			Hash:     sf.hash,
		}
	}
	srv.SetIntegrationWarnings(warnings, checkMissingIntegrations(cwd, home))
}

func bindListener(host string, port int) (net.Listener, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var listener net.Listener
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		listener, err = net.Listen("tcp", addr)
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

// resolveServeReviewPath computes the daemon's review folder so that
// srv.reviewPath, the session-registry entry, session.ReviewFilePath, and
// session.critJSONPath() all agree on one folder.
func resolveServeReviewPath(outputDir, planDir, sessionKey string) string {
	switch {
	case outputDir != "":
		abs, _ := filepath.Abs(outputDir)
		return filepath.Join(abs, ".crit")
	case planDir != "":
		abs, _ := filepath.Abs(planDir)
		return filepath.Join(abs, ".crit")
	default:
		path, _ := reviewFilePath(sessionKey)
		return path
	}
}

func runServe(args []string) {
	pipe := openReadyPipe()

	sc, err := server.ResolveDaemonCLIConfig(args)
	if err != nil {
		daemonFatal(pipe, "Error: %v", err)
	}
	if sc == nil {
		return
	}
	sc.Quiet = true

	listener, err := bindListener(sc.Host, sc.Port)
	if err != nil {
		daemonFatal(pipe, "Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	srv, err := NewServer(nil, frontendFS, sc.ShareURL, sc.ProxyAuth, sc.AuthToken, sc.Author, version, addr.Port, sc.AgentCmd)
	if err != nil {
		daemonFatal(pipe, "Error creating server: %v", err)
	}
	srv.SetListenHost(sc.Host)

	cwd, _ := resolvedCWD()
	homeDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		homeDir = home
	}
	key := serveSessionKey(sc)
	branch := ""
	if vcs := DetectVCS(sc.VCSOverride); vcs != nil {
		branch = vcs.CurrentBranch()
	}
	sc.ReviewPath = resolveServeReviewPath(sc.OutputDir, sc.PlanDir, key)
	var cliArgs []string
	switch {
	case sc.LiveOrigin != "":
		cliArgs = []string{"live", sc.LiveOrigin}
	case sc.PreviewFile != "":
		cliArgs = []string{"preview", sc.PreviewFile}
	default:
		cliArgs = sc.Files
	}
	sessionArgs := sc.Files
	if sc.LiveOrigin != "" {
		sessionArgs = []string{liveSessionArgsTag, sc.LiveOrigin}
	}
	if sc.PreviewFile != "" {
		sessionArgs = []string{"preview", sc.PreviewFile}
	}
	sessionStartedAt := time.Now().UTC()
	srv.ConfigureDaemon(sc.Cfg, cwd, homeDir, sc.ReviewPath, cliArgs, sessionStartedAt)
	if err := writeSessionFile(key, sessionEntry{
		PID:        os.Getpid(),
		Port:       addr.Port,
		Host:       sc.Host,
		CWD:        cwd,
		Args:       sessionArgs,
		Branch:     branch,
		ReviewPath: sc.ReviewPath,
		StartedAt:  sessionStartedAt.Format(time.RFC3339),
	}); err != nil {
		daemonFatal(pipe, "Error writing session file: %v", err)
	}

	var proxyLn net.Listener
	var proxySrv *http.Server
	if sc.LiveOrigin != "" {
		pl, ps, err := bindProxyServer(sc.LiveOrigin, addr.Port, sc.LiveCookie)
		if err != nil {
			daemonFatal(pipe, "Error starting proxy server: %v", err)
		}
		proxyLn = pl
		proxySrv = ps
		go func() {
			if err := proxySrv.Serve(pl); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("Proxy server error: %v", err)
				_ = pl.Close()
			}
		}()
	}

	httpServer := &http.Server{
		Handler:     srv,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()

	srv.SetShutdownCtx(ctx)

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Server error: %v", err)
			stop()
		}
	}()

	signalReadiness(pipe, addr.Port)

	if !sc.NoOpen {
		dh := hostForDisplay(sc.Host)
		openURL := fmt.Sprintf("http://%s:%d", dh, addr.Port)
		if sc.LiveOrigin != "" {
			openURL = fmt.Sprintf("http://%s:%d/live", dh, addr.Port)
		} else if sc.PreviewFile != "" {
			openURL = fmt.Sprintf("http://%s:%d/preview", dh, addr.Port)
		}
		go openBrowser(openURL)
	}

	srv.PrimePRListCache(ctx)

	type sessionResult struct {
		session *Session
		err     error
	}
	ch := make(chan sessionResult, 1)
	go func() {
		s, err := server.CreateSession(sc)
		ch <- sessionResult{s, err}
	}()

	var sess *Session
	var initErr error
	select {
	case res := <-ch:
		sess, initErr = res.session, res.err
	case <-time.After(2 * time.Minute):
		initErr = fmt.Errorf("session initialization timed out after 2 minutes")
	}
	if initErr != nil {
		log.Printf("Error: %v", initErr)
		srv.SetInitErr(initErr)
		stop()
		<-ctx.Done()
		removeSessionFile(key)
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		return
	}
	server.ApplySessionOverrides(sess, sc)
	switch {
	case sc.LiveOrigin != "":
		sess.CLIArgs = []string{"live", sc.LiveOrigin}
	case sc.PreviewFile != "":
		sess.CLIArgs = []string{"preview", sc.PreviewFile}
	default:
		sess.CLIArgs = sc.Files
	}

	checkStaleIntegrations(sc, srv, cwd)

	if !sc.NoUpdateCheck && os.Getenv("CRIT_NO_UPDATE_CHECK") == "" {
		go srv.CheckForUpdates()
	}
	if sc.LiveOrigin != "" && proxyLn != nil {
		sess.ProxyPort = proxyLn.Addr().(*net.TCPAddr).Port
		sess.ReviewType = "live"
		sess.Origin = sc.LiveOrigin
	}
	if sess.ReviewType == "live" || sess.ReviewType == "preview" {
		sess.SetLiveRoundStart(func(_, next int) {
			sess.Notify(SSEEvent{Type: "live-round-start", Round: next})
		})
	}
	srv.SetSession(sess)

	if sess.Mode == "git" {
		go func() {
			if prInfo := detectPRInfo(); prInfo != nil {
				srv.SetPRInfo(prInfo)
			}
		}()
	}

	watchStop := make(chan struct{})
	go sess.Watch(watchStop)

	<-ctx.Done()
	close(watchStop)

	removeSessionFile(key)

	sess.Shutdown()

	var shutWG sync.WaitGroup
	shutWG.Add(1)
	go func() {
		defer shutWG.Done()
		apiCtx, apiCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer apiCancel()
		_ = httpServer.Shutdown(apiCtx)
	}()
	if proxySrv != nil {
		shutWG.Add(1)
		go func() {
			defer shutWG.Done()
			proxyCtx, proxyCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer proxyCancel()
			_ = proxySrv.Shutdown(proxyCtx)
		}()
	}
	shutWG.Wait()
	_ = proxyLn

	if !srv.WaitBackground(30 * time.Second) {
		log.Printf("Warning: background goroutines did not drain within 30s; proceeding with shutdown")
	}

	sess.WriteFiles()

	if srv.ShouldRecordStatsOnShutdown() {
		recordSessionStats(sess, srv.Author(), sessionStartedAt)
	}

	if sess.ReviewFilePath != "" {
		fmt.Fprintf(os.Stderr, "Review file: %s\n", reviewPathsFor(sess.ReviewFilePath).Review)
	}
}
