package comment

import "github.com/tomasz-tomczyk/crit/internal/session"

func CheckCommentCLIAllowed(critPath string) error {
	return checkCommentCLIAllowed(critPath)
}

func AddCommentToCritJSONScoped(filePath string, startLine, endLine int, body, author, userID, outputDir string, scope session.InheritedScope) error {
	return addCommentToCritJSONScoped(filePath, startLine, endLine, body, author, userID, outputDir, scope)
}

func AddReplyToCritJSON(commentID, body, author, userID string, resolve bool, outputDir, filterPath string) error {
	return addReplyToCritJSON(commentID, body, author, userID, resolve, outputDir, filterPath)
}

func AppendReply(cj *session.CritJSON, commentID, body, author, userID string, resolve bool, filterPath string) error {
	return appendReply(cj, commentID, body, author, userID, resolve, filterPath)
}

func BulkAddCommentsToCritJSONScoped(entries []BulkCommentEntry, globalAuthor, globalUserID, outputDir string, scope session.InheritedScope) error {
	return bulkAddCommentsToCritJSONScoped(entries, globalAuthor, globalUserID, outputDir, scope)
}

func AddReviewCommentToCritJSONScoped(body, author, userID, outputDir string, scope session.InheritedScope) error {
	return addReviewCommentToCritJSONScoped(body, author, userID, outputDir, scope)
}

func AddFileCommentToCritJSONScoped(filePath, body, author, userID, outputDir string, scope session.InheritedScope) error {
	return addFileCommentToCritJSONScoped(filePath, body, author, userID, outputDir, scope)
}

func AppendFileCommentScoped(cj *session.CritJSON, filePath, body, author, userID string, scope session.InheritedScope) {
	appendFileCommentScoped(cj, filePath, body, author, userID, scope)
}

func AppendReviewCommentScoped(cj *session.CritJSON, body, author, userID string, scope session.InheritedScope) {
	appendReviewCommentScoped(cj, body, author, userID, scope)
}

func AppendCommentScoped(cj *session.CritJSON, filePath string, startLine, endLine int, body, author, userID string, scope session.InheritedScope) {
	appendCommentScoped(cj, filePath, startLine, endLine, body, author, userID, scope)
}

func ProcessBulkReviewEntry(cj *session.CritJSON, i int, e BulkCommentEntry, author, userID string, scope session.InheritedScope) error {
	return processBulkReviewEntry(cj, i, e, author, userID, scope)
}

// ParseLineSpec parses a line range spec like "10" or "10-20".
func ParseLineSpec(spec string) (start, end int, err error) {
	return parseLineSpec(spec)
}
