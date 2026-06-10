package main

import (
	"github.com/tomasz-tomczyk/crit/internal/browser"
	"github.com/tomasz-tomczyk/crit/internal/comment"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/live"
	"github.com/tomasz-tomczyk/crit/internal/preview"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type (
	Config               = config.Config
	Session              = session.Session
	CritJSON             = session.CritJSON
	CritJSONFile         = session.CritJSONFile
	Focus                = session.Focus
	Comment              = session.Comment
	Reply                = session.Reply
	FileEntry            = session.FileEntry
	SSEEvent             = session.SSEEvent
	CommentFocusOverride = focus.CommentFocusOverride
	InheritedScope       = focus.InheritedScope
	inheritedScope       = focus.InheritedScope
	PlanDaemonFlags      = session.PlanDaemonFlags
	planDaemonFlags      = session.PlanDaemonFlags
	Server               = server.Server
	StaleIntegration     = server.StaleIntegration
	PRInfo               = github.PRInfo
	shareFile            = share.ShareFile
	webComment           = share.WebComment
	ghReplyForPush       = github.GhReplyForPush
	ghEditForPush        = github.GhEditForPush
	replyKey             = github.ReplyKey
	pushBuckets          = github.PushBuckets
	bodyRewriter         = github.BodyRewriter
	BulkCommentEntry     = comment.BulkCommentEntry
	sessionEntry         = daemon.SessionEntry
	commonDaemonFlags    = daemon.CommonDaemonFlags
)

var (
	LoadConfig               = config.LoadConfig
	SaveGlobalConfig         = config.SaveGlobalConfig
	ResolvePort              = config.ResolvePort
	ResolveHost              = config.ResolveHost
	ResolveShareURL          = config.ResolveShareURL
	DetectVCS                = vcs.DetectVCS
	SetDefaultBranchOverride = vcs.SetDefaultBranchOverride
	ErrNoChangedFiles        = session.ErrNoChangedFiles
	DetectVCSChanges         = session.DetectVCSChanges
	NewGitSession            = session.NewGitSession
	NewGitSessionLenient     = session.NewGitSessionLenient
	NewSessionFromFiles      = session.NewSessionFromFiles
	NewLiveSession           = session.NewLiveSession
	NewPreviewSession        = session.NewPreviewSession
	ResolveFocus             = focus.ResolveFocus
	ResolveCommentScope      = focus.ResolveCommentScope
	resolveCommentScope      = focus.ResolveCommentScope
	NewServer                = server.NewServer
	mustGetwd                = session.MustGetwd

	reviewsDir         = daemon.ReviewsDir
	resolvedCWD        = daemon.ResolvedCWD
	sessionKey         = daemon.SessionKey
	liveSessionKey     = daemon.LiveSessionKey
	previewSessionKey  = preview.PreviewSessionKey
	planSessionKey     = session.PlanSessionKey
	findAliveSession   = daemon.FindAliveSession
	daemonHasBrowser   = daemon.DaemonHasBrowser
	startDaemon        = daemon.StartDaemon
	stopDaemon         = daemon.StopDaemon
	readSessionFile    = daemon.ReadSessionFile
	listSessionsForCWD = func(cwd string) ([]sessionEntry, []string) {
		sessions, err := daemon.ListSessionsForCWD(cwd)
		if err != nil {
			return nil, nil
		}
		return sessions, nil
	}
	findSessionForCWDBranch = daemon.FindSessionForCWDBranch
	stopAllDaemonsForCWD    = daemon.StopAllDaemonsForCWD
	writeSessionFile        = daemon.WriteSessionFile
	removeSessionFile       = daemon.RemoveSessionFile
	reviewFilePath          = daemon.ReviewFilePath
	openReadyPipe           = daemon.OpenReadyPipe
	daemonFatal             = daemon.DaemonFatal
	signalReadiness         = daemon.SignalReadiness
	hostForDisplay          = daemon.HostForDisplay
	shutdownSignals         = daemon.ShutdownSignals
	appendCommonDaemonFlags = daemon.AppendCommonDaemonFlags
	runReviewClient         = daemon.RunReviewClient
	runReviewClientRaw      = daemon.RunReviewClientRaw
	openBrowser             = browser.OpenBrowser

	reviewPathsFor            = session.ReviewPathsFor
	loadCritJSON              = review.LoadCritJSON
	saveCritJSON              = review.SaveCritJSON
	resolveReviewPath         = review.ResolveReviewPath
	resolveReviewPathWithArgs = review.ResolveReviewPathWithArgs

	detectPRInfo                = github.DetectPRInfo
	detectPR                    = github.DetectPR
	invalidatePRCache           = github.InvalidatePRCache
	resolvePullScope            = focus.ResolvePullScope
	bucketCommentsForPush       = github.BucketCommentsForPush
	requireGH                   = github.RequireGH
	fetchPRComments             = github.FetchPRComments
	fetchPRThreadResolved       = github.FetchPRThreadResolved
	mergeGHCommentsScoped       = github.MergeGHCommentsScoped
	summarizeBuckets            = github.SummarizeBuckets
	detailedDryRun              = github.DetailedDryRun
	writeOrphanExport           = github.WriteOrphanExport
	bucketsToGHComments         = github.BucketsToGHComments
	createGHReview              = github.CreateGHReview
	collectNewRepliesForPush    = github.CollectNewRepliesForPush
	updateCritJSONWithGitHubIDs = github.UpdateCritJSONWithGitHubIDs
	stripBodyRewriter           = github.DefaultStripBodyRewriter

	defaultConfig          = config.DefaultConfigString
	unpublishFromWeb       = share.UnpublishFromWeb
	checkGitHubSyncAllowed = share.CheckGitHubSyncAllowed
	CurrentBranch          = vcs.CurrentBranch
	defaultBaseRef         = vcs.DefaultBaseRef
	MergeBase              = vcs.MergeBase

	buildPlanDaemonArgs = session.BuildPlanDaemonArgs

	looksLikeLiveArgs    = live.LooksLikeLiveArgs
	looksLikePreviewArgs = preview.LooksLikePreviewArgs
	bindProxyServer      = live.BindProxyServer
	applyPlanOverrides   = session.ApplyPlanOverrides
	recordSessionStats   = session.RecordSessionStats

	loadShareConfig      = share.LoadShareConfig
	resolveShareURL      = share.ResolveShareURL
	resolveAuthToken     = share.ResolveAuthToken
	checkShareAllowed    = share.CheckShareAllowed
	loadExistingShareCfg = share.LoadExistingShareCfg
	needsShareConsent    = config.NeedsShareConsent
	readFileShared       = session.ReadFileShared
	atomicWriteFile      = session.AtomicWriteFile

	fetchWebComments           = share.FetchWebComments
	mergeWebComments           = share.MergeWebComments
	upsertShareToWeb           = share.UpsertShareToWeb
	updateShareState           = share.UpdateShareState
	computeShareHash           = share.ComputeShareHash
	buildLocalIDSet            = share.BuildLocalIDSet
	buildLocalFingerprintIndex = share.BuildLocalFingerprintIndex
	persistShareState          = share.PersistShareState
	clearShareState            = share.ClearShareState
	loadCommentsForShare       = share.LoadCommentsForShare
	shareReviewFiles           = share.ShareReviewFiles
	shareScope                 = share.ShareScope
	fetchPRByNumber            = github.FetchPRByNumber

	addReviewCommentToCritJSONScoped = comment.AddReviewCommentToCritJSONScoped
	addFileCommentToCritJSONScoped   = comment.AddFileCommentToCritJSONScoped
	planStorageDir                   = session.PlanStorageDir
	savePlanVersion                  = session.SavePlanVersion

	crawlPreview                    = session.CrawlPreview
	buildSharePayload               = share.BuildSharePayload
	setBearer                       = share.SetBearer
	decodeJSONOrHTMLHint            = share.DecodeJSONOrHTMLHint
	findReviewFileByBranch          = review.FindReviewFileByBranch
	parsePushEvent                  = github.ParsePushEvent
	postPushReplies                 = github.PostPushReplies
	collectEditedForPush            = github.CollectEditedForPush
	collectDeletesForPush           = github.CollectDeletesForPush
	patchGHComment                  = github.PatchGHComment
	deleteGHComment                 = github.DeleteGHComment
	updateCritJSONWithEditedBodies  = github.UpdateCritJSONWithEditedBodies
	updateCritJSONAfterDeletes      = github.UpdateCritJSONAfterDeletes
	exportsDir                      = github.ExportsDir
	commentScopeOverrideFromFlag    = focus.CommentScopeOverrideFromFlag
	slugify                         = session.Slugify
	resolveSlug                     = session.ResolveSlug
	isStdinPipe                     = session.IsStdinPipe
	checkCommentCLIAllowed          = comment.CheckCommentCLIAllowed
	addCommentToCritJSONScoped      = comment.AddCommentToCritJSONScoped
	addReplyToCritJSON              = comment.AddReplyToCritJSON
	bulkAddCommentsToCritJSONScoped = comment.BulkAddCommentsToCritJSONScoped
	clearCritJSON                   = review.ClearCritJSON
	lookupPlanSlug                  = session.LookupPlanSlug
	savePlanSlug                    = session.SavePlanSlug
	cleanOrphanedSessions           = daemon.CleanOrphanedSessions
	sessionsDir                     = daemon.SessionsDir
	terminationSignals              = daemon.TerminationSignals
	terminateProcess                = daemon.TerminateProcess
	isDaemonAlive                   = daemon.IsDaemonAlive
)

var (
	errReviewFileAmbiguousForBranch = review.ErrReviewFileAmbiguousForBranch
	errShareUnauthorized            = share.ErrShareUnauthorized
	errGHAuthFailed                 = github.ErrGHAuthFailed
)

const (
	defaultShareURL    = config.DefaultShareURL
	DiffScopeFullStack = focus.DiffScopeFullStack
	FocusRange         = focus.FocusRange
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
	session.ResolveFocusFn = focus.ResolveFocus
	session.ResolveReviewPathFn = review.ResolveReviewPath
	session.LoadCritJSONFromPathFn = review.LoadCritJSON
	session.EnsureReviewFolderFn = review.EnsureReviewFolder
	session.ParsePRSpecFn = focus.ParsePRSpec
	session.ParseRangeSpecFn = focus.ParseRangeSpec
	session.ParseScopeSpecFn = focus.ParseScopeSpec
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
			NoIntegrationCheck: sc.NoIntegrationCheck,
			VCSOverride:        sc.VCSOverride,
			BaseBranch:         sc.BaseBranch,
			IgnorePatterns:     sc.IgnorePatterns,
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
