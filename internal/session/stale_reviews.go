package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

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
			key := strings.TrimSuffix(name, ".json")
			if activeSessions[key] {
				continue
			}
			if sr, ok := checkStaleReviewFolder(revDir, de, key, cutoff); ok {
				stale = append(stale, sr)
			}
			continue
		}

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

func checkStaleReviewFolder(revDir string, de os.DirEntry, key string, cutoff time.Time) (staleReview, bool) {
	folder := filepath.Join(revDir, de.Name())
	reviewPath := filepath.Join(folder, "review.json")

	if data, readErr := os.ReadFile(reviewPath); readErr == nil {
		var cj CritJSON
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
		var cj CritJSON
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
		if !removeStaleReviewPath(s.path) {
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
