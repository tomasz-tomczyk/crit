package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// makeBenchCritJSON builds a review shaped like a large session: nFiles files
// with nComments comments each. Bodies are ~200 bytes so the payload is
// dominated by structure, not by a single giant string — matching real
// reviews where JSON churn comes from comment count, not comment size.
func makeBenchCritJSON(nFiles, nComments int) CritJSON {
	cj := CritJSON{
		Branch:      "feature/bench",
		BaseRef:     "main",
		UpdatedAt:   "2026-09-04T00:00:00Z",
		ReviewRound: 2,
		Files:       make(map[string]CritJSONFile, nFiles),
	}
	body := "This is a review comment body with enough text to be realistic. " +
		"It spans a couple of sentences so marshal cost resembles production. "
	for f := 0; f < nFiles; f++ {
		path := fmt.Sprintf("pkg/file%03d.go", f)
		comments := make([]Comment, nComments)
		for i := range comments {
			comments[i] = Comment{
				ID:        fmt.Sprintf("c-%d-%d", f, i),
				StartLine: i*10 + 1,
				EndLine:   i*10 + 3,
				Body:      body,
				Author:    "reviewer",
				CreatedAt: "2026-09-04T00:00:00Z",
				UpdatedAt: "2026-09-04T00:00:00Z",
			}
		}
		cj.Files[path] = CritJSONFile{
			Status:   "modified",
			FileHash: "abc123",
			Comments: comments,
		}
	}
	return cj
}

// BenchmarkReviewSaveLoad measures the full review-file roundtrip
// (marshal + atomic write + read + unmarshal) that runs on every debounced
// save. This is the highest-risk Go-side regression surface: comment-count
// growth hits this path on every keystroke-debounced write.
//
// Run: go test -run='^$' -bench=BenchmarkReviewSaveLoad -benchmem ./internal/session/
func BenchmarkReviewSaveLoad(b *testing.B) {
	for _, tc := range []struct {
		name      string
		nFiles    int
		nComments int
	}{
		{"10x10", 10, 10},
		{"50x50", 50, 50},
	} {
		cj := makeBenchCritJSON(tc.nFiles, tc.nComments)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			dir := b.TempDir()
			// NOTE: b.N is the run's total iteration count, not a loop index —
			// use an explicit counter so each iteration gets a fresh identity
			// (fresh atomic-write + rename) instead of overwriting one path.
			i := 0
			for b.Loop() {
				// Each iteration writes to a fresh identity so AtomicWriteFile
				// rename cost is included and iterations don't share state.
				identity := filepath.Join(dir, fmt.Sprintf("review-%d", i))
				i++
				if err := SaveCritJSON(identity, cj); err != nil {
					b.Fatalf("SaveCritJSON: %v", err)
				}
				loaded, err := readCritJSONFromDisk(identity)
				if err != nil {
					b.Fatalf("readCritJSONFromDisk: %v", err)
				}
				if len(loaded.Files) != tc.nFiles {
					b.Fatalf("loaded %d files, want %d", len(loaded.Files), tc.nFiles)
				}
			}
		})
	}
}

// makeBenchPlanContent builds deterministic prev/curr content pairs shaped like
// a large AI-generated plan: n lines with every ~15th line changed.
func makeBenchPlanContent(n int) (prev, curr string) {
	prevLines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		switch i % 15 {
		case 3:
			prevLines = append(prevLines, fmt.Sprintf("## Section %d original title", i))
		case 7:
			prevLines = append(prevLines, fmt.Sprintf("- old item %d with some detail text", i))
		default:
			prevLines = append(prevLines, fmt.Sprintf("This is body line %d with enough text to look realistic.", i))
		}
	}
	prev = strings.Join(prevLines, "\n")
	currLines := make([]string, 0, n+10)
	for i, l := range prevLines {
		switch i % 15 {
		case 3:
			currLines = append(currLines, fmt.Sprintf("## Section %d revised title", i))
		case 7:
			currLines = append(currLines, fmt.Sprintf("- new item %d with some detail text", i))
			currLines = append(currLines, fmt.Sprintf("- inserted follow-up %d", i))
		default:
			currLines = append(currLines, l)
		}
	}
	return prev, strings.Join(currLines, "\n")
}

// BenchmarkCarryForward measures the round-complete comment remap for one
// large markdown file: ComputeLineDiff over prev/curr plus anchor remapping
// for every comment. This runs on the server before clients get file-changed,
// so one 10k-line plan costs ~one diff here (~250ms) and N large plans stack.
// A second accidental LCS pass (or heavier remap) shows up here first.
//
// Run: go test -run='^$' -bench=BenchmarkCarryForward -benchmem ./internal/session/
func BenchmarkCarryForward(b *testing.B) {
	prev, curr := makeBenchPlanContent(10000)
	comments := make([]Comment, 50)
	for i := range comments {
		comments[i] = Comment{
			ID:        fmt.Sprintf("c-%d", i),
			StartLine: i*100 + 1,
			EndLine:   i*100 + 3,
			Body:      "carried comment",
			Quote:     fmt.Sprintf("body line %d", i*100+1),
			CreatedAt: "2026-09-04T00:00:00Z",
			UpdatedAt: "2026-09-04T00:00:00Z",
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		// Fresh entry per iteration (carry-forward mutates in place). The
		// struct setup is nanoseconds against a ~250ms diff — negligible
		// pollution, and it keeps the b.Loop timer contract intact
		// (no StopTimer/StartTimer across iterations).
		f := &FileEntry{
			Path:             "plan.md",
			Status:           "modified",
			FileType:         "markdown",
			Content:          curr,
			PreviousContent:  prev,
			PreviousComments: comments,
		}
		s := &Session{}
		s.CarryForwardFileComments(f)
		if len(f.Comments) != len(comments) {
			b.Fatalf("carried %d comments, want %d", len(f.Comments), len(comments))
		}
	}
}
