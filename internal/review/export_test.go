package review

import "testing"

var (
	findStaleReviewsForTest      = findStaleReviews
	deleteStaleReviewsForTest    = deleteStaleReviews
	removeStaleReviewPathForTest = removeStaleReviewPath
)

func withFetchPRByNumber(t *testing.T, fn func(int) (*PRHeadInfo, error)) {
	t.Helper()
	prev := FetchPRHeadInfoFn
	FetchPRHeadInfoFn = fn
	t.Cleanup(func() { FetchPRHeadInfoFn = prev })
}
