package session

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/diff"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type (
	sessionEntry      = daemon.SessionEntry
	shareFile         = ShareFile
	commonDaemonFlags = PlanDaemonFlags
	DiffLine          = vcs.DiffLine
)

var (
	sessionKey              = daemon.SessionKey
	planSessionKey          = PlanSessionKey
	applyPlanOverrides      = ApplyPlanOverrides
	buildPlanDaemonArgs     = BuildPlanDaemonArgs
	parsePRSpec             = parsePRSpecForTest
	parseRangeSpec          = parseRangeSpecForTest
	parseScopeSpec          = parseScopeSpecForTest
	resolveFocus            = resolveFocusForTest
	commitAt                = vcs.CommitAtForTest
	isCommitish             = vcs.IsCommitish
	newProcessGroup         = daemon.NewProcessGroupForTest
	terminateProcess        = daemon.TerminateProcess
	processExists           = daemon.ProcessExists
	crawlPreview            = CrawlPreview
	readFileShared          = ReadFileShared
	ResolveDefaultBranchSHA = vcs.ResolveDefaultBranchSHA
	loadSnapshotsFile       = loadSnapshotsFromDisk
	saveSnapshotsFile       = SaveSnapshotsFile
	saveCritJSON            = SaveCritJSON
	LoadCritJSON            = loadCritJSONForTest
	clearReviewFolder       = ClearReviewFolder
	cloneRoundSnapshots     = CloneRoundSnapshots
	resetDefaultBranchOnce  = vcs.ResetDefaultBranchOnceForTest
	ParseUnifiedDiff        = vcs.ParseUnifiedDiff
	fileDiffUnified         = vcs.FileDiffUnifiedForTest
)

func loadCritJSONForTest(critPath string) (CritJSON, error) {
	if err := ensureReviewFolder(critPath); err != nil {
		return CritJSON{}, err
	}
	return readCritJSONFromDisk(critPath)
}

func parsePRSpecForTest(spec string) (int, error) {
	if ParsePRSpecFn != nil {
		return ParsePRSpecFn(spec)
	}
	return 0, fmt.Errorf("ParsePRSpecFn not wired")
}

func parseRangeSpecForTest(spec string) (base, head string, err error) {
	if ParseRangeSpecFn != nil {
		return ParseRangeSpecFn(spec)
	}
	return "", "", fmt.Errorf("ParseRangeSpecFn not wired")
}

func parseScopeSpecForTest(s string) (DiffScope, error) {
	if ParseScopeSpecFn != nil {
		return ParseScopeSpecFn(s)
	}
	return "", fmt.Errorf("ParseScopeSpecFn not wired")
}

func resolveFocusForTest(prSpec, rangeSpec, scopeSpec string, remoteFiles bool, v vcs.VCS, repoRoot string) (*Focus, error) {
	if ResolveFocusFn != nil {
		return ResolveFocusFn(prSpec, rangeSpec, scopeSpec, remoteFiles, v, repoRoot)
	}
	return nil, fmt.Errorf("ResolveFocusFn not wired")
}

type testServerConfig struct {
	files       []string
	focus       *Focus
	remoteFiles bool
}

func focusKeyArgsFromServerConfig(sc *testServerConfig) []string {
	if sc == nil {
		return nil
	}
	return FocusKeyArgs(&CLIReviewConfig{Files: sc.files, Focus: sc.focus})
}

func withDaemonFocus(t *testing.T, f *Focus) {
	t.Helper()
	restore := SetProbeDaemonFocusFnForTest(func() *Focus {
		if f == nil {
			return nil
		}
		out := *f
		return &out
	})
	t.Cleanup(restore)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// lineStatsForRound mirrors server.lineStatsForRound for unit tests without
// importing internal/server (import cycle).
func lineStatsForRound(sess *Session, n int) (int, int) {
	if n <= 1 {
		return 0, 0
	}
	var adds, dels int
	for _, byRound := range sess.RoundSnapshots {
		curr, ok := byRound[n]
		if !ok {
			continue
		}
		prev, hasPrev := byRound[n-1]
		if !hasPrev {
			adds += len(diff.SplitLines(curr.Content))
			continue
		}
		entries := diff.ComputeLineDiff(prev.Content, curr.Content)
		for _, e := range entries {
			switch e.Type {
			case "added":
				adds++
			case "removed":
				dels++
			}
		}
	}
	return adds, dels
}
