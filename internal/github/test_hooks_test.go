package github

import "testing"

func withFetchPRByNumber(t *testing.T, fn func(int) (*PRInfo, error)) {
	t.Helper()
	restore := SwapFetchPRByNumberForTest(fn)
	t.Cleanup(restore)
}
