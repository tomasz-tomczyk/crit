# Crit Roadmap

## Shipped

- Inline comments (single line + range)
- Split & unified diff between rounds
- AI review loop (finish review -> clipboard prompt -> agent edits -> diff)
- Vim keybindings
- Share reviews (crit.live)
- Syntax highlighting (13+ languages)
- Mermaid diagrams
- Suggestion mode
- Draft autosave
- Dark/light/system themes
- Table of contents
- `crit go <port>` agent signal

---

## Near-term

### Claude Code skill / hook integration

Developers describe a plan-then-review workflow as the highest-value pattern. Crit should integrate directly into Claude Code's plan mode so `crit` opens automatically when a plan is generated. A PostToolUse hook or custom skill that runs `crit <plan>.md` after plan creation would eliminate the manual step.

### Multi-file review

Agents generate plans that span multiple files. Support `crit plan.md spec.md` or `crit plans/` to open multiple documents in a tabbed view, with comments scoped per file but a single "Finish Review" that collects all feedback.

---

## Medium-term

### Persistent project knowledge base

Developers describe manually maintaining markdown files that accumulate corrections and context across sessions. Crit could generate a `crit-knowledge.md` after each review cycle that captures: what was changed, why, and what patterns to follow. This becomes a reusable context file for future sessions.

### Review templates / checklists

Common review patterns (security review, architecture review, API design review) as built-in templates. When opening a plan, optionally overlay a checklist (e.g., "Does this handle error cases? Are there missing edge cases? Is this consistent with existing patterns?").

### Cursor / Windsurf / Aider integration

Beyond Claude Code, provide integration guides or plugins for other popular AI coding tools. At minimum, document the workflow for each. Ideally, provide a `crit` MCP server that any MCP-compatible agent can call.

---

## Long-term / Exploratory

### Auto-review via LLM

`crit --auto-review plan.md` runs an LLM pass over the plan before the human reviews it. Pre-populates comments flagging: missing error handling, inconsistencies with existing code patterns, security concerns, overly complex approaches. Human reviews the LLM's comments alongside the plan.

### Crit as MCP server

Expose Crit's review workflow as an MCP server. Agents call `crit.open(file)` to trigger review, `crit.status()` to check if review is complete, and `crit.comments()` to retrieve feedback. Enables fully programmatic integration without clipboard.

### Review history / versioning

Track all rounds of review for a document. Show the evolution of a plan from initial draft through final version. Useful for post-mortems and understanding how a design decision was reached.

### Team review analytics

For teams using crit.live: aggregate data on review patterns, common comment types, average rounds per plan. Help teams understand their review discipline.
