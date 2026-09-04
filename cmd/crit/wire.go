package main

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/browser"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/gitlab"
	"github.com/tomasz-tomczyk/crit/internal/live"
	"github.com/tomasz-tomczyk/crit/internal/notify"
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

	resolvedCWD        = daemon.ResolvedCWD
	sessionKey         = daemon.SessionKey
	liveSessionKey     = daemon.LiveSessionKey
	previewSessionKey  = preview.PreviewSessionKey
	planSessionKey     = session.PlanSessionKey
	writeSessionFile   = daemon.WriteSessionFile
	writeDaemonFailure = daemon.WriteDaemonFailure
	removeSessionFile  = daemon.RemoveSessionFile
	reviewFilePath     = daemon.ReviewFilePath
	openReadyPipe      = daemon.OpenReadyPipe
	daemonFatal        = daemon.DaemonFatal
	signalReadiness    = daemon.SignalReadiness
	hostForDisplay     = daemon.HostForDisplay
	advertisedURL      = daemon.AdvertisedURL
	shutdownSignals    = daemon.ShutdownSignals
	openBrowser        = browser.OpenBrowserWithCommand
	notifyRoundReady   = notify.RoundReady

	reviewPathsFor = session.ReviewPathsFor
	detectPRInfo   = github.DetectPRInfo
	CurrentBranch  = vcs.CurrentBranch

	bindProxyServer    = live.BindProxyServer
	recordSessionStats = session.RecordSessionStats
	atomicWriteFile    = session.AtomicWriteFile
)

func selectProvider(explicit forge.Kind) (forge.Provider, error) {
	cfg := config.LoadConfig("")
	selection := cfg.Forge
	if explicit != "" && explicit != forge.Auto {
		selection = string(explicit)
	}
	remote := ""
	if out, err := exec.Command("git", "remote", "get-url", "origin").Output(); err == nil {
		remote = strings.TrimSpace(string(out))
	}
	if selection == "" || selection == string(forge.Auto) {
		host := forge.RemoteHost(remote)
		if host != "" && !strings.Contains(host, "github") && !strings.Contains(host, "gitlab") && glabOwnsHost(host) {
			selection = string(forge.GitLab)
		}
	}
	kind, err := forge.DetectKind(selection, remote)
	if err != nil {
		return nil, err
	}
	if kind == forge.GitLab {
		return gitlab.NewProvider(cfg.GitLabURL)
	}
	return github.Provider{}, nil
}

func glabOwnsHost(host string) bool {
	if _, err := exec.LookPath("glab"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "glab", "auth", "status", "--hostname", host).Run() == nil
}

func fetchChange(provider forge.Provider, number int) (forge.ChangeRequest, error) {
	return provider.Get(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: number})
}

func listOpenGitLabChanges(ctx context.Context) ([]forge.ChangeSummary, error) {
	provider, err := gitlab.NewProvider(config.LoadConfig("").GitLabURL)
	if err != nil {
		return nil, err
	}
	return provider.ListOpen(ctx, forge.RepoContext{})
}

func init() {
	forge.SelectProviderFn = selectProvider
	forge.ReviewFn = session.RunReview
	session.InvalidatePRCache = func(number int, project, host string) {
		github.InvalidatePR(forge.ChangeID{Number: number, Project: project, Host: host})
	}
	session.FetchMRFileContent = func(f session.Focus, sha, path string) ([]byte, error) {
		project := f.RemoteProject
		if sha == f.BaseSHA && f.RemoteBaseProject != "" {
			project = f.RemoteBaseProject
		}
		provider, err := gitlab.NewProvider(config.LoadConfig("").GitLabURL)
		if err != nil {
			return nil, err
		}
		return provider.FetchFile(context.Background(), forge.RepoContext{}, forge.RepoRef{
			Project: project, Host: f.RemoteHost,
		}, sha, path)
	}
	session.FetchMRDiffs = func(f session.Focus) ([]session.RemoteDiffFile, error) {
		provider, err := gitlab.NewProvider(config.LoadConfig("").GitLabURL)
		if err != nil {
			return nil, err
		}
		return provider.FetchDiffs(context.Background(), forge.RepoContext{}, forge.ChangeID{
			Number: f.ChangeNumber, Project: f.RemoteBaseProject, Host: f.RemoteHost,
		})
	}
	session.PrintVersionFn = printVersion
	session.PrintHelpFn = printHelp
	server.PrintVersionFn = printVersion
	server.PrintHelpFn = printHelp
	server.ListOpenMRsFn = listOpenGitLabChanges
	server.DetectForgeKindFn = func() forge.Kind {
		provider, err := selectProvider(forge.Auto)
		if err != nil {
			return forge.GitHub
		}
		return provider.Kind()
	}
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
			OutputDir:          sc.OutputDir,
			PlanDir:            sc.PlanDir,
			NoOpen:             sc.NoOpen,
			OpenCmd:            sc.OpenCmd,
			Quiet:              sc.Quiet,
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
	wirePRResolveHooks()
	wireMRResolveHooks()
}

func wirePRResolveHooks() {
	focus.SetPRResolveHooks(
		func(spec string) (focus.ChangeResolveInfo, error) {
			id, err := github.ParsePRSpec(spec)
			if err != nil {
				return focus.ChangeResolveInfo{}, err
			}
			info, err := github.FetchPR(id)
			if err != nil {
				return focus.ChangeResolveInfo{}, err
			}
			return focus.ChangeResolveInfo{
				URL:               info.URL,
				Number:            info.Number,
				Title:             info.Title,
				BaseRefOid:        info.BaseRefOid,
				HeadRefOid:        info.HeadRefOid,
				BaseRefName:       info.BaseRefName,
				HeadRefName:       info.HeadRefName,
				HeadRepoURL:       info.HeadRepoURL,
				BaseRepoProject:   id.Project, // only URL-derived; bare --pr stays checkout-scoped
				HeadRepoProject:   github.ProjectFromRemoteURL(info.HeadRepoURL),
				HeadRepoHost:      id.Host,
				IsCrossRepository: info.IsCrossRepository,
			}, nil
		},
		func(info focus.ChangeResolveInfo, v vcs.VCS) bool {
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

func wireMRResolveHooks() {
	focus.SetMRResolveHooks(
		func(spec string) (focus.ChangeResolveInfo, error) {
			id, err := gitlab.ParseMRSpec(spec)
			if err != nil {
				return focus.ChangeResolveInfo{}, err
			}
			provider, providerErr := gitlab.NewProvider(config.LoadConfig("").GitLabURL)
			if providerErr != nil {
				return focus.ChangeResolveInfo{}, providerErr
			}
			change, err := provider.Get(context.Background(), forge.RepoContext{}, id)
			if err != nil {
				return focus.ChangeResolveInfo{}, err
			}
			return focus.ChangeResolveInfo{
				URL: change.URL, Number: change.ID.Number, Title: change.Title,
				BaseRefOid: change.BaseSHA, HeadRefOid: change.HeadSHA,
				BaseRefName: change.BaseRefName, HeadRefName: change.HeadRefName,
				HeadRepoURL: change.HeadRepo.CloneURL, BaseRepoProject: change.BaseRepo.Project, HeadRepoProject: change.HeadRepo.Project,
				HeadRepoHost: change.HeadRepo.Host, IsCrossRepository: change.CrossRepository,
			}, nil
		},
		func(info focus.ChangeResolveInfo, v vcs.VCS) bool {
			return v != nil && v.DefaultBranch() != "" && info.BaseRefName != v.DefaultBranch()
		},
	)
}
