package main

import (
	"fmt"
	"io"
	"os"
)

const (
	ansiDim   = "\033[2m"
	ansiGreen = "\033[32m"
	ansiReset = "\033[0m"
)

// Status handles formatted terminal output for the crit review lifecycle.
type Status struct {
	w     io.Writer
	color bool
}

func newStatus(w io.Writer) *Status {
	color := true
	if os.Getenv("NO_COLOR") != "" {
		color = false
	} else if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			color = false
		}
	} else {
		// Not a file (e.g. bytes.Buffer in tests) — no color
		color = false
	}
	return &Status{w: w, color: color}
}

func (s *Status) dim(text string) string {
	if s.color {
		return ansiDim + text + ansiReset
	}
	return text
}

func (s *Status) green(text string) string {
	if s.color {
		return ansiGreen + text + ansiReset
	}
	return text
}

func (s *Status) arrow() string {
	return s.dim("→")
}

// Listening prints the server URL on startup.
func (s *Status) Listening(url string) {
	fmt.Fprintf(s.w, "  %s\n", s.dim("Listening on "+url))
}

// RoundFinished prints the round summary and finish confirmation.
func (s *Status) RoundFinished(round, commentCount int, hasPrompt bool) {
	if commentCount > 0 {
		noun := "comments"
		if commentCount == 1 {
			noun = "comment"
		}
		fmt.Fprintf(s.w, "%s Round %d: %d %s added\n", s.arrow(), round, commentCount, noun)
	}
	if hasPrompt {
		fmt.Fprintf(s.w, "%s Finish review — prompt copied %s\n", s.arrow(), s.green("✓"))
	} else {
		fmt.Fprintf(s.w, "%s Finish review\n", s.arrow())
	}
}

// WaitingForAgent prints the waiting state.
func (s *Status) WaitingForAgent() {
	fmt.Fprintf(s.w, "%s %s\n", s.arrow(), s.dim("Waiting for agent…"))
}

// FileUpdated prints the edit detection summary. Skips output for 0 edits.
func (s *Status) FileUpdated(editCount int) {
	if editCount == 0 {
		return
	}
	noun := "edits"
	if editCount == 1 {
		noun = "edit"
	}
	fmt.Fprintf(s.w, "%s %s\n", s.arrow(), s.dim(fmt.Sprintf("File updated (%d %s detected)", editCount, noun)))
}

// RoundReady prints the new round summary with resolved/open counts.
func (s *Status) RoundReady(round, resolved, open int) {
	line := fmt.Sprintf("Round %d: diff ready", round)
	if resolved > 0 && open > 0 {
		line += " — " + s.green(fmt.Sprintf("%d resolved", resolved)) + fmt.Sprintf(", %d open", open)
	} else if resolved > 0 {
		line += " — " + s.green(fmt.Sprintf("%d resolved", resolved))
	} else if open > 0 {
		line += fmt.Sprintf(" — %d open", open)
	}
	fmt.Fprintf(s.w, "%s %s\n", s.arrow(), line)
}
