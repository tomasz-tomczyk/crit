package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// TestConcurrentSaveCritJSON_NoCorruption stress-tests the saveCritJSON
// write path under concurrent writers. Probes gap #23 (concurrent
// pull/push corruption): if saveCritJSON's atomicity is ever broken,
// the file will fail to parse mid-run.
//
// Production calls saveCritJSON from at least: the daemon's debounced
// writer, comment_cli direct writes, share/unpublish, and crit pull/push.
// These can interleave when a daemon and a CLI run at the same time.
//
// The test does not assert ordering — last-writer-wins is acceptable.
// It only asserts that the file is always valid JSON at every observable
// moment (read-modify-write loop) and at the end.
func TestConcurrentSaveCritJSON_NoCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.json")

	// Seed with a non-empty CritJSON so workers have something real to
	// round-trip. An empty Files map is fine — the marshal/unmarshal pair
	// is what we want to stress.
	seed := CritJSON{
		Branch:  "test",
		BaseRef: "main",
		Files: map[string]CritJSONFile{
			"a.go": {Status: "modified", FileHash: "h1", Comments: []Comment{}},
		},
	}
	if err := saveCritJSON(path, seed); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	const writers = 4
	const iters = 50

	var wg sync.WaitGroup
	var failures atomic.Int64
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				data, err := readFileShared(reviewPathsFor(path).Review)
				if err != nil {
					t.Errorf("worker %d read iter %d: %v", id, i, err)
					failures.Add(1)
					return
				}
				var local CritJSON
				if err := json.Unmarshal(data, &local); err != nil {
					t.Errorf("worker %d parse iter %d: %v\n%s", id, i, err, data)
					failures.Add(1)
					return
				}
				if err := saveCritJSON(path, local); err != nil {
					t.Errorf("worker %d save iter %d: %v", id, i, err)
					failures.Add(1)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	if failures.Load() > 0 {
		t.Fatalf("%d worker failures — concurrent save path is not safe", failures.Load())
	}

	// Final parse must succeed — if atomicity broke, the file will be
	// truncated, partially written, or contain interleaved bytes.
	data, err := os.ReadFile(reviewPathsFor(path).Review)
	if err != nil {
		t.Fatal(err)
	}
	var final CritJSON
	if err := json.Unmarshal(data, &final); err != nil {
		t.Fatalf("FILE CORRUPTED after concurrent saves: %v\n%s", err, data)
	}
	if _, ok := final.Files["a.go"]; !ok {
		t.Errorf("seeded file entry lost: %+v", final.Files)
	}
}
