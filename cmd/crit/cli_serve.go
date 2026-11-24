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

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/reviewpath"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// liveSessionArgsTag is the leading element of sessionEntry.Args for a
// live daemon: ["live", "<origin>"].
const liveSessionArgsTag = "live"

func serveSessionKey(sc *server.DaemonCLIConfig) string {
	if sc.SessionKeyOverride != "" {
		return sc.SessionKeyOverride
	}
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

// checkServeLiveCookies is the daemon backstop for live cookie forwarding.
// RunLive checks flags and config before connecting to or starting a daemon.
func checkServeLiveCookies(sc *server.DaemonCLIConfig) error {
	if sc.LiveOrigin != "" && sc.LiveCookie != "" && config.NeedsUnauthenticatedNetworkAck(sc.Host, sc.PublicURL) {
		return fmt.Errorf("refusing upstream cookies with host %q and public URL %q: anyone who can reach the live proxy would be logged in as you", sc.Host, sc.PublicURL)
	}
	return nil
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

// serveCLIArgs is what the review file records as cli_args, so that
// `crit --session` can restart the daemon in the same mode. A commit range is
// stored as "range:<base>..<head>", which the reconnect turns back into
// --range. A head typed as a ref (HEAD, a branch) is stored as that ref, not
// the commit it resolved to: the session follows the ref each round and
// re-keys comments to the new head, so a restart must resolve it again or it
// opens on a stale head without those comments. PR/MR keys are not stored:
// they can name a project ("pr:owner/repo#N") that --pr and --mr do not accept.
func serveCLIArgs(sc *server.DaemonCLIConfig) []string {
	switch {
	case sc.LiveOrigin != "":
		return []string{"live", sc.LiveOrigin}
	case sc.PreviewFile != "":
		return []string{"preview", sc.PreviewFile}
	case sc.Focus != nil && sc.Focus.Kind == session.FocusRange && sc.Focus.ChangeNumber == 0:
		if sc.Focus.HeadRef != "" {
			return []string{"range:" + sc.Focus.BaseSHA + ".." + sc.Focus.HeadRef}
		}
		return server.FocusKeyArgs(sc)
	default:
		return sc.Files
	}
}

// bindPorts binds the API listener for the requested port. In live mode
// (withProxy) it also binds the live proxy listener on the API port + 1, so
// both ports are held before anything else can take the second one.
func bindPorts(host string, req config.PortRange, withProxy bool) (api, proxy net.Listener, err error) {
	if req.IsRange() {
		return bindPortsInRange(host, req, withProxy)
	}
	api, err = bindFixedPort(host, req.First)
	if err != nil || !withProxy {
		return api, nil, err
	}
	proxyPort := api.Addr().(*net.TCPAddr).Port + 1
	proxy, err = listenTCP(proxyListenHost(host), proxyPort)
	if err != nil {
		_ = api.Close()
		return nil, nil, fmt.Errorf("proxy port %d already in use: %w", proxyPort, err)
	}
	return api, proxy, nil
}

// bindFixedPort binds one port, or a random one when port is 0. A fixed port
// is tried 3 times, 500 ms apart, so a restarting daemon can release it.
func bindFixedPort(host string, port int) (net.Listener, error) {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		var ln net.Listener
		ln, err = listenTCP(host, port)
		if err == nil {
			return ln, nil
		}
		if port == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, err
}

// bindPortsInRange makes one pass over the range and takes the first free
// port (or, in live mode, the first free pair N, N+1 with N+1 in the range).
// Any listen error moves on to the next port: sandbox denials and OS error
// codes differ, so it does not only look for "address in use".
func bindPortsInRange(host string, req config.PortRange, withProxy bool) (api, proxy net.Listener, err error) {
	var lastErr error
	for port := req.First; port <= req.Last; port++ {
		if withProxy && port+1 > req.Last {
			break
		}
		api, lastErr = listenTCP(host, port)
		if lastErr != nil {
			continue
		}
		if !withProxy {
			return api, nil, nil
		}
		proxy, lastErr = listenTCP(proxyListenHost(host), port+1)
		if lastErr == nil {
			return api, proxy, nil
		}
		_ = api.Close()
	}
	if withProxy {
		return nil, nil, fmt.Errorf("no two consecutive free ports in %s for the API and live proxy (last error: %w)", req, lastErr)
	}
	return nil, nil, fmt.Errorf("no free port in %s (last error: %w)", req, lastErr)
}

func listenTCP(host string, port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
}

// proxyListenHost is the host the live proxy binds: the API host, or
// 127.0.0.1 when the API host is empty.
func proxyListenHost(host string) string {
	if host == "" {
		return "127.0.0.1"
	}
	return host
}

// resolveServeReviewPath computes the daemon's review folder so that
// srv.reviewPath, the session-registry entry, session.ReviewFilePath, and
// session.critJSONPath() all agree on one folder.
var serveAbsPath = filepath.Abs

func resolveServeReviewPath(outputDir, planDir, sessionKey string) (string, error) {
	switch {
	case outputDir != "":
		// --output / config output is a crit data root (like ~/.crit).
		return reviewpath.Identity(outputDir, sessionKey)
	case planDir != "":
		abs, err := serveAbsPath(planDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, ".crit"), nil
	default:
		return reviewFilePath(sessionKey)
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

	listener, proxyLn, err := bindPorts(sc.Host, sc.Port, sc.LiveOrigin != "")
	if err != nil {
		daemonFatal(pipe, "Error starting server: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)

	srv, err := NewServer(nil, frontendFS, sc.ShareURL, sc.ProxyAuth, sc.AuthToken, sc.Author, version, addr.Port, sc.AgentCmd)
	if err != nil {
		daemonFatal(pipe, "Error creating server: %v", err)
	}
	srv.SetListenHost(sc.Host)
	srv.SetPublicURL(sc.PublicURL)
	exposesProxy := sc.LiveOrigin != "" && config.NeedsUnauthenticatedNetworkAck(sc.Host, sc.PublicURL)
	if err := checkServeLiveCookies(sc); err != nil {
		daemonFatal(pipe, "Error: %v", err)
	}
	if sc.AllowUnauthenticatedNetwork {
		fmt.Fprintln(os.Stderr, "crit: WARNING: unauthenticated network exposure enabled. Anyone who can reach this port can read review files and write comments.")
		if exposesProxy {
			fmt.Fprintf(os.Stderr, "crit: WARNING: the live proxy on port %d is exposed too. Anyone who can reach it can browse %s through it.\n", addr.Port+1, sc.LiveOrigin)
		}
	}

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
	sc.ReviewPath, err = resolveServeReviewPath(sc.OutputDir, sc.PlanDir, key)
	if err != nil {
		daemonFatal(pipe, "Error: resolve review path: %v", err)
	}
	cliArgs := serveCLIArgs(sc)
	sessionArgs := sc.Files
	if sc.LiveOrigin != "" {
		sessionArgs = []string{liveSessionArgsTag, sc.LiveOrigin}
	}
	if sc.PreviewFile != "" {
		sessionArgs = []string{"preview", sc.PreviewFile}
	}
	sessionStartedAt := time.Now().UTC()
	sessionGeneration := sessionStartedAt.Format(time.RFC3339Nano)
	srv.ConfigureDaemon(sc.Cfg, cwd, homeDir, sc.ReviewPath, cliArgs, sessionStartedAt)
	if err := writeSessionFile(key, sessionEntry{
		PID:        os.Getpid(),
		Port:       addr.Port,
		Host:       sc.Host,
		PublicURL:  sc.PublicURL,
		CWD:        cwd,
		Args:       sessionArgs,
		Branch:     branch,
		ReviewPath: sc.ReviewPath,
		StartedAt:  sessionGeneration,
	}); err != nil {
		daemonFatal(pipe, "Error writing session file: %v", err)
	}

	var proxySrv *http.Server
	if proxyLn != nil {
		ps, err := newProxyServer(sc.Host, sc.PublicURL, sc.LiveOrigin, addr.Port, sc.LiveCookie)
		if err != nil {
			daemonFatal(pipe, "Error starting proxy server: %v", err)
		}
		proxySrv = ps
		go func() {
			if err := proxySrv.Serve(proxyLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("Proxy server error: %v", err)
				_ = proxyLn.Close()
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
	// A separate cancel keeps the signal handlers registered when
	// /api/shutdown starts the shutdown; calling stop would unregister them.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	srv.SetShutdownCtx(ctx)
	srv.SetStopFunc(cancel)

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Server error: %v", err)
			stop()
		}
	}()

	signalReadiness(pipe, addr.Port)

	if !sc.NoOpen {
		openPath := ""
		if sc.LiveOrigin != "" {
			openPath = "/live"
		} else if sc.PreviewFile != "" {
			openPath = "/preview"
		}
		openURL := advertisedURL(sc.PublicURL, sc.Host, addr.Port, openPath)
		go openBrowser(openURL, sc.OpenCmd)
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
		if err := writeDaemonFailure(key, sessionGeneration, initErr); err != nil {
			log.Printf("Warning: could not preserve daemon initialization error: %v", err)
		}
		stop()
		<-ctx.Done()
		// Keep the session entry and log until stale-session cleanup. The client
		// may already have received the entry and needs the log to report this
		// fatal initialization error after the server stops.
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		return
	}
	server.ApplySessionOverrides(sess, sc)
	sess.CLIArgs = cliArgs
	sess.SessionKey = key
	sess.CWD = cwd

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
	// Desktop notify is independent of Quiet: the daemon always sets Quiet so
	// status goes through the client pipe, but AFK notifications should still fire.
	if sc.NotifyOnRoundReady {
		reviewURL := advertisedURL(sc.PublicURL, sc.Host, addr.Port, "")
		sess.SetOnRoundReady(func(round int) {
			go notifyRoundReady(round, reviewURL)
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
