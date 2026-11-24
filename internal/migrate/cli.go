package migrate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/userdirs"
)

// runningReviews is RunningReviews; tests replace it.
var runningReviews = RunningReviews

// RunMigrateXDG is `crit migrate-xdg [--dry-run]`.
func RunMigrateXDG(args []string) error {
	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run", "-n":
			dryRun = true
		default:
			return clicmd.Usage("Usage: crit migrate-xdg [--dry-run]")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}
	return run(os.Stdout, home, dryRun)
}

func run(w io.Writer, home string, dryRun bool) error {
	p, err := BuildPlan(home)
	if err != nil {
		return clicmd.ExitError{Code: 1, Err: err}
	}
	if p.Empty() {
		fmt.Fprintf(w, "Nothing to migrate: crit already uses %s and %s.\n", Tilde(home, p.ConfigDir), Tilde(home, p.StateDir))
		return nil
	}

	if len(p.Conflicts) > 0 {
		var b strings.Builder
		b.WriteString("Nothing was moved. These already exist in the new folders:\n")
		for _, c := range p.Conflicts {
			fmt.Fprintf(&b, "  %s  (from %s)\n", Tilde(home, c.To), Tilde(home, c.From))
		}
		b.WriteString("Merge each one by hand (keep the copy you want and delete the other), then run crit migrate-xdg again.")
		return clicmd.ExitError{Code: 1, Err: errors.New(b.String())}
	}

	if len(p.Moves) == 0 {
		fmt.Fprintln(w, "Nothing was moved.")
		printSkipped(w, home, p.Skipped)
		return nil
	}

	if dryRun {
		if len(p.Moves) > 0 {
			fmt.Fprintln(w, "Would move:")
			printMoves(w, home, p.Moves)
		}
		printSkipped(w, home, p.Skipped)
		return nil
	}

	if running := runningReviews(home); len(running) > 0 {
		return clicmd.ExitError{Code: 1, Err: fmt.Errorf("nothing was moved: %d review%s still running; stop them with crit stop --all, then run crit migrate-xdg again",
			len(running), clicmd.Plural(len(running)))}
	}

	moved, err := Execute(p)
	if len(moved) > 0 {
		fmt.Fprintln(w, "Moved:")
		printMoves(w, home, moved)
	}
	if err != nil {
		return clicmd.ExitError{Code: 1, Err: fmt.Errorf("stopped: %w; what was not moved is still in the old location, so fix the problem and run crit migrate-xdg again", err)}
	}
	printSkipped(w, home, p.Skipped)
	removeHintStamp()
	fmt.Fprintf(w, "Done. crit now reads its config from %s and keeps its state in %s.\n", Tilde(home, p.ConfigDir), Tilde(home, p.StateDir))
	return nil
}

func printMoves(w io.Writer, home string, moves []Move) {
	for _, m := range moves {
		fmt.Fprintf(w, "  %s → %s\n", Tilde(home, m.From), Tilde(home, m.To))
	}
}

func printSkipped(w io.Writer, home string, skipped []Skip) {
	if len(skipped) == 0 {
		return
	}
	fmt.Fprintln(w, "Left in place:")
	for _, s := range skipped {
		fmt.Fprintf(w, "  %s %s\n", Tilde(home, s.Path), s.Reason)
	}
}

// hintInterval is how often the startup hint may show.
const hintInterval = 7 * 24 * time.Hour

// hintStampPath records when the startup hint last showed. It lives in the
// user cache folder so that showing the hint never creates ~/.crit or the XDG
// folders.
var hintStampPath = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "crit", "migrate-xdg-hint"), nil
}

// MaybeHint prints a one-line nudge to run crit migrate-xdg on a legacy
// install, at most once every hintInterval. The caller decides whether a
// person is watching (a terminal on stderr): agents never see it.
func MaybeHint(w io.Writer, home string, now time.Time) {
	if userdirs.ConfigDir(home) == "" || userdirs.StateBaseDir(home) == "" {
		return
	}
	if !userdirs.IsLegacyInstall(home) {
		return
	}
	stamp, err := hintStampPath()
	if err != nil {
		return
	}
	if info, err := os.Stat(stamp); err == nil && now.Sub(info.ModTime()) < hintInterval {
		return
	}
	fmt.Fprintf(w, "crit: your crit files are in the old location (~/.crit). Run crit migrate-xdg to move them to %s and %s.\n",
		Tilde(home, userdirs.ConfigDir(home)), Tilde(home, userdirs.StateBaseDir(home)))
	if os.MkdirAll(filepath.Dir(stamp), 0o700) == nil {
		if os.WriteFile(stamp, nil, 0o600) == nil {
			os.Chtimes(stamp, now, now)
		}
	}
}

func removeHintStamp() {
	if stamp, err := hintStampPath(); err == nil {
		os.Remove(stamp)
	}
}
