package vcs

import (
	"fmt"
	"os/exec"
	"strings"
)

// CompareTargets lists revision names the user may pick as the diff comparison
// point. Local vs remote grouping is VCS-specific (git branches, jj bookmarks).
type CompareTargets struct {
	VCS      string   `json:"vcs"`
	Detected string   `json:"detected"`
	Local    []string `json:"local"`
	Remote   []string `json:"remote"`
}

// CompareTargetsFor returns grouped compare targets for v. When v is nil, returns
// zero values.
func CompareTargetsFor(v VCS, dir string) (CompareTargets, error) {
	if v == nil {
		return CompareTargets{}, nil
	}
	switch v.Name() {
	case "jj":
		return jjCompareTargets(v, dir)
	case "sl":
		return saplingCompareTargets(v, dir)
	default:
		return gitCompareTargets(v, dir)
	}
}

func gitCompareTargets(v VCS, dir string) (CompareTargets, error) {
	local, err := LocalBranches(dir)
	if err != nil {
		return CompareTargets{}, err
	}
	remote, err := v.RemoteBranches(dir)
	if err != nil {
		return CompareTargets{}, err
	}
	return CompareTargets{
		VCS:      "git",
		Detected: v.DefaultBranch(),
		Local:    local,
		Remote:   remote,
	}, nil
}

func saplingCompareTargets(v VCS, dir string) (CompareTargets, error) {
	remote, err := v.RemoteBranches(dir)
	if err != nil {
		return CompareTargets{}, err
	}
	return CompareTargets{
		VCS:      "sl",
		Detected: v.DefaultBranch(),
		Local:    nil,
		Remote:   remote,
	}, nil
}

func jjCompareTargets(v VCS, dir string) (CompareTargets, error) {
	local, err := JJLocalBookmarks(dir)
	if err != nil {
		return CompareTargets{}, err
	}
	remote, err := v.RemoteBranches(dir)
	if err != nil {
		return CompareTargets{}, err
	}
	return CompareTargets{
		VCS:      "jj",
		Detected: v.DefaultBranch(),
		Local:    local,
		Remote:   remote,
	}, nil
}

// LocalBranches returns local branch short names (git refs/heads).
func LocalBranches(dir string) ([]string, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("for-each-ref heads: %w", err)
	}
	var branches []string
	for _, line := range SplitNonEmpty(string(out)) {
		if line != "" && line != "HEAD" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// JJLocalBookmarks returns bookmark names tracked locally (not remote-tracking).
func JJLocalBookmarks(dir string) ([]string, error) {
	out, err := JJCommandInDir(dir, "bookmark", "list", "-T", "name")
	if err != nil {
		return nil, nil //nolint:nilerr // no bookmarks yet is fine
	}
	seen := map[string]bool{}
	var names []string
	for _, line := range SplitNonEmpty(out) {
		name := strings.TrimSpace(line)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}
