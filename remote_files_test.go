package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseRepoFromPRURL(t *testing.T) {
	cases := []struct {
		in        string
		wantOwner string
		wantName  string
		wantOK    bool
	}{
		{"https://github.com/foo/bar/pull/295", "foo", "bar", true},
		{"https://github.com/foo/bar/pull/295/files", "foo", "bar", true},
		{"https://github.com/foo/bar/pull/295?diff=split", "foo", "bar", true},
		{"https://github.com/foo/bar/pull/295/", "foo", "bar", true},
		{"http://github.com/foo/bar/pull/1", "foo", "bar", true},
		{"https://github.example.com/foo/bar/pull/42", "foo", "bar", true},
		{"not a url", "", "", false},
		{"https://github.com/foo", "", "", false},
		{"https://github.com/foo/bar", "", "", false},
		{"https://github.com/foo/bar/issues/295", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			gotOwner, gotName, gotOK := parseRepoFromPRURL(c.in)
			if gotOwner != c.wantOwner || gotName != c.wantName || gotOK != c.wantOK {
				t.Errorf("parseRepoFromPRURL(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.in, gotOwner, gotName, gotOK, c.wantOwner, c.wantName, c.wantOK)
			}
		})
	}
}

func TestDecodePRFileContent_HappyPath(t *testing.T) {
	body := []byte("hello world\n")
	encoded := base64.StdEncoding.EncodeToString(body)
	raw := []byte(fmt.Sprintf(`{"content":%q,"encoding":"base64","sha":"abc"}`, encoded))
	got, err := decodePRFileContent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("got %q, want %q", got, body)
	}
}

func TestDecodePRFileContent_StripsNewlinesInPayload(t *testing.T) {
	// GitHub wraps base64 at 60-char lines.
	body := strings.Repeat("abcdefghij", 20) // 200 bytes
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	// Insert newlines to mimic GitHub's wrapping.
	var wrapped strings.Builder
	for i, r := range encoded {
		if i > 0 && i%60 == 0 {
			wrapped.WriteByte('\n')
		}
		wrapped.WriteRune(r)
	}
	raw := []byte(fmt.Sprintf(`{"content":%q,"encoding":"base64"}`, wrapped.String()))
	got, err := decodePRFileContent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != body {
		t.Errorf("got %q, want %q", got, body)
	}
}

func TestDecodePRFileContent_BadEncoding(t *testing.T) {
	raw := []byte(`{"content":"aGk=","encoding":"utf-8"}`)
	_, err := decodePRFileContent(raw)
	if err == nil {
		t.Fatal("expected error for non-base64 encoding")
	}
	if !strings.Contains(err.Error(), "encoding") {
		t.Errorf("error should mention encoding: %v", err)
	}
}

func TestDecodePRFileContent_MalformedJSON(t *testing.T) {
	raw := []byte(`not json`)
	_, err := decodePRFileContent(raw)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// stubFetchFn replaces fetchPRFileContentFn for the duration of a test, with
// a counter so tests can verify cache hits.
func stubFetchFn(t *testing.T, body []byte, fetchErr error, calls *int32) {
	t.Helper()
	prev := fetchPRFileContentFn
	fetchPRFileContentFn = func(_, _, _, _ string) ([]byte, error) {
		atomic.AddInt32(calls, 1)
		if fetchErr != nil {
			return nil, fetchErr
		}
		return body, nil
	}
	t.Cleanup(func() { fetchPRFileContentFn = prev })
}

func TestSession_ReadFileAtSHA_RemoteMode(t *testing.T) {
	var calls int32
	stubFetchFn(t, []byte("file content"), nil, &calls)

	s := &Session{
		RemoteFiles: true,
		Focus: Focus{
			Kind:  FocusRange,
			PRURL: "https://github.com/foo/bar/pull/1",
		},
	}

	first, err := s.readFileAtSHA("abc123", "x.go")
	if err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if string(first) != "file content" {
		t.Errorf("first read returned %q", first)
	}

	second, err := s.readFileAtSHA("abc123", "x.go")
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if string(second) != "file content" {
		t.Errorf("second read returned %q", second)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 fetch call (cache hit on second read), got %d", calls)
	}
}

func TestSession_ReadFileAtSHA_RemoteModePropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	var calls int32
	stubFetchFn(t, nil, wantErr, &calls)

	s := &Session{
		RemoteFiles: true,
		Focus: Focus{
			Kind:  FocusRange,
			PRURL: "https://github.com/foo/bar/pull/1",
		},
	}

	_, err := s.readFileAtSHA("abc", "x.go")
	if !errors.Is(err, wantErr) {
		t.Errorf("got err %v, want %v", err, wantErr)
	}
}

// fakeReadFileVCS lets us assert that local-mode reads call vcs.ReadFileAtSHA
// while remote-mode reads do not.
type fakeReadFileVCS struct {
	VCS
	calls int32
	data  []byte
	err   error
}

func (f *fakeReadFileVCS) ReadFileAtSHA(_, _, _ string) ([]byte, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.data, f.err
}

func TestSession_ReadFileAtSHA_RemoteFallsBackWhenURLUnparseable(t *testing.T) {
	var apiCalls int32
	stubFetchFn(t, []byte("from api"), nil, &apiCalls)

	v := &fakeReadFileVCS{data: []byte("from local")}
	s := &Session{
		VCS:         v,
		RepoRoot:    "/tmp/repo",
		RemoteFiles: true,
		Focus: Focus{
			Kind:  FocusRange,
			PRURL: "totally-not-a-url",
		},
	}

	got, err := s.readFileAtSHA("abc", "x.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from local" {
		t.Errorf("expected fallback to local VCS, got %q", got)
	}
	if atomic.LoadInt32(&v.calls) != 1 {
		t.Errorf("expected 1 vcs.ReadFileAtSHA call, got %d", v.calls)
	}
	if atomic.LoadInt32(&apiCalls) != 0 {
		t.Errorf("expected 0 API calls, got %d", apiCalls)
	}
}

func TestSession_ReadFileAtSHA_LocalModeUsesVCS(t *testing.T) {
	var apiCalls int32
	stubFetchFn(t, []byte("from api"), nil, &apiCalls)

	v := &fakeReadFileVCS{data: []byte("from local")}
	s := &Session{
		VCS:         v,
		RepoRoot:    "/tmp/repo",
		RemoteFiles: false,
		Focus: Focus{
			Kind:  FocusRange,
			PRURL: "https://github.com/foo/bar/pull/1",
		},
	}

	got, err := s.readFileAtSHA("abc", "x.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from local" {
		t.Errorf("expected local VCS read, got %q", got)
	}
	if atomic.LoadInt32(&apiCalls) != 0 {
		t.Errorf("expected 0 API calls in local mode, got %d", apiCalls)
	}
	if atomic.LoadInt32(&v.calls) != 1 {
		t.Errorf("expected 1 VCS call, got %d", v.calls)
	}
}

func TestParseServerFlags_Remote(t *testing.T) {
	sf := parseServerFlags([]string{"--pr", "1", "--remote"})
	if !sf.remoteFiles {
		t.Errorf("expected remoteFiles=true, got %+v", sf)
	}
	if sf.prSpec != "1" {
		t.Errorf("expected prSpec=\"1\", got %q", sf.prSpec)
	}
}

func TestParseServerFlags_RemoteDefaultsFalse(t *testing.T) {
	sf := parseServerFlags([]string{"--pr", "1"})
	if sf.remoteFiles {
		t.Errorf("expected remoteFiles=false by default, got %+v", sf)
	}
}

// TestRemoteFiles_FlagThreading verifies the full chain:
// --remote (flag) → serverConfig.remoteFiles → (later) Session.RemoteFiles.
// We exercise the parseServerFlags + serverConfig assembly without hitting
// resolveServerConfig (which depends on git env). The Session-side assignment
// happens in applySessionOverrides; we test the assignment directly here.
func TestRemoteFiles_FlagThreading(t *testing.T) {
	sf := parseServerFlags([]string{"--pr", "1", "--remote"})
	if !sf.remoteFiles {
		t.Fatalf("flag did not parse: %+v", sf)
	}

	sc := &serverConfig{remoteFiles: sf.remoteFiles}
	if !sc.remoteFiles {
		t.Fatalf("serverConfig did not adopt flag: %+v", sc)
	}

	// applySessionOverrides assigns sc.remoteFiles to session.RemoteFiles
	// before SetFocus. Mirror that single assignment for the chain test.
	session := &Session{}
	session.RemoteFiles = sc.remoteFiles
	if !session.RemoteFiles {
		t.Errorf("session.RemoteFiles not set, got %+v", session)
	}
}

// TestResolveFocus_RangeRemoteSkipsHasObject verifies that --remote in --range
// mode bypasses the local SHA presence check that would otherwise fail.
func TestResolveFocus_RangeRemoteSkipsHasObject(t *testing.T) {
	v := &fakeStackVCS{name: "git", hasSeq: nil} // HasObject always false
	f, err := resolveFocus("", "abc..def", "", true, v, t.TempDir())
	if err != nil {
		t.Fatalf("expected --remote to skip HasObject, got err: %v", err)
	}
	if f == nil || f.HeadSHA != "def" {
		t.Errorf("got %+v", f)
	}
}

// TestResolveFocus_RangeNonRemoteEnforcesHasObject is a regression guard
// keeping the default behavior intact when --remote is not set.
func TestResolveFocus_RangeNonRemoteEnforcesHasObject(t *testing.T) {
	v := &fakeStackVCS{name: "git", hasSeq: nil} // HasObject always false
	_, err := resolveFocus("", "abc..def", "", false, v, t.TempDir())
	if err == nil {
		t.Fatal("expected error from missing local SHA")
	}
}
