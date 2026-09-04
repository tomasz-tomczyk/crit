package session

import "testing"

func TestDropStaleCacheOnPRSwitch(t *testing.T) {
	type call struct {
		number  int
		project string
		host    string
	}
	var calls []call
	prev := InvalidatePRCache
	InvalidatePRCache = func(number int, project, host string) {
		calls = append(calls, call{number, project, host})
	}
	t.Cleanup(func() { InvalidatePRCache = prev })

	gh := func(num int, project, host string) Focus {
		return Focus{Kind: FocusRange, Forge: "github", ChangeNumber: num, RemoteBaseProject: project, RemoteHost: host}
	}

	t.Run("same number and project skips", func(t *testing.T) {
		calls = nil
		dropStaleCacheOnPRSwitch(gh(1, "a/b", ""), gh(1, "a/b", ""))
		if len(calls) != 0 {
			t.Fatalf("calls=%v want none", calls)
		}
	})

	t.Run("same number different project invalidates", func(t *testing.T) {
		calls = nil
		dropStaleCacheOnPRSwitch(gh(1, "org/repo-a", ""), gh(1, "org/repo-b", ""))
		if len(calls) != 1 || calls[0] != (call{1, "org/repo-a", ""}) {
			t.Fatalf("calls=%v", calls)
		}
	})

	t.Run("same number different host invalidates", func(t *testing.T) {
		calls = nil
		dropStaleCacheOnPRSwitch(gh(9, "acme/app", "github.com"), gh(9, "acme/app", "github.example.com"))
		if len(calls) != 1 || calls[0] != (call{9, "acme/app", "github.com"}) {
			t.Fatalf("calls=%v", calls)
		}
	})

	t.Run("different number invalidates", func(t *testing.T) {
		calls = nil
		dropStaleCacheOnPRSwitch(gh(2, "o/r", ""), gh(3, "o/r", ""))
		if len(calls) != 1 || calls[0].number != 2 {
			t.Fatalf("calls=%v", calls)
		}
	})

	t.Run("non github skips", func(t *testing.T) {
		calls = nil
		dropStaleCacheOnPRSwitch(
			Focus{Forge: "gitlab", ChangeNumber: 1},
			Focus{Forge: "github", ChangeNumber: 2},
		)
		if len(calls) != 0 {
			t.Fatalf("calls=%v want none", calls)
		}
	})

	t.Run("zero change number skips", func(t *testing.T) {
		calls = nil
		dropStaleCacheOnPRSwitch(gh(0, "a/b", ""), gh(1, "a/b", ""))
		if len(calls) != 0 {
			t.Fatalf("calls=%v want none", calls)
		}
	})

	t.Run("nil hook is safe", func(t *testing.T) {
		InvalidatePRCache = nil
		dropStaleCacheOnPRSwitch(gh(1, "a/b", ""), gh(2, "a/b", ""))
	})
}
