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

func UpsertShareToWeb(cfg session.CritJSON, files []ShareFile, comments []ShareComment, authToken string) (UpsertResult, error) {
	return upsertShareToWeb(cfg, files, comments, authToken)
}

func UpdateShareState(critPath, hash string, reviewRound int) error {
	return updateShareState(critPath, hash, reviewRound)
}

func ComputeShareHash(files []ShareFile, comments []ShareComment) string {
	return computeShareHash(files, comments)
}

func BuildLocalIDSet(cj session.CritJSON) map[string]bool { return buildLocalIDSet(cj) }

func BuildLocalFingerprintIndex(cj session.CritJSON) (map[string]bool, map[string]string) {
	return buildLocalFingerprintIndex(cj)
}

func PersistShareState(critPath, shareURL, deleteToken, scope, org, orgName, visibility string) error {
	return persistShareState(critPath, shareURL, deleteToken, scope, org, orgName, visibility)
}

func ClearShareState(critPath string) error { return clearShareState(critPath) }

// DecodeJSONOrHTMLHint decodes JSON from an HTTP response or returns a helpful HTML error.
func DecodeJSONOrHTMLHint(resp *http.Response, v any) error {
	return decodeJSONOrHTMLHint(resp, v)
}

func RemapPreviewCommentFiles(comments []ShareComment) {
	remapPreviewCommentFiles(comments)
}
