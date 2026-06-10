package session

import (
	"time"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

const MaxAttachmentBytes = maxAttachmentBytes

type DeleteResult = deleteResult

const (
	DeleteResultDeleted   = deleteResultDeleted
	DeleteResultNotFound  = deleteResultNotFound
	DeleteResultForbidden = deleteResultForbidden
)

func SaveAttachment(reviewPath string, data []byte) (string, error) {
	return saveAttachment(reviewPath, data)
}

func RandomUUID() (string, error) {
	return randomUUID()
}

// MustGetwd returns the current working directory, or "." on error.
func MustGetwd() string {
	return mustGetwd()
}

func AttachmentPathFor(reviewPath, filename string) (path, mime string, err error) {
	return attachmentPathFor(reviewPath, filename)
}

func SanitizeAttachmentAltText(name string) string {
	return sanitizeAttachmentAltText(name)
}

func CommentsAtOrBeforeRound(comments []Comment, round int) []Comment {
	return commentsAtOrBeforeRound(comments, round)
}

func SessionActivity(sess *Session, author string) (files, comments int) {
	return sessionActivity(sess, author)
}

func RecordSessionStats(sess *Session, author string, startedAt time.Time) {
	recordSessionStats(sess, author, startedAt)
}

func PlanStorageDir(slug string) (string, error) {
	return planStorageDir(slug)
}

func SavePlanVersion(dir string, content []byte) (int, error) {
	return savePlanVersion(dir, content)
}

func Slugify(s string) string {
	return slugify(s)
}

func ResolveSlug(content []byte) string {
	return resolveSlug(content)
}

func IsStdinPipe() bool {
	return isStdinPipe()
}

func LookupPlanSlug(sessionID string) (string, bool) {
	return lookupPlanSlug(sessionID)
}

func SavePlanSlug(sessionID, slug string) error {
	return savePlanSlug(sessionID, slug)
}

func WhitespaceIgnoredHunks(cached []vcs.DiffHunk, status, oldPath string, ignoreWhitespace bool, path, baseRef, repoRoot string, vc vcs.VCS) []vcs.DiffHunk {
	return whitespaceIgnoredHunks(cached, status, oldPath, ignoreWhitespace, path, baseRef, repoRoot, vc)
}

func CarryForwardComment(old Comment, newID, now string) Comment {
	return carryForwardComment(old, newID, now)
}

func (s *Session) CarryForwardFileComments(f *FileEntry) {
	s.carryForwardFileComments(f)
}

func (s *Session) HandleRoundCompleteFilesForTest() {
	s.handleRoundCompleteFiles()
}

func (s *Session) MergeExternalCritJSONForTest() bool {
	return s.mergeExternalCritJSON()
}

func (s *Session) DrainRoundCompleteForTest() {
	<-s.roundComplete
}

func (s *Session) SetLastCritJSONMtimeForTest(t time.Time) {
	s.lastCritJSONMtime = t
}

// RemoveStaleReviewPath removes a review identity folder or legacy flat file.
func RemoveStaleReviewPath(path string) bool {
	return removeStaleReviewPath(path)
}

func (s *Session) StartWatchFileMtimesForTest(stop <-chan struct{}) {
	go s.watchFileMtimes(stop)
}

func (s *Session) QuiesceForTest() {
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	s.mu.Unlock()
	s.writeMu.Lock()   // wait for any in-flight writeCritJSON
	s.writeMu.Unlock() //nolint:staticcheck // intentional barrier
}
