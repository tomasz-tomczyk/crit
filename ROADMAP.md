# Crit Roadmap

## Shipped

- Inline comments (single line + range selection via gutter drag)
- Split & unified diff between review rounds
- AI review loop (finish review → clipboard prompt → agent edits → diff)
- Vim keybindings (`j`/`k`/`c`/`Shift+F`)
- Share reviews via crit.live
- Syntax highlighting (13+ languages)
- Mermaid diagrams
- Suggestion mode (before/after edits in comments)
- Draft autosave
- Dark/light/system themes
- Table of contents
- `crit go <port>` agent signal for multi-round workflow
- Agent integration configs for Claude Code, Cursor, Windsurf, Aider, Cline, GitHub Copilot
- Concurrent reviews (each instance on its own port)
- Live file watching via SSE (browser reloads when source changes)
- Real-time `.review.md` and `.comments.json` persistence

---

## Near-term

### Claude Code skill/hook integration

Close the last manual step in the review loop. A `PostToolUse` hook or custom skill that detects when a plan is written and runs `crit <plan>.md` automatically. The goal: plan appears in the browser before the agent can start implementing. Plannotator has this for Claude Code and it's clearly a major driver of adoption. Crit should match it with a zero-config path.

### Multi-file review

Agents generate plans that reference multiple files - a spec, an architecture doc, an API contract. Support `crit plan.md spec.md` or `crit plans/` to open multiple documents in a tabbed view. Comments scoped per file. Single "Finish Review" collects all feedback into a combined `.review.md`. This is one of the most-requested Plannotator features and a natural fit for crit's existing model.

### Plan version history and diffs

Show the evolution of a document across review rounds - not just the current diff, but a timeline of every round. Answers the question: "how did we get here?" Validates assumptions, helps post-mortems, gives the agent a clearer signal of what changed and when. Plannotator's open issues explicitly request this.

### Questions/answers flow

Allow comments to be marked as questions with an explicit expected answer. The agent's resolution acknowledges the question. After the round, unresolved questions are surfaced. This adds structure to the review loop beyond free-form comments - useful when reviewing architectural decisions that need explicit sign-off.

### More themes

Draft PR: https://github.com/tomasz-tomczyk/crit/pull/11

### Review completion sound

A subtle `AudioContext`-generated tone when the agent signals round complete. The browser tab is likely backgrounded - sound gets attention. Disable with `CRIT_NO_SOUND=1` env var. Small touch, high delight.

### Comment templates

A small "Insert template" dropdown in the comment form with pre-defined starters: "Consider using X instead of Y", "This will fail when...", "Missing error handling for...". Stored in `localStorage`, user-editable. Reduces friction of typing the same comment patterns.

### "Reviewing as..." identity pill (crit.live)

On shared reviews at crit.live, show a persistent header pill: `Reviewing as: Tomasz (purple)`. Makes multi-reviewer sessions feel collaborative rather than anonymous. The color-coding per identity already exists - this surfaces it explicitly.

---

## Long-term / Exploratory

### Crit as MCP server

Expose the review workflow as an MCP server. Any MCP-compatible agent calls `crit.open(file)` to trigger a review, `crit.status()` to check completion, and `crit.comments()` to retrieve feedback - no clipboard, no manual paste. This makes crit a first-class participant in automated agent pipelines and removes the last friction point for programmatic integration.

### Auto-review via LLM

`crit --auto-review plan.md` runs an LLM pass over the plan before the human sees it. Pre-populates comments flagging missing error handling, inconsistencies with existing patterns, security concerns, and overly complex approaches. Human reviews the LLM's comments alongside the plan - the human role shifts from first-pass reader to reviewer of a reviewer. The value is speed on large plans: the LLM surfaces the 20% that needs human attention.

### Review templates and checklists

When opening a plan, optionally overlay a checklist (security review, architecture review, API design review, performance review). Each checklist item is a structured comment that must be resolved before "Finish Review" is enabled. Turns ad-hoc review into a repeatable discipline. Useful as crit matures from personal tool to team workflow.

### Persistent project knowledge base

After each review cycle, generate or append to a `crit-knowledge.md` that captures what changed, why, and what patterns to follow. Becomes context for future sessions - hand it to the agent alongside new plans. Closes the loop between reviews: instead of each session starting cold, the agent accumulates institutional knowledge.

### Comment export formats

Export comments as GitHub PR review format, Jira issues, or structured JSON for downstream tooling. Useful when crit is embedded in larger workflows where review artifacts need to flow elsewhere.

### Review history and analytics (crit.live)

For teams using crit.live: aggregate data on review patterns, comment types, average rounds per plan. Surfaces which plan patterns generate the most review friction and helps teams calibrate their agent prompting. Enterprise-only, hosted, does not touch the CLI.

### File-based plans (daemon mode)

Support a persistent `crit` daemon that watches a directory for new `.md` files matching a pattern and automatically opens them for review. Eliminates the need to manually invoke `crit` per session - suitable for fully automated agent pipelines. Plannotator has open requests for this exact pattern.

### Self-hosted crit.live

`docker run crit-web` for teams that need private sharing without data leaving their infrastructure. The Phoenix app is already open source - provide a production-ready Docker image and configuration guide.
