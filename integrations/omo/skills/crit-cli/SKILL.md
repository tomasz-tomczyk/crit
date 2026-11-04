---
name: crit-cli
description: Use when an agent needs to author or reply to crit inline comments programmatically (including multi-agent workflows commenting on shared code/plans/docs/proposals), publish or unpublish a crit review with crit share, sync a crit review to or from a GitHub PR or GitLab MR, or read/interpret a crit review JSON file. Covers crit comment, crit share, crit unpublish, crit pull, crit push, review file format, and resolution workflow. Not for invoking an interactive review loop — that's the `crit` skill.
user-invocable: false
---

# Crit CLI reference

Use this skill for headless operations, not to start an interactive review.
Comments have three scopes: `line` and `file` comments live under
`files.<path>.comments`; `review` comments live in `review_comments`.
The review file path is shown by `crit status`.

## Read comments and select a session

```bash
crit comments
crit comments --json
crit comments --all
crit comments --plan <slug>
crit comments [path]
crit status --json
```

Default comment output includes unresolved review-level and file comments.
Missing `resolved` means unresolved; only `true` means resolved. Read replies
before acting. `quote` narrows the requested change, `anchor` locates content
after edits, and `drifted: true` means line numbers may be approximate.

When multiple sessions match, headless commands refuse to guess. Use the
`sessions` from `crit status --json` to select `--session <id>` for
`comment`, `comments`, `share`, `pull`, or `push`.

## Review JSON

```json
{
  "review_comments": [
    {
      "id": "r_a1b2c3",
      "scope": "review",
      "body": "Overall feedback",
      "resolved": false,
      "replies": []
    }
  ],
  "files": {
    "src/example.go": {
      "comments": [
        {
          "id": "c_d4e5f6",
          "scope": "line",
          "start_line": 5,
          "end_line": 10,
          "body": "Inline feedback",
          "quote": "selected text",
          "anchor": "Original source lines",
          "author": "Reviewer",
          "resolved": false,
          "replies": [
            {"id": "rp_a1b2c3", "body": "Updated", "author": "OMO"}
          ]
        }
      ]
    }
  }
}
```

File comments use `scope: "file"` and `start_line: 0`.

## Author and reply

```bash
crit comment --author 'OMO' '<review-level body>'
crit comment --author 'OMO' <path> '<file-level body>'
crit comment --author 'OMO' <path>:<line> '<body>'
crit comment --author 'OMO' <path>:<start>-<end> '<body>'
crit comment --reply-to <id> --author 'OMO' '<what changed>'
```

Always pass `--author 'OMO'`. Single-quote bodies to protect backticks and
shell metacharacters. Source lines are 1-indexed file-on-disk lines, not
diff line numbers. Reply bodies support markdown.

Only use `--resolve` when the user explicitly asks. The same restriction
applies to the `resolve` field in JSON.

Plan-mode reviews live under `~/.crit/plans/<slug>/`. Always use
`--plan <slug>` when replying; the slug is in the feedback prompt.

```bash
crit comment --plan <slug> --reply-to <id> --author 'OMO' '<what changed>'
```

If an ID occurs in multiple files, disambiguate with `--path <path>` or the
`file` field in a bulk entry.

## Bulk comments and replies

For three or more comments, write a JSON array using the file-edit tool:

```json
[
  {"scope": "review", "body": "Overall feedback"},
  {"path": "src/example.go", "scope": "file", "body": "File feedback"},
  {"file": "src/example.go", "line": "5-10", "body": "Inline feedback"},
  {"reply_to": "c_d4e5f6", "body": "Updated"}
]
```

```bash
crit comment --json --file /tmp/crit-comments.json --author 'OMO'
```

Bulk writes are atomic. `--file -` reads stdin. Each entry requires `body`.
`line` accepts an integer or range string; `end_line` is optional.
`file` and `path` are aliases. Per-entry `author` overrides `--author`.
Optional `quote` and `quote_offset` describe a selected substring.

Scope is inferred: `reply_to` means reply; no file/path or line means review;
path without a line means file; file/path plus line means line.

## Forge sync

```bash
crit pull [number-or-url]
crit push --dry-run [number-or-url]
crit push --viewed [number-or-url]
crit pull --viewed [number-or-url]
crit push --event comment [-m '<body>'] [number-or-url]
crit push --event approve [number-or-url]
crit push --event request-changes [number-or-url]
crit pull --forge gitlab <iid>
```

Use authenticated `gh` for GitHub or `glab` for GitLab. The current branch
can supply the PR/MR number. Only post reviews when the user asks.

## Sharing

```bash
crit share <file> [file...]
crit share --share-url <url> <file>
crit share --org <slug> --visibility unlisted <file>
crit unpublish [file...]
```

Only share or unpublish when requested. Relay the complete printed URL.
Use `--qr` only in real monospace terminals. Organization visibility defaults
to `organization`; other values are `unlisted` and `public`.
`--share-url` chooses the deployment; it is required if multiple configured
targets have no default. Unpublish uses the persisted delete token.
