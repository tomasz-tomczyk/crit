package comment

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

type commentsListFlags struct {
	outputDir    string
	plan         string
	jsonOutput   bool
	all          bool
	explicitPath string
}

func parseCommentsListFlags(args []string) (commentsListFlags, error) {
	var f commentsListFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--plan":
			val, err := clicmd.RequireFlagValue(args, i, "--plan")
			if err != nil {
				return f, err
			}
			f.plan = val
			i++
		case "--output", "-o":
			val, err := clicmd.RequireFlagValue(args, i, arg)
			if err != nil {
				return f, err
			}
			f.outputDir = val
			i++
		case "--json":
			f.jsonOutput = true
		case "--all":
			f.all = true
		default:
			if len(arg) > 0 && arg[0] == '-' {
				return f, fmt.Errorf("unknown flag: %s", arg)
			}
			if f.explicitPath != "" {
				return f, fmt.Errorf("unexpected extra argument: %s", arg)
			}
			f.explicitPath = arg
		}
	}
	return f, nil
}

func resolveCommentsListFlags(f *commentsListFlags) error {
	if f.plan != "" {
		if f.outputDir != "" {
			return fmt.Errorf("--plan and --output cannot be used together")
		}
		if f.explicitPath != "" {
			return fmt.Errorf("--plan and an explicit review path cannot be used together")
		}
		var err error
		f.outputDir, err = session.PlanStorageDir(session.Slugify(f.plan))
		if err != nil {
			return err
		}
	}
	return nil
}

func resolveCommentsCritPath(f commentsListFlags) (string, error) {
	if f.explicitPath != "" {
		return resolveExplicitReviewPath(f.explicitPath)
	}
	return review.ResolveReviewPath(f.outputDir)
}

func resolveExplicitReviewPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(path, "review.json")); err == nil {
			abs, err := filepath.Abs(path)
			if err != nil {
				return "", err
			}
			return abs, nil
		}
		return review.ResolveReviewPath(path)
	}
	if filepath.Base(path) == "review.json" {
		abs, err := filepath.Abs(filepath.Dir(path))
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	return "", fmt.Errorf("expected review.json or .crit directory, got %q", path)
}

// RunComments lists comments from the current or specified review file.
func RunComments(args []string) error {
	f, err := parseCommentsListFlags(args)
	if err != nil {
		return err
	}
	if err := resolveCommentsListFlags(&f); err != nil {
		return err
	}

	critPath, err := resolveCommentsCritPath(f)
	if err != nil {
		return err
	}

	cj, err := loadCritJSON(critPath)
	if err != nil {
		return err
	}

	unresolvedOnly := !f.all
	entries := listCommentsFromCritJSON(cj, unresolvedOnly)
	if f.jsonOutput {
		data, err := encodeCommentsJSON(entries)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Println(formatCommentsText(entries, unresolvedOnly))
	return nil
}
