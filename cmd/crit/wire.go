package main

import (
	"github.com/tomasz-tomczyk/crit/internal/browser"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/live"
	"github.com/tomasz-tomczyk/crit/internal/preview"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type (
	Config           = config.Config
	Session          = session.Session
	CritJSON         = session.CritJSON
	CritJSONFile     = session.CritJSONFile
	Focus            = session.Focus
	Reply            = session.Reply
	SSEEvent         = session.SSEEvent
	InheritedScope   = focus.InheritedScope
	Server           = server.Server
	StaleIntegration = server.StaleIntegration
	PRInfo           = github.PRInfo
	sessionEntry     = daemon.SessionEntry
)

var (
	DetectVCS    = vcs.DetectVCS
	ResolveFocus = focus.ResolveFocus
	NewServer    = server.NewServer
	mustGetwd    = session.MustGetwd

	resolvedCWD       = daemon.ResolvedCWD
	sessionKey        = daemon.SessionKey
	liveSessionKey    = daemon.LiveSessionKey
	previewSessionKey = preview.PreviewSessionKey
	planSessionKey    = session.PlanSessionKey
	writeSessionFile  = daemon.WriteSessionFile
	removeSessionFile = daemon.RemoveSessionFile
	reviewFilePath    = daemon.ReviewFilePath
	openReadyPipe     = daemon.OpenReadyPipe
	daemonFatal       = daemon.DaemonFatal
	signalReadiness   = daemon.SignalReadiness
	hostForDisplay    = daemon.HostForDisplay
	shutdownSignals   = daemon.ShutdownSignals
	openBrowser       = browser.OpenBrowserWithCommand

	reviewPathsFor = session.ReviewPathsFor
	detectPRInfo   = github.DetectPRInfo
	CurrentBranch  = vcs.CurrentBranch

	bindProxyServer    = live.BindProxyServer
	recordSessionStats = session.RecordSessionStats
	atomicWriteFile    = session.AtomicWriteFile
)

func init() {
	session.InvalidatePRCache = github.InvalidatePRCache
	session.PrintVersionFn = printVersion
	session.PrintHelpFn = printHelp
	server.PrintVersionFn = printVersion
	server.PrintHelpFn = printHelp
	session.InstalledAgentsFn = installedAgents
	session.CheckMissingIntegrationsFn = checkMissingIntegrations
	session.PrintMissingHintsFn = printMissingHints
	server.AvailableIntegrationsFn = availableIntegrations
	server.DetectInstalledIntegrationsFn = func(projectDir, homeDir string) []server.IntegrationStatus {
		statuses := detectInstalledIntegrations(projectDir, homeDir)
		out := make([]server.IntegrationStatus, len(statuses))
		for i, st := range statuses {
			out[i] = server.IntegrationStatus{
				Agent: st.Agent, Status: st.Status, Location: st.Location, Hint: st.Hint, Hash: st.Hash,
			}
		}
		return out
	}
	session.ResolveServerConfigFn = func(args []string) (*session.CLIReviewConfig, error) {
		sc, err := server.ResolveDaemonCLIConfig(args)
		if err != nil {
			return nil, err
		}
		if sc == nil {
			return nil, nil
		}
		return &session.CLIReviewConfig{
			Files:              sc.Files,
			Focus:              sc.Focus,
			PlanDir:            sc.PlanDir,
			NoOpen:             sc.NoOpen,
			OpenCmd:            sc.OpenCmd,
			NoIntegrationCheck: sc.NoIntegrationCheck,
			VCSOverride:        sc.VCSOverride,
			BaseBranch:         sc.BaseBranch,
			IgnorePatterns:     sc.IgnorePatterns,
			SessionID:          sc.SessionID,
		}, nil
	}
	session.PreflightCheckFn = func(sc *session.CLIReviewConfig) string {
		return server.PreflightCheck(&server.DaemonCLIConfig{
			VCSOverride:    sc.VCSOverride,
			BaseBranch:     sc.BaseBranch,
			IgnorePatterns: sc.IgnorePatterns,
		})
	}
	focus.SetPRResolveHooks(
		func(prNum int) (focus.PRResolveInfo, error) {
			info, err := github.FetchPRByNumber(prNum)
			if err != nil {
				return focus.PRResolveInfo{}, err
			}
			return focus.PRResolveInfo{
				URL:               info.URL,
				Number:            info.Number,
				Title:             info.Title,
				BaseRefOid:        info.BaseRefOid,
				HeadRefOid:        info.HeadRefOid,
				BaseRefName:       info.BaseRefName,
				HeadRefName:       info.HeadRefName,
				HeadRepoURL:       info.HeadRepoURL,
				IsCrossRepository: info.IsCrossRepository,
			}, nil
		},
		func(info focus.PRResolveInfo, v vcs.VCS) bool {
			return github.IsStackedPR(&PRInfo{
				URL:               info.URL,
				Number:            info.Number,
				Title:             info.Title,
				BaseRefOid:        info.BaseRefOid,
				HeadRefOid:        info.HeadRefOid,
				BaseRefName:       info.BaseRefName,
				HeadRefName:       info.HeadRefName,
				HeadRepoURL:       info.HeadRepoURL,
				IsCrossRepository: info.IsCrossRepository,
			}, v)
		},
	)
}
