package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/picker"

	"golang.org/x/term"
)

// Production wiring for the interactive path; tests replace these.
var (
	selectResumeTarget = picker.Select
	runReviewForResume = RunReview
	stdinIsTerminal    = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// resumableReview is one review folder under ~/.crit/reviews, whether or not a
// daemon is still serving it.
type resumableReview struct {
	key        string
	cwd        string
	branch     string
	cliArgs    []string
	reviewType string
	origin     string
	unresolved int
	updatedAt  time.Time
	running    bool
}

// RunResume lists stored reviews and hands the chosen one to RunReview, which
// connects to its daemon or restarts it.
func RunResume(args []string) error {
	listOnly, id, passthrough, err := parseResumeArgs(args)
	if err != nil {
		return err
	}
	if id != "" {
		return runReviewForResume(append(passthrough, "--session", id))
	}

	reviews, err := listResumableReviews()
	if err != nil {
		return err
	}
	if len(reviews) == 0 {
		fmt.Println("No reviews to resume. Run crit in a repository to start one.")
		return nil
	}
	if listOnly {
		printResumableReviews(os.Stdout, reviews)
		return nil
	}
	if !stdinIsTerminal() {
		printResumableReviews(os.Stderr, reviews)
		return clicmd.ExitError{Code: 1, Err: errors.New("crit resume needs a terminal to show the picker; pass a session ID or use --list")}
	}

	// The picker draws on stderr so stdout stays clean for the review output
	// that follows.
	index, err := selectResumeTarget(os.Stdin, os.Stderr, "Resume a review", resumeItems(reviews))
	if err != nil {
		if errors.Is(err, picker.ErrCancelled) {
			return nil
		}
		return err
	}
	return runReviewForResume(append(passthrough, "--session", reviews[index].key))
}

// parseResumeArgs splits out the flags resume handles itself. Everything else
// is forwarded to RunReview, so `crit resume --no-open` works.
func parseResumeArgs(args []string) (listOnly bool, id string, passthrough []string, err error) {
	for _, arg := range args {
		switch {
		case arg == "--list" || arg == "-l":
			listOnly = true
		case daemon.ValidSessionKey(arg):
			if id != "" {
				return false, "", nil, clicmd.Usage("crit resume accepts at most one session ID")
			}
			id = arg
		default:
			passthrough = append(passthrough, arg)
		}
	}
	return listOnly, id, passthrough, nil
}

// listResumableReviews reads every review folder under ~/.crit/reviews, newest
// first. Reviews stored elsewhere (crit plan, or --output) are not listed:
// their identity path cannot be discovered from the reviews directory.
func listResumableReviews() ([]resumableReview, error) {
	dir, err := daemon.ReviewsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading reviews directory: %w", err)
	}

	running := runningSessionKeys()
	var reviews []resumableReview
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		if !daemon.ValidSessionKey(key) {
			continue
		}
		review, ok := readResumableReview(filepath.Join(dir, entry.Name()), key)
		if !ok {
			continue
		}
		review.running = running[key]
		reviews = append(reviews, review)
	}
	sort.Slice(reviews, func(i, j int) bool {
		return reviews[i].updatedAt.After(reviews[j].updatedAt)
	})
	return reviews, nil
}

func runningSessionKeys() map[string]bool {
	_, keys := daemon.ListAllSessions()
	running := make(map[string]bool, len(keys))
	for _, key := range keys {
		running[key] = true
	}
	return running
}

// readResumableReview parses one review folder. Folders without a readable
// review.json (orphaned snapshots, partial writes) are skipped.
func readResumableReview(folder, key string) (resumableReview, bool) {
	reviewPath := ReviewPathsFor(folder).Review
	data, err := os.ReadFile(reviewPath)
	if err != nil {
		return resumableReview{}, false
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return resumableReview{}, false
	}

	unresolved, _ := countComments(cj)
	review := resumableReview{
		key:        key,
		cwd:        cj.CWD,
		branch:     cj.Branch,
		cliArgs:    cj.CliArgs,
		reviewType: cj.ReviewType,
		origin:     cj.Origin,
		unresolved: unresolved,
	}
	if t, err := time.Parse(time.RFC3339, cj.UpdatedAt); err == nil {
		review.updatedAt = t
	} else if info, err := os.Stat(reviewPath); err == nil {
		review.updatedAt = info.ModTime()
	}
	return review, true
}

// label describes what the review covers: the live origin, the PR/range focus,
// the reviewed files, or the branch.
func (r resumableReview) label() string {
	if r.reviewType == "live" || r.reviewType == "preview" {
		if r.origin != "" {
			return r.reviewType + ": " + r.origin
		}
		return r.reviewType
	}
	if len(r.cliArgs) > 0 {
		return strings.Join(r.cliArgs, " ")
	}
	if r.branch != "" {
		return r.branch
	}
	return r.key
}

// detail is the dimmed context after the label: where the review lives, how
// stale it is, and what is waiting in it.
func (r resumableReview) detail() string {
	parts := []string{displayPath(r.cwd)}
	if !r.updatedAt.IsZero() {
		parts = append(parts, humanizeAge(time.Since(r.updatedAt)))
	}
	if r.unresolved > 0 {
		parts = append(parts, fmt.Sprintf("%d open comment%s", r.unresolved, clicmd.Plural(r.unresolved)))
	}
	if r.running {
		parts = append(parts, "running")
	}
	return strings.Join(parts, " · ")
}

// unavailable reports why a review cannot be resumed, or "" when it can.
func (r resumableReview) unavailable() string {
	if r.cwd == "" {
		return ""
	}
	if info, err := os.Stat(r.cwd); err != nil || !info.IsDir() {
		return "directory is gone — " + r.cwd
	}
	return ""
}

func resumeItems(reviews []resumableReview) []picker.Item {
	items := make([]picker.Item, len(reviews))
	for i, review := range reviews {
		items[i] = picker.Item{
			Label:  review.label(),
			Detail: review.detail(),
			Note:   review.unavailable(),
		}
	}
	return items
}

func printResumableReviews(w io.Writer, reviews []resumableReview) {
	fmt.Fprintf(w, "Reviews you can resume: %d\n", len(reviews))
	for _, review := range reviews {
		fmt.Fprintf(w, "  %s  %s\n", review.key, review.label())
		detail := review.detail()
		if note := review.unavailable(); note != "" {
			detail = note
		}
		fmt.Fprintf(w, "    %s\n", detail)
	}
	fmt.Fprintln(w, "\nResume one with: crit resume <id>")
}

// displayPath shortens a home-relative path to ~/… and names the directory a
// review without a recorded cwd will resume in.
func displayPath(path string) string {
	if path == "" {
		return "current directory"
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
}

func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
