# PlanReview — TODO

## Features (From Spec, Not Yet Implemented)

- [ ] **Comment collapse/expand**: Comments can be collapsed/expanded (spec section "Displayed comments"). Currently all comments are always expanded.
- [ ] **WebSocket heartbeat**: Detect if browser tab is closed, log a message to terminal (spec says optional).
- [ ] **File watching**: Auto-reload UI when source `.md` file changes on disk (spec says nice-to-have for v1).
- [ ] **Heading anchors**: Subtle link icon on hover for headings (spec says nice-to-have).
- [ ] **Empty/binary file handling**: Show user-friendly messages for empty files or non-markdown files (spec "Edge Cases").
- [ ] **Very large files**: Test with files up to ~10k lines, ensure no performance issues.

## UI Refinements

- [ ] **Gutter line range display**: For multi-line blocks (code blocks, tables), the gutter shows only the start line. Could show a range like `41-58` or show line numbers for each sub-line.
- [ ] **Comment form scroll into view**: After opening a comment form, scroll it into view if it's off-screen.
- [ ] **Drag selection visual feedback**: During gutter drag, highlight the full range of selected blocks more prominently.
- [ ] **Mobile/responsive**: Basic responsive CSS exists but untested on small screens.

## Future Enhancements (Post-v1)

- [ ] **GitHub Actions release workflow**: Cross-compile binaries on tagged releases (spec has a workflow sketch).
- [ ] **Homebrew tap**: `brew install planreview`.
- [ ] **Comment resolution**: Mark comments as "resolved" (like GitHub), visually collapsed but still in review file.
- [ ] **Diff view**: After the AI agent updates the plan, show what changed alongside original comments.
- [ ] **Multiple reviewers**: Support `--author "Tom"` for team review scenarios.
- [ ] **Configurable comment format**: Let users customize the review comment prefix in output.
- [ ] **Export formats**: Export comments as GitHub Issues, Linear tickets, or TODO list.
