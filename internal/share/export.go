package share

import (
	"net/http"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

type (
	FetchWebCommentsResult = fetchWebCommentsResult
	UpsertResult           = upsertResult
)

func LoadShareConfig() config.Config { return loadShareConfig() }

func ResolveShareURL(flagValue string, cfg config.Config, fallback string) string {
	return resolveShareURL(flagValue, cfg, fallback)
}

func ResolveAuthToken(cfg config.Config) string { return resolveAuthToken(cfg) }

func CheckShareAllowed(critPath string) error { return checkShareAllowed(critPath) }

func CheckGitHubSyncAllowed(cj session.CritJSON, op string) error {
	return checkGitHubSyncAllowed(cj, op)
}

func LoadExistingShareCfg(critPath string, paths []string) (session.CritJSON, bool, error) {
	return loadExistingShareCfg(critPath, paths)
}

func FetchWebComments(shareURL string, localIDs, localFingerprints map[string]bool, localFingerprintIDs map[string]string, authToken string) (FetchWebCommentsResult, error) {
	return fetchWebComments(shareURL, localIDs, localFingerprints, localFingerprintIDs, authToken)
}

func FetchWebCommentsFromTarget(shareURL, shareBaseURL string, localIDs, localFingerprints map[string]bool, localFingerprintIDs map[string]string, authToken string) (FetchWebCommentsResult, error) {
	return fetchWebCommentsFromTarget(shareURL, shareBaseURL, localIDs, localFingerprints, localFingerprintIDs, authToken, nil)
}

func FetchWebCommentsForReview(shareURL, shareBaseURL string, cj session.CritJSON, authToken string) (FetchWebCommentsResult, error) {
	return fetchWebCommentsForReview(shareURL, shareBaseURL, cj, authToken)
}

func MergeFetchedComments(critPath string, fetched FetchWebCommentsResult) error {
	return mergeFetchedComments(critPath, fetched)
}

func FetchHasUpdates(fetched FetchWebCommentsResult) bool { return fetchHasUpdates(fetched) }

func UpsertShareToWeb(cfg session.CritJSON, files []ShareFile, comments []ShareComment, authToken string) (UpsertResult, error) {
	return upsertShareToWeb(cfg, files, comments, authToken)
}

func UpdateShareState(critPath string, files []ShareFile, comments []ShareComment, reviewRound int, commentsSent bool) error {
	return updateShareState(critPath, files, comments, reviewRound, commentsSent)
}

func SharedLines(comments []ShareComment) map[string]session.SharedLine {
	return sharedLines(comments)
}

func RecordSharedLines(critPath string, lines map[string]session.SharedLine) error {
	return recordSharedLines(critPath, lines)
}

func PersistShareState(critPath, shareURL, deleteToken, scope, org, orgName, visibility string) error {
	return persistShareState(critPath, shareURL, deleteToken, scope, org, orgName, visibility)
}

func PersistShareStateForTarget(critPath, shareURL, shareBaseURL, deleteToken, scope, org, orgName, visibility string) error {
	return persistShareStateForTarget(critPath, shareURL, shareBaseURL, deleteToken, scope, org, orgName, visibility)
}

func ClearShareState(critPath string) error { return clearShareState(critPath) }

// DecodeJSONOrHTMLHint decodes JSON from an HTTP response or returns a helpful HTML error.
func DecodeJSONOrHTMLHint(resp *http.Response, v any) error {
	return decodeJSONOrHTMLHint(resp, v)
}

func RemapPreviewCommentFiles(comments []ShareComment, entryPath string) {
	remapPreviewCommentFiles(comments, entryPath)
}
