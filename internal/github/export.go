package github

import "github.com/tomasz-tomczyk/crit/internal/session"

type (
	GhReplyForPush = ghReplyForPush
	ReplyKey       = replyKey
	PushBuckets    = pushBuckets
	BodyRewriter   = bodyRewriter
	GhComment      = ghComment
	GhEditForPush  = ghEditForPush
	InheritedScope = inheritedScope
)

func BucketCommentsForPush(cj CritJSON, currentHeadSHA string, inRangeMode bool) PushBuckets {
	return bucketCommentsForPush(cj, currentHeadSHA, inRangeMode)
}

func RequireGH() error {
	return requireGH()
}

func FetchPRComments(prNumber int) ([]GhComment, error) {
	return fetchPRComments(prNumber)
}

func FetchPRThreadResolved(prNumber int) (map[int64]bool, error) {
	return fetchPRThreadResolved(prNumber)
}

func MergeGHComments(cj *session.CritJSON, ghComments []GhComment) int {
	return mergeGHComments(cj, ghComments)
}

func MergeGHCommentsScoped(cj *CritJSON, ghComments []GhComment, scope InheritedScope, threadResolved map[int64]bool) int {
	return mergeGHCommentsScoped(cj, ghComments, scope, threadResolved)
}

func BucketsToGHComments(postable []scopedComment, rewrite bodyRewriter) []map[string]any {
	return bucketsToGHComments(postable, rewrite)
}

func CreateGHReview(prNumber int, comments []map[string]any, message, event string) (map[string]int64, error) {
	return createGHReview(prNumber, comments, message, event)
}

func CollectNewRepliesForPush(cf CritJSONFile, rewrite bodyRewriter) []GhReplyForPush {
	return collectNewRepliesForPush(cf, rewrite)
}

func PostPushReplies(prNumber int, allReplies []GhReplyForPush) (map[ReplyKey]int64, int, bool) {
	return postPushReplies(prNumber, allReplies)
}

func ParsePushEvent(flag string) (string, error) {
	return parsePushEvent(flag)
}

func CollectEditedForPush(cj CritJSON) []GhEditForPush {
	return collectEditedForPush(cj)
}

func CollectDeletesForPush(cj CritJSON) []int64 {
	return collectDeletesForPush(cj)
}

func PatchGHComment(id int64, body string) error {
	return patchGHComment(id, body)
}

func DeleteGHComment(id int64) (int, error) {
	return deleteGHComment(id)
}

func UpdateCritJSONWithEditedBodies(critPath string, edits []GhEditForPush) error {
	return updateCritJSONWithEditedBodies(critPath, edits)
}

var ErrGHAuthFailed = errGHAuthFailed

func ExportsDir() (string, error) {
	return exportsDir()
}

func UpdateCritJSONAfterDeletes(critPath string, drained []int64) error {
	return updateCritJSONAfterDeletes(critPath, drained)
}

func UpdateCritJSONWithGitHubIDs(critPath string, commentIDs map[string]int64, replyIDs map[ReplyKey]int64) error {
	return updateCritJSONWithGitHubIDs(critPath, commentIDs, replyIDs)
}

func SummarizeBuckets(prNum int, b PushBuckets) string {
	return summarizeBuckets(prNum, b)
}

func DetailedDryRun(b PushBuckets) string {
	return detailedDryRun(b)
}

func WriteOrphanExport(prNum int, b PushBuckets, exportDir string) (string, error) {
	return writeOrphanExport(prNum, b, exportDir)
}

func RenderOrphanMarkdown(prNum int, b PushBuckets) string {
	return renderOrphanMarkdown(prNum, b)
}

// SwapFetchPRByNumberForTest replaces the PR fetch function for the duration of a test.
func SwapFetchPRByNumberForTest(fn func(int) (*PRInfo, error)) func() {
	prev := fetchPRByNumberFn
	fetchPRByNumberFn = fn
	prMetaCache.reset()
	return func() {
		fetchPRByNumberFn = prev
		prMetaCache.reset()
	}
}

// DefaultStripBodyRewriter removes local attachment refs before GitHub push.
var DefaultStripBodyRewriter BodyRewriter = session.StripBodyRewriter
