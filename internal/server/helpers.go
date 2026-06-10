package server

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/auth"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
)

const defaultShareURL = config.DefaultShareURL

func shareScope(paths []string) string        { return share.ShareScope(paths) }
func tokenFromHostedURL(rawURL string) string { return share.TokenFromHostedURL(rawURL) }

func saveGlobalConfig(mutate func(map[string]json.RawMessage) error) error {
	return config.SaveGlobalConfig(mutate)
}

func loadConfigFile(path string) (config.Config, error) {
	cfg, _, err := config.LoadConfigFile(path)
	return cfg, err
}

func globalConfigPath() string { return config.GlobalConfigPath() }

func buildSharePayload(files []ShareFile, comments []shareComment, reviewRound int, cliArgs []string, org, visibility, reviewType string) map[string]any {
	return share.BuildSharePayload(files, comments, reviewRound, cliArgs, org, visibility, reviewType)
}

func buildUpsertPayload(files []ShareFile, comments []shareComment, deleteToken string, reviewRound int, cliArgs []string) map[string]any {
	return share.BuildUpsertPayload(files, comments, deleteToken, reviewRound, cliArgs)
}

func shareReviewFiles(critPath string, files []ShareFile, filePaths []string, svcURL, authToken, fallbackAuthor, org, visibility, reviewType string) (shareReviewFilesResult, error) {
	return share.ShareReviewFiles(critPath, files, filePaths, svcURL, authToken, fallbackAuthor, org, visibility, reviewType)
}

func unpublishFromWeb(shareURL, deleteToken, authToken string) error {
	return share.UnpublishFromWeb(shareURL, deleteToken, authToken)
}

func loadCommentsForShare(critPath string, filePaths []string, fallbackAuthor string) ([]shareComment, int) {
	return share.LoadCommentsForShare(critPath, filePaths, fallbackAuthor)
}

func loadPreviewShareComments(critPath string, sessionPaths []string, fallbackAuthor string) ([]shareComment, int) {
	return share.LoadPreviewShareComments(critPath, sessionPaths, fallbackAuthor)
}

func loadCliArgsFromReviewFile(critPath string) []string {
	return share.LoadCliArgsFromReviewFile(critPath)
}

func dedupWebComments(cj CritJSON, incoming []webComment) ([]webComment, map[string][]webReply) {
	return share.DedupWebComments(cj, incoming)
}

func mergeWebComments(critPath string, newComments []webComment, replyUpdates map[string][]webReply) error {
	return share.MergeWebComments(critPath, newComments, replyUpdates)
}

func crawlPreview(origin string) ([]ShareFile, error) { return session.CrawlPreview(origin) }

func saveAttachment(reviewPath string, data []byte) (string, error) {
	return session.SaveAttachment(reviewPath, data)
}

func attachmentPathFor(reviewPath, filename string) (path, mime string, err error) {
	return session.AttachmentPathFor(reviewPath, filename)
}

func sanitizeAttachmentAltText(name string) string { return session.SanitizeAttachmentAltText(name) }

func commentsAtOrBeforeRound(comments []Comment, round int) []Comment {
	return session.CommentsAtOrBeforeRound(comments, round)
}

func recordSessionStats(sess *Session, author string, startedAt time.Time) {
	session.RecordSessionStats(sess, author, startedAt)
}

func clearAuthIdentity() { auth.ClearAuthIdentity() }

func filterPathsIgnored(paths []string, patterns []string) []string {
	return config.FilterPathsIgnored(paths, patterns)
}

func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	return strings.Split(content, "\n")
}

type (
	shareComment           = share.ShareComment
	shareReviewFilesResult = share.ShareReviewFilesResult
	webComment             = share.WebComment
	webReply               = share.WebReply
)
