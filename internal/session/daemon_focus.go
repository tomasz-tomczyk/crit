package session

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

var probeDaemonFocusFn = probeDaemonFocusReal

// ProbeDaemonFocus contacts the running daemon (if any) and returns its Focus.
func ProbeDaemonFocus() *Focus {
	return probeDaemonFocusFn()
}

// SetProbeDaemonFocusFnForTest replaces daemon focus probing during tests.
func SetProbeDaemonFocusFnForTest(fn func() *Focus) (restore func()) {
	prev := probeDaemonFocusFn
	if fn == nil {
		probeDaemonFocusFn = func() *Focus { return nil }
	} else {
		probeDaemonFocusFn = fn
	}
	return func() { probeDaemonFocusFn = prev }
}

func probeDaemonFocusReal() *Focus {
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return nil
	}
	sessions, _ := daemon.ListSessionsForCWD(cwd)
	if len(sessions) == 0 {
		return nil
	}
	client := &http.Client{Timeout: 2 * time.Second}
	var rangeFoci []*Focus
	var workingFoci []*Focus
	for _, sess := range sessions {
		f := fetchSessionFocus(client, sess.Host, sess.Port)
		if f == nil {
			continue
		}
		if f.Kind == FocusRange {
			rangeFoci = append(rangeFoci, f)
		} else {
			workingFoci = append(workingFoci, f)
		}
	}
	if len(rangeFoci) == 1 {
		return rangeFoci[0]
	}
	if len(rangeFoci) > 1 {
		return nil
	}
	if len(workingFoci) == 1 {
		return workingFoci[0]
	}
	return nil
}

func fetchSessionFocus(client *http.Client, host string, port int) *Focus {
	connHost := host
	if connHost == "" {
		connHost = "127.0.0.1"
	}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/api/session", connHost, port))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var info struct {
		Focus *Focus `json:"focus"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil
	}
	return info.Focus
}

// ResolvePullScope picks the (HeadSHA, DiffScope) pair stamped on imported
// GitHub PR comments.
func ResolvePullScope(cj *CritJSON) InheritedScope {
	if focus := ProbeDaemonFocus(); focus != nil && focus.Kind == FocusRange {
		return InheritedScope{HeadSHA: focus.HeadSHA, DiffScope: "layer"}
	}
	if cj != nil && cj.ActiveDiffScope != "" {
		return InheritedScope{DiffScope: "layer"}
	}
	return InheritedScope{}
}
