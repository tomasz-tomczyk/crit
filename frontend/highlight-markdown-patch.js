/*
 * crit-markdown-patch v1 — patched highlight.js markdown grammar.
 *
 * Re-registers the 'markdown' language with three upstream bug fixes:
 *
 *  - hljs#4279: snake_case identifiers (e.g. flutter_eval, :no_entry:, _id)
 *    were wrongly italicized because the underscore variant of ITALIC/BOLD
 *    didn't enforce intraword boundaries. CommonMark requires `_` emphasis
 *    to be preceded by non-alphanumeric on the open side and followed by
 *    non-alphanumeric on the close side. Asterisk emphasis is unchanged.
 *    Reference fix: highlightjs/highlight.js PR #4342 (danvk).
 *
 *  - hljs#3719: A bare line of `***` (or `---`, `___`) was being matched as
 *    BOLD opener instead of HORIZONTAL_RULE because (a) HORIZONTAL_RULE's
 *    matcher accepted any line starting with 3+ `-`/`*` (substring match,
 *    not full line) and (b) BOLD came before HORIZONTAL_RULE in contains[].
 *    Fix: tighten HORIZONTAL_RULE to a full-line match (3+ identical
 *    `-`, `*`, or `_` characters, optional whitespace) and put it ahead of
 *    BOLD in the contains[] order.
 *
 *  - Italic/bold bleed across code spans: `` `*_id` fields — validate as
 *    *UUID* before any database query `` was rendering with italic starting
 *    at the `*` *inside* the backtick code span, eating the rest of the line
 *    including the closing backtick and the intended `*UUID*`. Root cause:
 *    in the contains[] array, BOLD/ITALIC were listed *before* CODE, so at
 *    each scan position hljs tried italic-open first and won, never giving
 *    the backtick code span a chance. Fix: move CODE before BOLD/ITALIC.
 *    Plus tightened the asterisk italic/bold patterns to require a closing
 *    delimiter on the *same line* (no `\n` in content) and to require the
 *    content to start and end with non-whitespace, per CommonMark
 *    left/right-flanking rules. This stops asterisk-glob patterns like
 *    src/[two-stars]/[star].go and similar inside string literals from
 *    opening an unterminated bold/italic run that bleeds across the line.
 *
 * Based on hljs 11.11.1 src/languages/markdown.js. When bumping
 * @highlightjs/cdn-assets, re-diff this file against the upstream grammar.
 *
 * Sentinel: CRIT_MD_PATCH_v1 — used by tests to confirm the patch is bundled.
 */
(function () {
  if (typeof hljs === 'undefined' || !hljs.registerLanguage) return;

  hljs.registerLanguage('markdown', function (hljs) {
    const regex = hljs.regex;

    const INLINE_HTML = {
      begin: /<\/?[A-Za-z_]/,
      end: '>',
      subLanguage: 'xml',
      relevance: 0
    };

    // Fix: hljs#3719 — full-line match required (prevents `***`/`---`/`___`
    // on their own line from being treated as a BOLD opener).
    const HORIZONTAL_RULE = {
      className: 'section',
      match: /^[ \t]*([-*_])([ \t]*\1){2,}[ \t]*$/m
    };

    const CODE = {
      className: 'code',
      variants: [
        { begin: '(`{3,})[^`](.|\\n)*?\\1`*[ ]*' },
        { begin: '(~{3,})[^~](.|\\n)*?\\1~*[ ]*' },
        { begin: '```', end: '```+[ ]*$' },
        { begin: '~~~', end: '~~~+[ ]*$' },
        { begin: '`.+?`' },
        {
          begin: '(?=^( {4}|\\t))',
          contains: [
            { begin: '^( {4}|\\t)', end: '(\\n)$' }
          ],
          relevance: 0
        }
      ]
    };

    const LIST = {
      className: 'bullet',
      begin: '^[ \t]*([*+-]|(\\d+\\.))(?=\\s+)',
      end: '\\s+',
      excludeEnd: true
    };

    const LINK_REFERENCE = {
      begin: /^\[[^\n]+\]:/,
      returnBegin: true,
      contains: [
        { className: 'symbol', begin: /\[/, end: /\]/, excludeBegin: true, excludeEnd: true },
        { className: 'link', begin: /:\s*/, end: /$/, excludeBegin: true }
      ]
    };

    const URL_SCHEME = /[A-Za-z][A-Za-z0-9+.-]*/;
    const LINK = {
      variants: [
        { begin: /\[.+?\]\[.*?\]/, relevance: 0 },
        { begin: /\[.+?\]\(((data|javascript|mailto):|(?:http|ftp)s?:\/\/).*?\)/, relevance: 2 },
        { begin: regex.concat(/\[.+?\]\(/, URL_SCHEME, /:\/\/.*?\)/), relevance: 2 },
        { begin: /\[.+?\]\([./?&#].*?\)/, relevance: 1 },
        { begin: /\[.*?\]\(.*?\)/, relevance: 0 }
      ],
      returnBegin: true,
      contains: [
        { match: /\[(?=\])/ },
        { className: 'string', relevance: 0, begin: '\\[', end: '\\]', excludeBegin: true, returnEnd: true },
        { className: 'link', relevance: 0, begin: '\\]\\(', end: '\\)', excludeBegin: true, excludeEnd: true },
        { className: 'symbol', relevance: 0, begin: '\\]\\[', end: '\\]', excludeBegin: true, excludeEnd: true }
      ]
    };

    // Fix: hljs#4279 — `__..__` underscore-bold must not match intraword
    // (e.g. `snake__case`). The lookahead in `begin` requires a valid closer
    // to exist on the same line; otherwise hljs would greedily pair the
    // opener with end-of-line and falsely bold an unterminated identifier.
    //
    // Asterisk variant: also require a closer on the same line so that
    // unclosed `**` runs (e.g. `**/*.go` inside a string literal) don't
    // open a bold span that bleeds to end-of-line / next match. Per
    // CommonMark, an opening `**` must be followed by a non-whitespace
    // char (already enforced); we additionally require the closer `**`
    // to appear on the same line (no `\n` between them).
    const BOLD = {
      className: 'strong',
      contains: [], // defined later
      variants: [
        {
          begin: /(?<![A-Za-z0-9])_{2}(?!\s)(?=[^\n]*_{2}(?![A-Za-z0-9]))/,
          end: /_{2}(?![A-Za-z0-9])/
        },
        {
          // Same-line closer lookahead. `[^\n]*?` is non-greedy and stops at
          // the first `**` on the line. Without this, `"src/**/*.go"` opens a
          // bold span and waits for any later `**` to close it.
          begin: /\*{2}(?!\s)(?=[^\n]*?\*{2})/,
          end: /\*{2}/
        }
      ]
    };

    // Fix: hljs#4279 — `_..._` underscore-italic must not match intraword
    // (e.g. `flutter_eval`, `:no_entry:`, `_id`). Same closer-lookahead trick
    // as BOLD: only open if a valid closer exists on this line; prevents
    // unterminated `_id` from getting wrapped to end-of-line.
    //
    // Asterisk variant: require a closer on the same line, otherwise an
    // unclosed `*` (e.g. the `*` inside `` `*_id` `` once nested-fence
    // re-tokenization strips the backticks, or `*.go` after `/`) bleeds
    // emphasis through the rest of the line. CommonMark allows intraword
    // `*` (e.g. `un*frigging*believable`), so no word-boundary flank — only
    // the same-line closer constraint and non-whitespace flanks.
    const ITALIC = {
      className: 'emphasis',
      contains: [], // defined later
      variants: [
        {
          // `(?<!\*)` blocks opening italic on the second `*` of a `**`
          // sequence — without this, `"src/**/*.go"` opens italic at the
          // second `*` (next char `/` passes `(?![*\s])`) and pairs with
          // the later `*` in `*.go`, italicising `*/*`. With it, we never
          // open italic mid-`**`. The `***bold italic***` case is still
          // handled because BOLD consumes the outer `**`/`**` first
          // (BOLD precedes ITALIC in variants, and hljs tries each variant
          // at every position) and the inner italic opens at a position
          // that has a non-`*` char before it.
          // `(?![*\s])` per CommonMark: opener must be followed by
          // non-whitespace, and we don't want it to be the start of `**`.
          // `(?=[^\n]*?\*)` requires a closing `*` on the same line.
          begin: /(?<!\*)\*(?![*\s])(?=[^\n]*?\*)/,
          end: /\*/
        },
        {
          begin: /(?<![A-Za-z0-9])_(?![_\s])(?=[^\n_]*_(?![A-Za-z0-9]))/,
          end: /_(?![A-Za-z0-9])/,
          relevance: 0
        }
      ]
    };

    // 3-level deep nesting is not allowed because it would create confusion
    // in cases like `***testing***` where we don't know if the last `***`
    // is starting a new bold/italic or finishing the last one.
    const BOLD_WITHOUT_ITALIC = hljs.inherit(BOLD, { contains: [] });
    const ITALIC_WITHOUT_BOLD = hljs.inherit(ITALIC, { contains: [] });
    BOLD.contains.push(ITALIC_WITHOUT_BOLD);
    ITALIC.contains.push(BOLD_WITHOUT_ITALIC);

    let CONTAINABLE = [INLINE_HTML, LINK];

    [BOLD, ITALIC, BOLD_WITHOUT_ITALIC, ITALIC_WITHOUT_BOLD].forEach(function (m) {
      m.contains = m.contains.concat(CONTAINABLE);
    });

    CONTAINABLE = CONTAINABLE.concat(BOLD, ITALIC);

    // Setext heading content MUST be a paragraph line per CommonMark §4.3 —
    // not a list item, blockquote, ATX header, indented code block, or HR.
    // Upstream hljs ignores this, so a YAML frontmatter block like:
    //   ---
    //   paths:
    //     - "src/**/*.go"
    //     - "internal/*.go"
    //   ---
    // matched the trailing `  - "internal/*.go"\n---` as a setext H2,
    // wrapping the bullet line and the closing fence in `hljs-section`.
    // The negative lookahead below excludes upper lines that start with
    // common block-level markers. It does NOT cover every CommonMark case
    // (HTML blocks, fenced code, link refs), but those are already handled
    // by other contains[] entries that consume them before HEADER gets a
    // chance — so in practice this fixes the reported failure modes.
    const HEADER = {
      className: 'section',
      variants: [
        { begin: '^#{1,6}', end: '$', contains: CONTAINABLE },
        {
          begin: '(?=^(?![ \\t]*([-*+>]|#|\\d+[.)])(?:\\s|$))(?! {4}|\\t).+?\\n[=-]{2,}[ \\t]*$)',
          contains: [
            { begin: '^[=-]*$' },
            { begin: '^', end: '\\n', contains: CONTAINABLE }
          ]
        }
      ]
    };

    const BLOCKQUOTE = {
      className: 'quote',
      begin: '^>\\s+',
      contains: CONTAINABLE,
      end: '$'
    };

    const ENTITY = {
      scope: 'literal',
      match: /&([a-zA-Z0-9]+|#[0-9]{1,7}|#[Xx][0-9a-fA-F]{1,6});/
    };

    return {
      name: 'Markdown',
      aliases: ['md', 'mkdown', 'mkd'],
      contains: [
        HEADER,
        // Fix: hljs#3719 — HORIZONTAL_RULE must come before BOLD so that a
        // bare line of `***`/`---`/`___` is consumed as a rule, not a bold
        // opener that swallows the rest of the document.
        HORIZONTAL_RULE,
        INLINE_HTML,
        // CODE must come before BOLD/ITALIC. hljs tries each contained
        // pattern at every scan position; whichever matches first wins. With
        // BOLD/ITALIC ahead of CODE, `*` inside a backtick code span (e.g.
        // `` `*_id` ``) is consumed by the italic-opener lookahead before
        // the backtick code span is recognized — italic then bleeds across
        // the closing backtick to end-of-line.
        CODE,
        LIST,
        BOLD,
        ITALIC,
        BLOCKQUOTE,
        LINK,
        LINK_REFERENCE,
        ENTITY
      ]
    };
  });
})();
