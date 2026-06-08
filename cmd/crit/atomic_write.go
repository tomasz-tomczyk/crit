package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to the target path atomically using a
// same-directory tempfile + fsync + rename. On POSIX rename is atomic, so a
// crash mid-write cannot leave a truncated file at target. Parent
// directories are created with mode 0700 if missing. The final file is
// chmod'd to perm regardless of umask.
//
// Used as the canonical write path for review files (saveCritJSON), session
// state (writeSessionFile), config (saveConfigFile), plan-session mappings
// (savePlanSlug), and aider integration files (installAider). Keep this
// function's signature stable — multiple call sites depend on it.
func atomicWriteFile(target string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(target)+"*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := renameAtomic(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp file to target: %w", err)
	}
	return nil
}
