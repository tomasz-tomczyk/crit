# Crit — TODO

## Features (From Spec, Not Yet Implemented)

- [ ] **Comment collapse/expand**: Comments can be collapsed/expanded (spec section "Displayed comments"). Currently all comments are always expanded.
- [ ] **Empty/binary file handling**: Show user-friendly messages for empty files or non-markdown files (spec "Edge Cases").
- [ ] **Very large files**: Test with files up to ~10k lines, ensure no performance issues.

## New Features

- [ ] **Diff view between rounds**: When the file changes between review rounds, show a diff highlighting what the agent changed. Core to the multi-round workflow — lets you immediately see whether your comments were addressed.
- [ ] **Keyboard navigation**: `j`/`k` to jump between comments, `n` to start a new comment on the focused block, `e` to edit. Everything currently requires mouse interaction.
