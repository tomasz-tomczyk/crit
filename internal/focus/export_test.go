package focus

import "github.com/tomasz-tomczyk/crit/internal/session"

var (
	resolveFocusFromPRForTest        = resolveFocusFromPR
	commentScopeOverrideFromFlagTest = commentScopeOverrideFromFlag
	loadCritJSONForOutputDirForTest  = loadCritJSONForOutputDir
)

func setProbeDaemonFocusForTest(fn func() *Focus) (restore func()) {
	return session.SetProbeDaemonFocusFnForTest(fn)
}

func resolveCommentScopeForTest(override CommentFocusOverride, outputDir string) (InheritedScope, error) {
	return ResolveCommentScope(override, outputDir)
}
