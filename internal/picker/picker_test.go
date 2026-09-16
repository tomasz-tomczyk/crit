package picker

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// wideScreen is roomy enough that no test row is truncated or scrolled by the
// terminal size; size-sensitive behavior gets its own explicit screen.
var wideScreen = screen{columns: 200, rows: 50}

func items(labels ...string) []Item {
	list := make([]Item, len(labels))
	for i, label := range labels {
		list[i] = Item{Label: label, Detail: "detail"}
	}
	return list
}

func TestRun_EnterSelectsHighlightedItem(t *testing.T) {
	keys := strings.NewReader("\x1b[B\x1b[B\r")
	got, err := run(keys, io.Discard, "Pick", items("one", "two", "three"), wideScreen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 2 {
		t.Errorf("selected %d, want 2", got)
	}
}

func TestRun_VimKeysMove(t *testing.T) {
	keys := strings.NewReader("jjk\r")
	got, err := run(keys, io.Discard, "Pick", items("one", "two", "three"), wideScreen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1", got)
	}
}

func TestRun_CursorStopsAtEnds(t *testing.T) {
	keys := strings.NewReader("kkkk\r")
	got, err := run(keys, io.Discard, "Pick", items("one", "two"), wideScreen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 0 {
		t.Errorf("selected %d, want 0 — cursor must not wrap past the top", got)
	}
}

func TestRun_CancelKeys(t *testing.T) {
	for name, keys := range map[string]string{
		"q":      "q",
		"ctrl-c": "\x03",
		"eof":    "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := run(strings.NewReader(keys), io.Discard, "Pick", items("one"), wideScreen)
			if !errors.Is(err, ErrCancelled) {
				t.Fatalf("err = %v, want ErrCancelled", err)
			}
		})
	}
}

// dribbleReader hands out one byte per Read, like a tty over a slow link where
// the three bytes of an arrow key arrive in separate reads.
type dribbleReader struct {
	data []byte
	pos  int
}

func (r *dribbleReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

// An arrow key split across reads must still move the cursor. Deciding what ESC
// meant from what happened to be buffered used to cancel the picker here.
func TestRun_ArrowKeySplitAcrossReads(t *testing.T) {
	keys := &dribbleReader{data: []byte("\x1b[B\x1b[B\r")}
	got, err := run(keys, io.Discard, "Pick", items("one", "two", "three"), wideScreen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 2 {
		t.Errorf("selected %d, want 2 — a split arrow sequence must still move", got)
	}
}

// A bare ESC does nothing, and the key typed after it is handled normally.
func TestRun_BareEscapeIsNotCancel(t *testing.T) {
	got, err := run(strings.NewReader("\x1bj\r"), io.Discard, "Pick", items("one", "two"), wideScreen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1 — the key after a bare ESC must still register", got)
	}
}

func TestRun_SkipsDisabledItems(t *testing.T) {
	list := []Item{
		{Label: "gone", Note: "directory is gone"},
		{Label: "usable"},
		{Label: "also gone", Note: "directory is gone"},
	}

	got, err := run(strings.NewReader("\r"), io.Discard, "Pick", list, wideScreen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1 — the cursor must start on a selectable item", got)
	}

	// Moving down from the only selectable row must not land on the disabled one.
	got, err = run(strings.NewReader("\x1b[B\r"), io.Discard, "Pick", list, wideScreen)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1 — disabled rows must be skipped", got)
	}
}

func TestRun_AllItemsDisabledCannotSelect(t *testing.T) {
	list := []Item{{Label: "gone", Note: "directory is gone"}}
	// Enter is ignored on a disabled row, so only the cancel key ends the loop.
	_, err := run(strings.NewReader("\r\rq"), io.Discard, "Pick", list, wideScreen)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestRender_ShowsLabelsDetailsAndNotes(t *testing.T) {
	var out bytes.Buffer
	list := []Item{
		{Label: "feature-branch", Detail: "~/code/app · 2h ago"},
		{Label: "old-branch", Note: "directory is gone — /tmp/x"},
	}
	if _, err := run(strings.NewReader("q"), &out, "Resume a review", list, wideScreen); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}

	text := out.String()
	for _, want := range []string{"Resume a review", "feature-branch", "~/code/app · 2h ago", "directory is gone", "enter resume"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered output missing %q:\n%s", want, text)
		}
	}
}

func TestRender_ScrollsLongListsAroundCursor(t *testing.T) {
	labels := make([]string, 0, maxVisible+5)
	for i := 0; i < maxVisible+5; i++ {
		labels = append(labels, "item-"+string(rune('a'+i)))
	}

	var out bytes.Buffer
	keys := strings.NewReader(strings.Repeat("j", maxVisible+4) + "q")
	if _, err := run(keys, &out, "Pick", items(labels...), wideScreen); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}

	text := out.String()
	last := labels[len(labels)-1]
	if !strings.Contains(text, last) {
		t.Errorf("scrolled list never rendered the final item %q", last)
	}
	if !strings.Contains(text, "showing") {
		t.Errorf("scrolled list should report its window position:\n%s", text)
	}
}

func TestRun_ErasesTheListOnExit(t *testing.T) {
	for name, keys := range map[string]string{"selected": "\r", "cancelled": "q"} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if _, err := run(strings.NewReader(keys), &out, "Pick", items("one", "two"), wideScreen); err != nil && !errors.Is(err, ErrCancelled) {
				t.Fatalf("run: %v", err)
			}
			// 1 prompt + 2 rows + 1 footer, rewound and cleared.
			if want := "\033[4A\r\033[J"; !strings.HasSuffix(out.String(), want) {
				t.Errorf("output does not end by erasing the list:\n%q", out.String())
			}
		})
	}
}

// Every line must fit the terminal width: a wrapped row occupies two physical
// rows but counts as one, which desyncs every later redraw and erase.
func TestRender_TruncatesToTerminalWidth(t *testing.T) {
	narrow := screen{columns: 40, rows: 24}
	list := []Item{{
		Label:  "a-very-long-branch-name-that-goes-on",
		Detail: "~/some/deep/path · 2h ago · 3 open comments · running",
	}}

	var out bytes.Buffer
	if _, err := run(strings.NewReader("q"), &out, strings.Repeat("P", 80), list, narrow); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}

	for _, line := range strings.Split(out.String(), "\r\n") {
		if visible := utf8.RuneCountInString(stripANSI(line)); visible > narrow.columns {
			t.Errorf("line is %d columns wide, want <= %d:\n%q", visible, narrow.columns, line)
		}
	}
}

func TestRender_ClampsRowsToTerminalHeight(t *testing.T) {
	short := screen{columns: 200, rows: 6}
	labels := make([]string, 10)
	for i := range labels {
		labels[i] = "item-" + string(rune('a'+i))
	}

	var out bytes.Buffer
	if _, err := run(strings.NewReader("q"), &out, "Pick", items(labels...), short); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}

	// 6 rows leaves 4 for items after the prompt and footer.
	if !strings.Contains(out.String(), "showing 1-4 of 10") {
		t.Errorf("short terminal should draw 4 rows:\n%s", out.String())
	}
	if strings.Contains(out.String(), "item-e") {
		t.Error("drew more rows than fit on the terminal")
	}
}

func TestVisibleRows(t *testing.T) {
	tests := map[screen]int{
		{columns: 80, rows: 50}: maxVisible, // capped by maxVisible
		{columns: 80, rows: 10}: 8,          // capped by terminal height
		{columns: 80, rows: 2}:  1,          // never less than one row
		{columns: 80, rows: 0}:  1,
	}
	for s, want := range tests {
		if got := s.visibleRows(); got != want {
			t.Errorf("screen%+v.visibleRows() = %d, want %d", s, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		text string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly-10", 10, "exactly-10"},
		{"truncate me", 8, "truncat…"},
		{"padded  tail", 8, "padded…"},
		{"anything", 1, "…"},
		{"anything", 0, ""},
		{"héllo wörld", 7, "héllo…"},
	}
	for _, tc := range tests {
		if got := truncate(tc.text, tc.max); got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.text, tc.max, got, tc.want)
		}
	}
}

// stripANSI removes escape sequences so a line's visible width can be measured.
func stripANSI(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '\033' {
			for i < len(line) && !isANSITerminator(line[i]) {
				i++
			}
			i++ // the terminator itself
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r != '\r' {
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

func isANSITerminator(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func TestSelect_RejectsEmptyList(t *testing.T) {
	if _, err := Select(nil, io.Discard, "Pick", nil); err == nil {
		t.Fatal("expected an error for an empty list")
	}
}

func TestSelect_RequiresATerminal(t *testing.T) {
	notATTY, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer notATTY.Close()

	if _, err := Select(notATTY, io.Discard, "Pick", items("one")); err == nil {
		t.Fatal("expected raw mode to fail on a non-terminal")
	}
}

func TestTerminalScreen_FallsBackWithoutATerminal(t *testing.T) {
	notATTY, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer notATTY.Close()

	if got := terminalScreen(notATTY); got != fallbackScreen {
		t.Errorf("terminalScreen() = %+v, want the fallback %+v", got, fallbackScreen)
	}
}
