package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func RunCleanup(args []string) error {
	days := 7
	force := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--days":
			if i+1 < len(args) {
				i++
				d, err := strconv.Atoi(args[i])
				if err != nil || d < 0 {
					fmt.Fprintf(os.Stderr, "Error: invalid --days value\n")
					return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
				}
				days = d
			}
		case "--force":
			force = true
		default:
			fmt.Fprintf(os.Stderr, "Usage: crit cleanup [--days N] [--force]\n")
			return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
	}

	revDir, err := daemon.ReviewsDir()
	if err != nil {
		return err
	}

	stale := findStaleReviews(revDir, days)
	if len(stale) == 0 {
		fmt.Println("No stale review files found.")
		return nil
	}

	fmt.Printf("Found %d stale review file%s:\n", len(stale), clicmd.Plural(len(stale)))
	for _, s := range stale {
		fmt.Printf("  %s  (%s%d days old, %d comment%s)\n", s.path, s.metaLabel(), int(s.age.Hours()/24), s.comments, clicmd.Plural(s.comments))
	}

	if !force {
		fmt.Print("Delete all? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	deleted := deleteStaleReviews(stale)
	fmt.Printf("Deleted %d review file%s.\n", deleted, clicmd.Plural(deleted))
	return nil
}

func removeStaleReviewPath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return os.RemoveAll(path) == nil
	}
	if err := os.Remove(path); err != nil {
		return false
	}
	_ = os.Remove(path + ".snapshots.json")
	return true
}

type staleReview struct {
	key        string
	path       string
	branch     string
	reviewType string
	origin     string
	age        time.Duration
	comments   int
}

func (s staleReview) metaLabel() string {
	if s.reviewType == "live" {
		if s.origin != "" {
			return "live: " + s.origin + ", "
		}
		return "live, "
	}
	if s.branch != "" {
		return s.branch + ", "
	}
	return ""
}

func findStaleReviews(revDir string, days int) []staleReview {
	entries, err := os.ReadDir(revDir)
	if err != nil {
		return nil
	}

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	activeSessions := buildActiveSessionSet()

	var stale []staleReview
	for _, de := range entries {
		name := de.Name()

		if de.IsDir() {
			// MIGRATION-REMOVAL: pre-fix early-v4 folders kept a stray .json
			// extension on the folder name. Strip it for the session-key
			// lookup; the standard load path will rename the folder on access.
			key := strings.TrimSuffix(name, ".json")
			if activeSessions[key] {
				continue
			}
			if sr, ok := checkStaleReviewFolder(revDir, de, key, cutoff); ok {
				stale = append(stale, sr)
			}
			continue
		}

		// MIGRATION-REMOVAL: legacy v3 flat *.json file. Treat as a stale
		// candidate so cleanup wipes it (and any sibling sidecar) the next
		// time crit runs. After the migration removal release this branch
		// goes away.
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		key := strings.TrimSuffix(name, ".json")
		if activeSessions[key] {
			continue
		}
		if sr, ok := checkStaleReview(revDir, de, key, cutoff); ok {
			stale = append(stale, sr)
		}
	}
	return stale
}

// checkStaleReviewFolder evaluates a directory entry inside the reviews dir.
// It is a v4-native staleness check for folder-form reviews. Three possible
// outcomes:
//
//  1. The folder contains review.json: read it, parse UpdatedAt, fall back
//     to file mtime if missing. Stale if the timestamp is before cutoff.
//  2. The folder lacks review.json but contains snapshots.json: it's an
//     orphan-snapshots folder (e.g. a crashed migration or a deleted review
//     left behind a sidecar). Stale if folder mtime is before cutoff.
//  3. Empty / unrecognized contents: skip.
func checkStaleReviewFolder(revDir string, de os.DirEntry, key string, cutoff time.Time) (staleReview, bool) {
	folder := filepath.Join(revDir, de.Name())
	reviewPath := filepath.Join(folder, "review.json")

	if data, readErr := os.ReadFile(reviewPath); readErr == nil {
		var cj session.CritJSON
		var updatedAt time.Time
		var branch string
		var reviewType string
		var origin string
		var commentCount int
		if json.Unmarshal(data, &cj) == nil {
			branch = cj.Branch
			reviewType = cj.ReviewType
			origin = cj.Origin
			if t, parseErr := time.Parse(time.RFC3339, cj.UpdatedAt); parseErr == nil {
				updatedAt = t
			}
			for _, f := range cj.Files {
				commentCount += len(f.Comments)
			}
			commentCount += len(cj.ReviewComments)
		}
		if updatedAt.IsZero() {
			if info, statErr := os.Stat(reviewPath); statErr == nil {
				updatedAt = info.ModTime()
			}
		}
		if !updatedAt.Before(cutoff) {
			return staleReview{}, false
		}
		return staleReview{
			key:        key,
			path:       folder,
			branch:     branch,
			reviewType: reviewType,
			origin:     origin,
			age:        time.Since(updatedAt),
			comments:   commentCount,
		}, true
	}

	// review.json missing — check for orphan snapshots folder.
	if _, err := os.Stat(filepath.Join(folder, "snapshots.json")); err != nil {
		return staleReview{}, false
	}
	info, err := de.Info()
	if err != nil {
		return staleReview{}, false
	}
	if !info.ModTime().Before(cutoff) {
		return staleReview{}, false
	}
	return staleReview{
		key:  key,
		path: folder,
		age:  time.Since(info.ModTime()),
	}, true
}

func buildActiveSessionSet() map[string]bool {
	sessDir, _ := daemon.SessionsDir()
	active := make(map[string]bool)
	sessEntries, err := os.ReadDir(sessDir)
	if err != nil {
		return active
	}
	for _, se := range sessEntries {
		if !strings.HasSuffix(se.Name(), ".json") {
			continue
		}
		key := strings.TrimSuffix(se.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(sessDir, se.Name()))
		if err != nil {
			continue
		}
		var entry daemon.SessionEntry
		if json.Unmarshal(data, &entry) == nil && daemon.IsDaemonAlive(entry) {
			active[key] = true
		}
	}
	return active
}

func checkStaleReview(revDir string, de os.DirEntry, key string, cutoff time.Time) (staleReview, bool) {
	path := filepath.Join(revDir, de.Name())
	info, err := de.Info()
	if err != nil {
		return staleReview{}, false
	}

	var updatedAt time.Time
	var branch string
	var commentCount int
	if data, readErr := os.ReadFile(path); readErr == nil {
		var cj session.CritJSON
		if json.Unmarshal(data, &cj) == nil {
			branch = cj.Branch
			if t, parseErr := time.Parse(time.RFC3339, cj.UpdatedAt); parseErr == nil {
				updatedAt = t
			}
			for _, f := range cj.Files {
				commentCount += len(f.Comments)
			}
			commentCount += len(cj.ReviewComments)
		}
	}
	if updatedAt.IsZero() {
		updatedAt = info.ModTime()
	}

	if !updatedAt.Before(cutoff) {
		return staleReview{}, false
	}
	return staleReview{
		key:      key,
		path:     path,
		branch:   branch,
		age:      time.Since(updatedAt),
		comments: commentCount,
	}, true
}

func deleteStaleReviews(stale []staleReview) int {
	sessDir, _ := daemon.SessionsDir()
	deleted := 0
	for _, s := range stale {
		if !session.RemoveStaleReviewPath(s.path) {
			fmt.Fprintf(os.Stderr, "Error deleting %s: directory not empty or path missing\n", s.path)
			continue
		}
		deleted++
		if sessDir != "" {
			os.Remove(filepath.Join(sessDir, s.key+".json"))
			os.Remove(filepath.Join(sessDir, s.key+".lock"))
			os.Remove(filepath.Join(sessDir, s.key+".log"))
		}
	}
	return deleted
}
