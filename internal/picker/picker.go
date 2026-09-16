// Package picker renders a single-choice list on an interactive terminal.
package picker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	ansiDim     = "\033[2m"
	ansiReverse = "\033[7m"
	ansiReset   = "\033[0m"
	hideCursor  = "\033[?25l"
	showCursor  = "\033[?25h"

	// maxVisible caps how many rows are drawn at once. Longer lists scroll.
	maxVisible = 12
)

// ErrCancelled is returned when the user dismisses the picker without choosing.
var ErrCancelled = errors.New("selection cancelled")

// Item is one row. A non-empty Note explains why the row cannot be chosen and
// marks it unselectable.
type Item struct {
	Label  string
	Detail string
	Note   string
}

func (i Item) disabled() bool { return i.Note != "" }

// Select puts in into raw mode, draws the list on out, and returns the index of
// the chosen item. It returns ErrCancelled if the user backs out.
func Select(in *os.File, out io.Writer, prompt string, items []Item) (int, error) {
	if len(items) == 0 {
		return 0, errors.New("no items to choose from")
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return 0, fmt.Errorf("entering raw mode: %w", err)
	}
	defer func() {
		_ = term.Restore(int(in.Fd()), state)
		fmt.Fprint(out, showCursor)
	}()
	fmt.Fprint(out, hideCursor)
	return run(in, out, prompt, items)
}

// run is Select without the terminal setup, so tests can drive it with a
// scripted key sequence.
func run(in io.Reader, out io.Writer, prompt string, items []Item) (int, error) {
	l := &list{items: items}
	l.cursor = l.firstSelectable()
	reader := bufio.NewReader(in)

	drawn := 0
	for {
		drawn = l.render(out, prompt, drawn)
		key, err := readKey(reader)
		if err != nil {
			erase(out, drawn)
			return 0, err
		}
		switch key {
		case keyUp:
			l.move(-1)
		case keyDown:
			l.move(1)
		case keyCancel:
			erase(out, drawn)
			return 0, ErrCancelled
		case keyEnter:
			if !l.items[l.cursor].disabled() {
				erase(out, drawn)
				return l.cursor, nil
			}
		}
	}
}

// erase rewinds over the drawn frame and clears it, so the list does not stay
// on screen behind whatever the caller prints next.
func erase(out io.Writer, drawn int) {
	if drawn == 0 {
		return
	}
	fmt.Fprintf(out, "\033[%dA\r\033[J", drawn)
}

type list struct {
	items  []Item
	cursor int
	offset int
}

func (l *list) firstSelectable() int {
	for i, item := range l.items {
		if !item.disabled() {
			return i
		}
	}
	return 0
}

// move advances the cursor by delta, skipping unselectable rows and stopping at
// the ends rather than wrapping.
func (l *list) move(delta int) {
	for i := l.cursor + delta; i >= 0 && i < len(l.items); i += delta {
		if !l.items[i].disabled() {
			l.cursor = i
			return
		}
	}
}

// window returns the slice bounds to draw, scrolling to keep the cursor in view.
func (l *list) window() (start, end int) {
	if len(l.items) <= maxVisible {
		return 0, len(l.items)
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+maxVisible {
		l.offset = l.cursor - maxVisible + 1
	}
	return l.offset, l.offset + maxVisible
}

// render draws the list, first erasing the previous drawn lines. It returns the
// number of lines it wrote.
func (l *list) render(out io.Writer, prompt string, drawn int) int {
	var b strings.Builder
	if drawn > 0 {
		fmt.Fprintf(&b, "\033[%dA", drawn)
	}

	writeLine(&b, prompt)
	lines := 1

	start, end := l.window()
	width := labelWidth(l.items[start:end])
	for i := start; i < end; i++ {
		writeLine(&b, l.row(i, width))
		lines++
	}

	footer := "↑/↓ move · enter resume · q cancel"
	if end-start < len(l.items) {
		footer = fmt.Sprintf("%s · showing %d-%d of %d", footer, start+1, end, len(l.items))
	}
	writeLine(&b, ansiDim+footer+ansiReset)
	lines++

	fmt.Fprint(out, b.String())
	return lines
}

func (l *list) row(i, width int) string {
	item := l.items[i]
	label := item.Label + strings.Repeat(" ", width-utf8.RuneCountInString(item.Label))
	trailing := item.Detail
	if item.disabled() {
		trailing = item.Note
	}

	text := fmt.Sprintf("%s  %s", label, trailing)
	switch {
	case item.disabled():
		return "    " + ansiDim + text + ansiReset
	case i == l.cursor:
		return "  " + ansiReverse + "❯ " + text + " " + ansiReset
	default:
		return "    " + text
	}
}

// writeLine erases the current line before writing, so a shorter row cannot
// leave characters behind from the previous frame.
func writeLine(b *strings.Builder, text string) {
	b.WriteString("\r\033[K")
	b.WriteString(text)
	b.WriteString("\r\n")
}

func labelWidth(items []Item) int {
	width := 0
	for _, item := range items {
		if n := utf8.RuneCountInString(item.Label); n > width {
			width = n
		}
	}
	return width
}

type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyEnter
	keyCancel
)

// readKey maps one keypress to a picker action. Arrow keys arrive as a
// three-byte escape sequence; a lone ESC (nothing buffered behind it) cancels.
func readKey(r *bufio.Reader) (key, error) {
	c, err := r.ReadByte()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return keyNone, ErrCancelled
		}
		return keyNone, err
	}
	switch c {
	case '\r', '\n':
		return keyEnter, nil
	case 'k':
		return keyUp, nil
	case 'j':
		return keyDown, nil
	case 'q', 3, 4: // q, Ctrl-C, Ctrl-D
		return keyCancel, nil
	case 27:
		return readEscape(r)
	}
	return keyNone, nil
}

func readEscape(r *bufio.Reader) (key, error) {
	if r.Buffered() == 0 {
		return keyCancel, nil
	}
	if c, err := r.ReadByte(); err != nil || c != '[' {
		return keyNone, nil //nolint:nilerr // an unknown sequence is a no-op, not a failure
	}
	c, err := r.ReadByte()
	if err != nil {
		return keyNone, nil //nolint:nilerr // truncated sequence, ignore
	}
	switch c {
	case 'A':
		return keyUp, nil
	case 'B':
		return keyDown, nil
	}
	return keyNone, nil
}
