package vcs

import "strings"

// TopicChainSHAs returns commit SHAs on the current topic branch, excluding the default branch tip.
func TopicChainSHAs(v VCS, repoRoot string) map[string]bool {
	out := make(map[string]bool)
	if v == nil {
		return out
	}
	defaultBranch := v.DefaultBranch()
	if v.Name() == "git" {
		mergeBase, err := RunGitInDir(repoRoot, "merge-base", defaultBranch, "HEAD")
		if err != nil {
			return out
		}
		baseSHA := strings.TrimSpace(mergeBase)
		if baseSHA == "" {
			return out
		}
		revs, err := RunGitInDir(repoRoot, "rev-list", baseSHA+"..HEAD")
		if err != nil {
			return out
		}
		for _, sha := range SplitNonEmpty(revs) {
			out[sha] = true
		}
		return out
	}
	if v.Name() == "jj" {
		revs, err := JJCommandInDir(repoRoot, "log", "-r", JJTopicChainRevset(repoRoot, 0), "--no-graph", "-T", "commit_id ++ \"\\n\"")
		if err != nil {
			return out
		}
		for _, sha := range SplitNonEmpty(revs) {
			out[sha] = true
		}
		return out
	}
	revs, err := SLCommandInDir(repoRoot, "log", "-r", "draft() & ::.", "-T", "{node}\n")
	if err != nil {
		return out
	}
	for _, sha := range SplitNonEmpty(revs) {
		out[sha] = true
	}
	return out
}

// CommitSubjectFor returns a short commit subject for sha using the given VCS backend.
func CommitSubjectFor(v VCS, repoRoot, sha string) string {
	if v == nil {
		return ""
	}
	var subject string
	switch v.Name() {
	case "git":
		out, err := RunGitInDir(repoRoot, "log", "-1", "--format=%s", sha)
		if err != nil {
			return ""
		}
		subject = strings.TrimSpace(out)
	case "jj":
		subject = JJCommitSubject(repoRoot, sha)
	default:
		out, err := SLCommandInDir(repoRoot, "log", "-r", sha, "-T", "{desc|firstline}")
		if err != nil {
			return ""
		}
		subject = strings.TrimSpace(out)
	}
	if len(subject) > 60 {
		subject = subject[:60] + "\u2026"
	}
	return subject
}
