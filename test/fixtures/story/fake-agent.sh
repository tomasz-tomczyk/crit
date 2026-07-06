#!/usr/bin/env bash
# fake-agent.sh — a canned `agent_cmd` for story-mode tests and E2E.
#
# Usable as crit's `agent_cmd`: crit pipes the story prompt on stdin (or via a
# {prompt} arg). This script ignores the prompt and prints a fixed story JSON
# to stdout, so tests can exercise the exec + JSON-extraction + ingest path
# without a real LLM. Later tasks (agent_cmd exec + JSON extraction) consume it.
#
# Override the emitted story with CRIT_FAKE_STORY_FILE=<path> to point at any
# canned JSON fixture (e.g. test/fixtures/story/valid-story.json).
set -euo pipefail

# Drain stdin so a writer piping the prompt doesn't get SIGPIPE.
cat >/dev/null 2>&1 || true

if [[ -n "${CRIT_FAKE_STORY_FILE:-}" ]]; then
  cat "${CRIT_FAKE_STORY_FILE}"
  exit 0
fi

cat <<'JSON'
{
  "version": 1,
  "agent": "fake-agent",
  "prologue": {
    "summary": "A canned story for tests.",
    "complexity": "low"
  },
  "chapters": [
    {
      "id": "ch1",
      "title": "Canned chapter",
      "summary": "Covers the single fixture hunk.",
      "hunk_refs": [{ "file_path": "app.go", "old_start": 0 }]
    }
  ]
}
JSON
