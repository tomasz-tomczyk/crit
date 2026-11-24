// Package migrate moves an existing install from the legacy layout
// (~/.crit.config.json and ~/.crit/) to the XDG folders, and nudges legacy
// users towards `crit migrate-xdg` on startup.
//
// It is transitional: once legacy installs are rare, remove this package, the
// migrate-xdg command and the startup hint (tracked in GitHub issue #1227; the
// legacy fallback in internal/userdirs is a separate decision).
package migrate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/userdirs"
)

// ErrNoXDG means the XDG folders are not in use, so there is nowhere to move
// to: Windows without XDG_CONFIG_HOME and XDG_STATE_HOME.
var ErrNoXDG = errors.New("the XDG folders are not in use here; set XDG_CONFIG_HOME and XDG_STATE_HOME to absolute paths to use them")

// rename is os.Rename; tests replace it to act as if the folders were on
// different file systems.
var rename = os.Rename

// configNames are the ~/.crit entries that belong in the config folder.
// Everything else in ~/.crit is state.
var configNames = map[string]bool{"prompts": true, "hooks": true, "custom.css": true}

// Move is one file or folder to move.
type Move struct {
	From, To string
}

// Skip is something the migration leaves for the user to move by hand.
type Skip struct {
	Path, Reason string
}

// Plan is what a migration would do. Conflicts are targets that already
// exist; a plan with any conflict is not run, so nothing is overwritten.
// Skipped entries stay where they are.
type Plan struct {
	Moves     []Move
	Conflicts []Move
	Skipped   []Skip
	LegacyDir string // ~/.crit, removed once empty
	ConfigDir string
	StateDir  string
}

// Empty reports whether there is nothing left of the legacy layout.
func (p Plan) Empty() bool {
	return len(p.Moves) == 0 && len(p.Conflicts) == 0 && len(p.Skipped) == 0 && !exists(p.LegacyDir)
}

// BuildPlan lists what moving home's legacy layout to the XDG folders would
// move: ~/.crit.config.json to the config folder as config.json; prompts,
// hooks and custom.css from ~/.crit to the config folder; everything else in
// ~/.crit to the state folder.
func BuildPlan(home string) (Plan, error) {
	p := Plan{
		LegacyDir: userdirs.LegacyDir(home),
		ConfigDir: userdirs.ConfigDir(home),
		StateDir:  userdirs.StateBaseDir(home),
	}
	if p.ConfigDir == "" || p.StateDir == "" {
		return p, ErrNoXDG
	}
	if legacy := userdirs.LegacyConfigFile(home); exists(legacy) {
		p.add(legacy, filepath.Join(p.ConfigDir, "config.json"))
	}
	if target, ok := symlinkTarget(p.LegacyDir); ok {
		// A linked ~/.crit (a dotfiles setup): moving its contents would
		// empty the folder it points at.
		p.Skipped = append(p.Skipped, Skip{Path: p.LegacyDir, Reason: "is a link to " + target + "; move its contents by hand"})
		return p, nil
	}
	entries, err := os.ReadDir(p.LegacyDir)
	if err != nil && !os.IsNotExist(err) {
		return p, fmt.Errorf("reading %s: %w", p.LegacyDir, err)
	}
	for _, e := range entries {
		dest := p.StateDir
		if configNames[e.Name()] {
			dest = p.ConfigDir
		}
		p.add(filepath.Join(p.LegacyDir, e.Name()), filepath.Join(dest, e.Name()))
	}
	return p, nil
}

func (p *Plan) add(from, to string) {
	if target, ok := symlinkTarget(from); ok && !filepath.IsAbs(target) {
		// A relative link would point somewhere else once moved.
		p.Skipped = append(p.Skipped, Skip{Path: from, Reason: "is a relative link to " + target + "; move it by hand"})
		return
	}
	m := Move{From: from, To: to}
	if occupied(to) {
		p.Conflicts = append(p.Conflicts, m)
		return
	}
	p.Moves = append(p.Moves, m)
}

// RunningReviews returns the daemons still running from any state root. A
// running daemon keeps writing to the legacy folders, so the migration waits.
func RunningReviews(home string) []daemon.SessionEntry {
	var alive []daemon.SessionEntry
	for _, root := range userdirs.StateDirs(home) {
		alive = append(alive, daemon.LiveSessionsIn(filepath.Join(root, "sessions"))...)
	}
	return alive
}

// Execute runs a plan without conflicts. It stops at the first failure and
// returns what it moved so far; the rest stays in the legacy layout, and
// running the migration again picks up where it stopped. ~/.crit is removed
// once it is empty.
func Execute(p Plan) ([]Move, error) {
	if len(p.Conflicts) > 0 {
		return nil, errors.New("the plan has conflicts")
	}
	var moved []Move
	for _, m := range p.Moves {
		if err := move(m.From, m.To); err != nil {
			return moved, fmt.Errorf("moving %s to %s: %w", m.From, m.To, err)
		}
		moved = append(moved, m)
	}
	if _, linked := symlinkTarget(p.LegacyDir); !linked && isEmptyDir(p.LegacyDir) {
		if err := os.Remove(p.LegacyDir); err != nil {
			return moved, fmt.Errorf("removing %s: %w", p.LegacyDir, err)
		}
	}
	return moved, nil
}

// move renames from to to, creating to's parent. An empty folder at to is
// replaced. Across file systems (a custom XDG_STATE_HOME on another disk) it
// copies, keeping modification times so the stale-review sweep still sees
// the real age, and removes from only after the copy is complete.
func move(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if isEmptyDir(to) {
		if err := os.Remove(to); err != nil {
			return err
		}
	}
	err := rename(from, to)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(from, to); err != nil {
		os.RemoveAll(to)
		return err
	}
	return os.RemoveAll(from)
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(path, target, info.Mode().Perm()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: cannot copy this kind of file", path)
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
}

func copyFile(from, to string, perm os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func symlinkTarget(path string) (string, bool) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return "", false
	}
	return target, true
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// occupied reports whether moving to path would replace something. An empty
// folder does not count: it holds nothing to lose.
func occupied(path string) bool {
	return exists(path) && !isEmptyDir(path)
}

func isEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

// Tilde shortens a path under home to ~/… for display.
func Tilde(home, path string) string {
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~/" + filepath.ToSlash(rel)
}
