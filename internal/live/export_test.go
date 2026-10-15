package live

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

type serverConfig struct {
	liveOrigin string
	reviewPath string
}

// createLiveSession builds a live session and, on success, registers a cleanup
// that cancels the 200ms debounced review write and waits for any in-flight
// write. Without it the write can land after the test returns, racing
// t.TempDir's RemoveAll ("directory is not empty" on Windows).
func createLiveSession(t *testing.T, sc *serverConfig) (*Session, error) {
	t.Helper()
	s, err := session.NewLiveSession(sc.liveOrigin, sc.reviewPath)
	if err == nil {
		t.Cleanup(s.QuiesceForTest)
	}
	return s, err
}

func newTestSession(t *testing.T) *Session {
	t.Helper()
	s := session.NewTestSession(t)
	s.ReviewType = "live"
	s.Origin = "http://localhost:3000"
	s.Files = nil
	t.Cleanup(s.QuiesceForTest) // see createLiveSession
	return s
}

var (
	setHome       = testutil.SetHome
	newTestServer = server.NewTestServer
)

// Avoid shelling out to `gh api users/<login>` (a real GitHub API call) when
// tests merge GitHub comments; mirrors the stub in internal/github's tests.
func init() {
	github.SetFetchGHUserNameForTest(func(string) (string, error) { return "", nil })
}
