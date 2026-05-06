// Resolve the per-port fixture state file path. Mirrors the e2e_state_file
// shell helper in ../lib.sh — both must agree on the location, otherwise
// the Node side can't read what the bash setup script wrote.
//
// On Windows, "/tmp/..." resolves differently for Git Bash (mapped to a
// Windows tempdir) versus Node (literal C:\tmp), so we keep state files
// inside e2e/.state/ — a directory both sides agree on.
//
// Override with CRIT_E2E_STATE_DIR (must match what the shell side sees).
import * as path from 'node:path';

export function stateFilePath(port: string | number): string {
  const dir = process.env.CRIT_E2E_STATE_DIR
    ?? path.resolve(__dirname, '..', '.state');
  return path.join(dir, `crit-e2e-state-${port}`);
}
