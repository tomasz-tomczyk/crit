package picker

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func items(labels ...string) []Item {
	list := make([]Item, len(labels))
	for i, label := range labels {
		list[i] = Item{Label: label, Detail: "detail"}
	}
	return list
}

func TestRun_EnterSelectsHighlightedItem(t *testing.T) {
	keys := strings.NewReader("\x1b[B\x1b[B\r")
	got, err := run(keys, io.Discard, "Pick", items("one", "two", "three"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 2 {
		t.Errorf("selected %d, want 2", got)
	}
}

func TestRun_VimKeysMove(t *testing.T) {
	keys := strings.NewReader("jjk\r")
	got, err := run(keys, io.Discard, "Pick", items("one", "two", "three"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1", got)
	}
}

func TestRun_CursorStopsAtEnds(t *testing.T) {
	keys := strings.NewReader("kkkk\r")
	got, err := run(keys, io.Discard, "Pick", items("one", "two"))
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
		"escape": "\x1b",
		"eof":    "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := run(strings.NewReader(keys), io.Discard, "Pick", items("one"))
			if !errors.Is(err, ErrCancelled) {
				t.Fatalf("err = %v, want ErrCancelled", err)
			}
		})
	}
}

func TestRun_SkipsDisabledItems(t *testing.T) {
	list := []Item{
		{Label: "gone", Note: "directory is gone"},
		{Label: "usable"},
		{Label: "also gone", Note: "directory is gone"},
	}

	got, err := run(strings.NewReader("\r"), io.Discard, "Pick", list)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1 — the cursor must start on a selectable item", got)
	}

	// Moving down from the only selectable row must not land on the disabled one.
	got, err = run(strings.NewReader("\x1b[B\r"), io.Discard, "Pick", list)
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
	_, err := run(strings.NewReader("\r\rq"), io.Discard, "Pick", list)
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
	if _, err := run(strings.NewReader("q"), &out, "Resume a review", list); !errors.Is(err, ErrCancelled) {
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
	if _, err := run(keys, &out, "Pick", items(labels...)); !errors.Is(err, ErrCancelled) {
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
			if _, err := run(strings.NewReader(keys), &out, "Pick", items("one", "two")); err != nil && !errors.Is(err, ErrCancelled) {
				t.Fatalf("run: %v", err)
			}
			// 1 prompt + 2 rows + 1 footer, rewound and cleared.
			if want := "\033[4A\r\033[J"; !strings.HasSuffix(out.String(), want) {
				t.Errorf("output does not end by erasing the list:\n%q", out.String())
			}
		})
	}
}

func TestSelect_RejectsEmptyList(t *testing.T) {
	if _, err := Select(nil, io.Discard, "Pick", nil); err == nil {
		t.Fatal("expected an error for an empty list")
	}
}
