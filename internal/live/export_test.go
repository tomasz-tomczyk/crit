package live

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

type serverConfig struct {
	liveOrigin string
	reviewPath string
}

func createLiveSession(sc *serverConfig) (*Session, error) {
	return session.NewLiveSession(sc.liveOrigin, sc.reviewPath)
}

func newTestSession(t *testing.T) *Session {
	t.Helper()
	s := session.NewTestSession(t)
	s.ReviewType = "live"
	s.Origin = "http://localhost:3000"
	s.Files = nil
	return s
}

var (
	setHome       = testutil.SetHome
	newTestServer = server.NewTestServer
)
