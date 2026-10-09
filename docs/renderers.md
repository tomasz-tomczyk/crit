# Renderers

A renderer turns source text into a visual view that you can still comment on
line by line. Renderers ship with Crit: today there is one, Mermaid, and new
ones (for example Arazzo workflows) are added to Crit's codebase the same way.
Crit does not load renderer scripts from outside its embedded assets.

## Built-in: Mermaid

- ` ```mermaid ` fences in markdown documents render as diagrams, and so do
  fences in story text and in comments shown in Document view. Comments on
  diff lines may show the fence as code.
- `mermaid.min.js` loads only when the first diagram renders. A review with
  no Mermaid fence or file never downloads it.
- `.mermaid` and `.mmd` files (the
  [extensions recommended by Mermaid](https://mermaid.js.org/ecosystem/integrations-create.html#file-extension))
  open with a **Diagram / Source** toggle. In files mode they open on
  Diagram. In git mode they open on Source, which is the diff.
- In a Mermaid file, click a node, edge label or actor to comment on the line
  that defines it. A comment on a ` ```mermaid ` fence covers the whole fence:
  use the gutter. The gutter also comments on the whole file.
- **Expand** opens the diagram fullscreen, with pan and zoom.

If a diagram fails to render, Crit shows its source with the parse error, and
the rest of the review keeps working.

## Kinds

| Kind | Selected by | Where it shows |
| --- | --- | --- |
| `fence` | `langs`: info strings of a code fence (` ```mermaid `) | markdown documents (Document and Rendered views), story text, comments shown in Document view (not comments on diff lines) |
| `file` | `paths`: globs on the file path (`*.mmd`, `**/*.arazzo.yaml`) | a `<label> / Source` toggle in the file header |

File renderers apply only to files Crit treats as code. A glob that matches
markdown (`*.md`) is ignored: markdown keeps its Document / Diff toggle. A
deleted file keeps the deleted-file diff.

Each kind is a separate registration. To cover both, register twice with the
same `name`, as Mermaid does.

## Adding a renderer

A renderer is a module in `web/`, next to `crit-renderer-mermaid.js`:

1. Create `web/crit-renderer-<name>.js` that calls
   `window.crit.renderers.register(...)` (see the contract below).
2. Load it in `web/index.html` after `crit-renderers.js` and before
   `crit-line-blocks.js` / `app.js`.
3. Keep the page weight unchanged for reviews that do not need it: load a
   heavy library from `render()` on first use, not at page load (see
   `loadMermaid` in `crit-renderer-mermaid.js`: one cached Promise that
   injects the `<script>` and rejects if it fails to load).
4. Add a Node test in `web/__tests__/` and, for a file renderer, an E2E
   fixture.

## Contract

```js
// fenced block, selected by info string
window.crit.renderers.register({
  kind: 'fence',
  name: 'arazzo',               // unique per kind; registering it again replaces it
  langs: ['arazzo'],            // fence info strings (case-insensitive)
  zoomable: true,               // optional: Expand opens <svg> output fullscreen
  render(source, ctx) { ... },  // required
});

// whole file, selected by glob
window.crit.renderers.register({
  kind: 'file',
  name: 'arazzo',
  label: 'Workflow',            // toggle label (default "Rendered")
  paths: ['**/*.arazzo.yaml'],  // path globs
  zoomable: true,
  render(source, ctx) { ... },
  anchorFor(element, ctx) { ... } // optional
});
```

When two renderers of the same kind claim the same fence or file, the one
registered last wins.

### `render(source, ctx)`

Fill `ctx.container`, or return one of:

- an HTML string, inserted as is (you are responsible for escaping it);
- a DOM node;
- a Promise of either.

| `ctx` field | Meaning |
| --- | --- |
| `container` | element to render into (already in the page on first render) |
| `source` | the text: the fence body, or the whole file |
| `kind` | `'fence'` or `'file'` |
| `path` | the file under review (`''` for fences in comments or story text) |
| `startLine`, `endLine` | where `source` sits in the file (`0` when it is not part of a file) |
| `theme` | `'light'` or `'dark'` |
| `renderer` | the `name` of the renderer being called |
| `renderAs(name, source)` | render `source` with the fence renderer `name` into the same `container`; returns a Promise of that renderer's result (return it from your `render`). Rejects when no fence renderer has that name. |

Crit calls `render` again with a new container when the theme changes (also
for diagrams that were scrolled out of view at the time). If `render` throws
or rejects, Crit shows the source with the error message.

`renderAs` lets a file renderer convert its format and hand the result to
another renderer, for example an Arazzo workflow drawn as Mermaid:

```js
render(source, ctx) {
  return ctx.renderAs('mermaid', arazzoToMermaid(source));
},
```

In the called renderer, `ctx.startLine` / `ctx.endLine` are `0`: the derived
source is not file lines. Expand follows the outer renderer's `zoomable`.

### `anchorFor(element, ctx)` (file renderers)

Crit calls this when the user clicks inside a rendered file. Return
`{ startLine, endLine }`, 1-based lines counted **from the start of the
file**, and Crit opens a comment form on them. Return `null` to ignore the
click. Comments on the whole file go through the gutter as usual.

The result may also carry `quote`: text from those lines that the comment is
about, stored like the quote of a comment made from a text selection. Crit
drops a `quote` that is not found in the chosen lines.

Fence renderers have no `anchorFor`: `anchorFor` on a fence registration is
ignored, and a comment on a rendered fence covers the whole fence, through
the gutter. Line-level anchoring inside fences is planned with #862.

Comments are always stored against source lines, so `crit comment`, the agent
loop and GitHub/GitLab sync work the same with or without renderers.

### Example: a file renderer

```js
// web/crit-renderer-steps.js — renders *.steps.txt as a numbered list
window.crit.renderers.register({
  kind: 'file',
  name: 'steps',
  label: 'Steps',
  paths: ['**/*.steps.txt'],
  render(source) {
    const ol = document.createElement('ol');
    // Number from the top of the file; blank lines are skipped, not renumbered.
    source.replace(/\n$/, '').split('\n').forEach((text, i) => {
      if (!text.trim()) return;
      const li = document.createElement('li');
      li.textContent = text;
      li.dataset.line = i + 1;
      ol.appendChild(li);
    });
    return ol;
  },
  anchorFor(el) {
    const li = el.closest('li[data-line]');
    return li ? { startLine: +li.dataset.line, endLine: +li.dataset.line } : null;
  },
});
```

## crit-web parity

crit-web renders shared reviews with the same modules. Renderer changes need
these files vendored into crit-web:

- `web/crit-renderers.js`
- `web/crit-renderer-mermaid.js`
- `web/crit-diagram-overlay.js`
- `web/crit-glob-match.js`
- `web/mermaid.min.js`

A new built-in renderer adds its own `crit-renderer-<name>.js` (and any
library it loads) to that list.

## Limits

- Renderers run in the review page itself, with the page's privileges.
- Renderers cannot read other files in the session yet.
