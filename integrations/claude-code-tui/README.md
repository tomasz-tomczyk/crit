# crit-tui

A crit review loop inside Claude Code, as a mod. Comment on lines of the
conversation, the working-tree diff, or a file in a pane. Send the review to
Claude in one go. Claude answers each comment in its thread through a tool.

**Early version.**

## Install

Needs Claude Code 2.1.287 or later (mods) and the `crit` CLI on your PATH.

```
/plugin marketplace add tomasz-tomczyk/crit
/plugin install crit-tui@crit
```

It sits next to the main `crit` plugin from the same marketplace; you can
have both. To try it from a checkout instead:

```sh
claude --plugin-dir integrations/claude-code-tui
```

## Use

- `/crit-tui` opens the pane (last source used, the conversation at first)
- `/crit-tui chat`, `/crit-tui diff`, `/crit-tui file <path>` pick the source
- `/crit-tui send` sends the review, `/crit-tui close` closes the pane

The pane is one `Client` region: it gets raw keys and mouse events. Click
it once to give it the keys (Esc gives them back to the prompt).

| input | does |
| --- | --- |
| click a line | move the cursor there |
| drag | select rows |
| click a thread | pick it for `r` / `x` |
| `j` `k`, ↑ ↓, wheel | move |
| shift+↑ ↓, or `v` then move | select rows |
| space / pagedown, ctrl+u / pageup | page |
| `g` `G`, home end | top, bottom |
| `n` `p`, → ← | next, previous message or file |
| `l`, tab | next thread |
| `c`, enter | comment on the line or selection |
| `r` | reply to the picked thread, or the one on this line |
| `x` | resolve it |
| `s` | send drafts and replies to Claude |
| `a` | ask Claude to review the diff and leave comments |
| `1` `2` `3` | conversation, diff, file |
| `h` | show or hide resolved threads |
| `f` | reload the source |

While typing a comment: enter saves, shift+enter adds a line, ctrl+u
clears. To cancel: enter or backspace on an empty box, or click `cancel`.
ctrl+c and Esc never reach the pane: Claude Code takes them.

While the pane is closed, a band above the prompt shows drafts and new
replies from Claude, with `send` and `open` buttons. The status line shows
the same counts. A reply in the transcript that has comments gets a line
under it with the count.

## What Claude gets

Three tools:

- `mcp__crit-tui__reply` `{ id, body, resolve? }`: answer a comment
- `mcp__crit-tui__comment` `{ file, line, end_line?, body }`: leave a comment on code
- `mcp__crit-tui__threads` `{ include_resolved? }`: list open threads

`send` submits one prompt with each new comment (where, quoted lines, body,
id) and each new reply, and asks Claude to answer every item with `reply`.

## Where comments live

Everything goes to crit through its CLI, so the crit browser UI and
`crit comments` see the same review.

- **Diff and file comments** go to the repo's review file with
  `crit comment --json`. A partial selection carries `quote` and
  `quote_offset`, as a web UI text selection does (an older crit ignores
  the two fields).
- **Chat comments** go to a crit plan named `claude-chat-<session id>`.
  On the first chat comment the mod saves the conversation with
  `crit plan --name <slug> --no-wait` (one row per line), then comments
  with `crit comment --plan <slug>`. After each turn it saves again; crit
  adds a version only when the text changed. Sessions with no chat comments
  create nothing. Needs a crit with `crit plan --no-wait`; with an older
  crit, chat comments stay in the session.
- **Claude's replies** are written with `--author Claude`.
- Without the crit CLI (setting `critBin`), all comments stay in the session.

## How the pane is built

- `hooks/register.tsx` owns the data (documents, threads, crit sync) and
  draws the pane as one `<Client module="./client.tsx">`.
- `hooks/client.tsx` owns the cursor, selection, scrolling and the comment
  box, and posts each change (`comment`, `reply`, `resolve`, `send`, ...) to
  the hooks module through `ui.message`.
- A Client's props are capped near 100k characters, so it gets a window of
  rows around the cursor and asks for a new window near the edges.
- The engine ignores a `setState` made while the Client draws, so the
  Client keeps its state in a module variable and calls `setState` only
  from its key and mouse handlers.

## Developing

```sh
claude plugin validate integrations/claude-code-tui
claude plugin test integrations/claude-code-tui
```

## Limits of this early version

- crit paths are taken as repo-root relative. Start Claude Code at the repo root.
- Removed (`-`) diff lines anchor to the new-side line they sit at; crit has
  no old-side anchor in `crit comment --json`.
- `crit comment --plan` leaves `anchor` empty (crit bug), so a chat comment
  does not follow its text if earlier lines of the conversation change.
- Reopening a resolved crit thread is left to the crit browser UI.
- The diff is `git diff HEAD` plus up to 20 untracked files.
