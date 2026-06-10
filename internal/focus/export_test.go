package focus

import "net/http"

var (
	fetchSessionFocusForTest         = fetchSessionFocus
	resolveFocusFromPRForTest        = resolveFocusFromPR
	commentScopeOverrideFromFlagTest = commentScopeOverrideFromFlag
	loadCritJSONForOutputDirForTest  = loadCritJSONForOutputDir
)

func setProbeDaemonFocusForTest(fn func() *Focus) (restore func()) {
	return SetProbeDaemonFocusFnForTest(fn)
}

func probeDaemonFocusForTest() *Focus {
	return probeDaemonFocus()
}

func resolveCommentScopeForTest(override CommentFocusOverride, outputDir string) (InheritedScope, error) {
	return ResolveCommentScope(override, outputDir)
}

func fetchSessionFocusHTTP(client *http.Client, host string, port int) *Focus {
	return fetchSessionFocusForTest(client, host, port)
}
