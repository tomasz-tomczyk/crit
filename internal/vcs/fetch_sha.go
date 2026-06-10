package vcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// EnsureSHAFetched ensures sha is reachable in the local object store,
// attempting auto-fetch from origin (and forkURL when set) when missing.
func EnsureSHAFetched(vcsInst VCS, sha, repoRoot, forkURL string) error {
	if vcsInst == nil {
		return nil
	}
	if vcsInst.HasObject(sha, repoRoot) {
		return nil
	}
	if vcsInst.Name() == "sl" {
		return EnsureSHAFetchedSapling(vcsInst, sha, repoRoot, forkURL)
	}
	if vcsInst.Name() == "jj" {
		return EnsureSHAFetchedJJ(vcsInst, sha, repoRoot, forkURL)
	}
	if vcsInst.Name() != "git" {
		return fmt.Errorf("commit %s not present locally (auto-fetch not supported for vcs=%q)", sha, vcsInst.Name())
	}

	if err := tryGitFetch(repoRoot, "origin", sha); err == nil &&
		vcsInst.HasObject(sha, repoRoot) {
		return nil
	}

	if forkURL != "" {
		if err := tryGitFetch(repoRoot, forkURL, sha); err == nil &&
			vcsInst.HasObject(sha, repoRoot) {
			return nil
		}
		return fmt.Errorf("commit %s not present locally; tried origin and fork %s — manual fetch required", sha, forkURL)
	}
	return fmt.Errorf("commit %s not present locally; manual fetch required (run `git fetch <remote> %s`)", sha, sha)
}

// EnsureSHAFetchedJJ falls back to git fetch for colocated JJ/Git repos.
func EnsureSHAFetchedJJ(vcsInst VCS, sha, repoRoot, forkURL string) error {
	if hasGitDirAt(repoRoot) {
		if err := tryGitFetch(repoRoot, "origin", sha); err == nil && HasObject(sha, repoRoot) {
			return nil
		}
		if forkURL != "" {
			if err := tryGitFetch(repoRoot, forkURL, sha); err == nil && HasObject(sha, repoRoot) {
				return nil
			}
			return fmt.Errorf("commit %s not present locally; tried `git fetch origin %s` and `git fetch %s %s` for colocated JJ repo — manual fetch required", sha, sha, forkURL, sha)
		}
		return fmt.Errorf("commit %s not present locally; manual fetch required (run `git fetch <remote> %s` in the colocated JJ repo)", sha, sha)
	}
	if forkURL != "" {
		return fmt.Errorf("commit %s not present locally; PR head is on fork %s. Pure JJ auto-fetch is not supported — fetch it manually and re-run", sha, forkURL)
	}
	return fmt.Errorf("commit %s not present locally; pure JJ auto-fetch is not supported — fetch it manually and re-run", sha)
}

// EnsureSHAFetchedSapling tries `sl pull -r <sha>` first, then git fetch fallbacks.
func EnsureSHAFetchedSapling(vcsInst VCS, sha, repoRoot, forkURL string) error {
	if err := trySLPull(repoRoot, sha); err == nil &&
		HasObject(sha, repoRoot) {
		return nil
	}
	if hasGitDirAt(repoRoot) {
		if err := tryGitFetch(repoRoot, "origin", sha); err == nil &&
			HasObject(sha, repoRoot) {
			return nil
		}
		if forkURL != "" {
			if err := tryGitFetch(repoRoot, forkURL, sha); err == nil &&
				HasObject(sha, repoRoot) {
				return nil
			}
			return fmt.Errorf("commit %s not present locally; tried `sl pull -r %s`, `git fetch origin %s`, and `git fetch %s %s` — manual fetch required", sha, sha, sha, forkURL, sha)
		}
	}
	if forkURL != "" {
		return fmt.Errorf("commit %s not present locally; PR head is on fork %s. Pure sapling can't fetch by URL — run `sl pull %s` (configure the fork as a path first if needed) and re-run", sha, forkURL, forkURL)
	}
	return fmt.Errorf("commit %s not present locally; tried `sl pull -r %s` and `git fetch origin %s` — run `sl pull` manually with the right source", sha, sha, sha)
}

func tryGitFetch(repoRoot, remote, sha string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "fetch", remote, sha)
	cmd.Dir = repoRoot
	return cmd.Run()
}

func trySLPull(repoRoot, sha string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sl", "pull", "-r", sha)
	cmd.Dir = repoRoot
	return cmd.Run()
}

func hasGitDirAt(repoRoot string) bool {
	info, err := os.Stat(filepath.Join(repoRoot, ".git"))
	return err == nil && info.IsDir()
}
