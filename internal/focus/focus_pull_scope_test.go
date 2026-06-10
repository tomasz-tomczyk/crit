package focus

import "testing"

func TestResolvePullScope_FromReviewFileScope(t *testing.T) {
	withDaemonFocus(t, nil)
	cj := &CritJSON{ActiveDiffScope: "layer"}
	got := ResolvePullScope(cj)
	if got.DiffScope != "layer" || got.HeadSHA != "" {
		t.Errorf("got %+v, want layer scope without head", got)
	}
}

func TestResolvePullScope_Empty(t *testing.T) {
	withDaemonFocus(t, nil)
	got := ResolvePullScope(&CritJSON{})
	if got.HeadSHA != "" || got.DiffScope != "" {
		t.Errorf("got %+v, want empty scope", got)
	}
}
