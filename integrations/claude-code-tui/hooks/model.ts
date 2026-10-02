import type { SessionMessage } from 'claude-code'

import type { ChatIndex, ClientRow, ClientThread, Doc, Reply, ReviewProps, Row, Source, Thread, UiState } from '../types'

export const TOOL_PREFIX = 'mcp__crit-tui__'
export const MAX_ROWS = 6000
const TOOL_OUTPUT_LINES = 6

export const EMPTY_UI: UiState = {
  source: 'chat',
  startAt: 0,
  center: 0,
  resolvedView: 'line',
  filePath: null,
  docRev: 0,
  wheel: 0,
  wheelBy: 0,
}

export const emptyDoc = (source: Source, title: string, error?: string): Doc => ({
  source,
  title,
  rows: [],
  ...(error ? { error } : {}),
})

const ANSI = /\u001b\[[0-9;?]*[ -/]*[@-~]/g

export const stripAnsi = (text: string) => text.replace(ANSI, '').replace(/\t/g, '  ')

export const isClaude = (author: string) => /claude/i.test(author)

// ── Documents ───────────────────────────────────────────────────────────

/**
 * Turns `git diff` output into rows. `+` and context lines anchor to their
 * new line number; a `-` line anchors to the new-side position it sits at.
 */
export function parseDiff(text: string): Row[] {
  const rows: Row[] = []
  let path = ''
  let oldLine = 0
  let newLine = 0

  for (const raw of text.split('\n')) {
    if (rows.length >= MAX_ROWS) {
      rows.push({ kind: 'note', text: '… diff cut here' })
      break
    }

    if (raw.startsWith('diff --git ')) {
      const match = / b\/(.+)$/.exec(raw)
      path = match?.[1] ?? raw.slice(11)
      rows.push({ kind: 'title', text: path, path })
      continue
    }

    if (raw.startsWith('+++ ')) {
      const next = raw.slice(4).replace(/^b\//, '')
      if (next !== '/dev/null' && next !== path) {
        path = next
        const title = [...rows].reverse().find(row => row.kind === 'title')
        if (title) {
          title.text = path
          title.path = path
        }
      }
      continue
    }

    if (/^(--- |index |new file mode|deleted file mode|similarity |rename |old mode|new mode)/.test(raw)) {
      continue
    }

    const hunk = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/.exec(raw)
    if (hunk) {
      oldLine = Number(hunk[1])
      newLine = Number(hunk[2])
      rows.push({ kind: 'hunk', text: `@@ -${hunk[1]} +${hunk[2]} @@${hunk[3]}` })
      continue
    }

    if (raw.startsWith('Binary files')) {
      rows.push({ kind: 'note', text: raw })
      continue
    }

    if (raw.startsWith('\\') || path === '') {
      continue
    }

    const sign = raw[0]
    const body = stripAnsi(raw.slice(1))

    if (sign === '+') {
      rows.push({ kind: 'line', sign: '+', text: body, path, line: newLine, no: String(newLine) })
      newLine += 1
    } else if (sign === '-') {
      rows.push({ kind: 'line', sign: '-', text: body, path, line: Math.max(1, newLine), no: '' })
      oldLine += 1
    } else if (sign === ' ') {
      rows.push({ kind: 'line', sign: ' ', text: body, path, line: newLine, no: String(newLine) })
      newLine += 1
      oldLine += 1
    }
  }

  return rows
}

/** One line telling what a tool call did. */
export const REVIEW_HEAD = 'Crit review:'

/** This mod's own tool calls, and the lookup that loads them, are left out of the chat. */
function isCritPlumbing(tool: string, input: Record<string, unknown>): boolean {
  if (tool.startsWith(TOOL_PREFIX)) return true
  return tool === 'ToolSearch' && typeof input.query === 'string' && input.query.includes('crit-tui')
}

export function toolSummary(tool: string, input: Record<string, unknown>): string {
  const pick = (key: string) => (typeof input[key] === 'string' ? (input[key] as string) : '')
  const arg =
    pick('command') ||
    pick('file_path') ||
    pick('pattern') ||
    pick('path') ||
    pick('url') ||
    pick('description') ||
    JSON.stringify(input)

  const name = tool.startsWith('mcp__') ? tool.split('__').slice(1).join(':') : tool

  return `${name}(${arg.replace(/\s+/g, ' ').slice(0, 120)})`
}

/**
 * The conversation as rows: a title per message, its text lines anchored to
 * `chat:<index>`, and each tool call with the head of its output, anchored
 * to `chat:<index>/<tool_use_id>`.
 */
export function chatDoc(messages: readonly SessionMessage[]): { doc: Doc; index: ChatIndex } {
  const rows: Row[] = []
  const index: ChatIndex = []

  messages.forEach((message, at) => {
    const text = message.text.trim()
    const tools = message.toolUses.filter(use => !isCritPlumbing(use.tool, use.input))
    const hasTools = tools.length > 0

    // The review this mod sent shows as one line, not the whole prompt.
    if (message.role === 'user' && text.startsWith(REVIEW_HEAD)) {
      rows.push({ kind: 'note', text: `↳ you sent a crit review: ${text.split('\n')[0]?.slice(REVIEW_HEAD.length).trim()}`, role: 'user' })
      return
    }

    if (text === '' && !hasTools) {
      return
    }

    const path = `chat:${at}`

    if (text !== '') {
      index.push({ index: at, role: message.role, text })
      rows.push({
        kind: 'title',
        text: message.role === 'user' ? `you · #${at}` : `claude · #${at}`,
        role: message.role,
        path,
      })
      text.split('\n').forEach((line, n) => {
        rows.push({ kind: 'line', text: stripAnsi(line), role: message.role, path, line: n + 1, no: String(n + 1) })
      })
    }

    for (const use of tools) {
      const toolPath = `${path}/${use.tool_use_id}`
      rows.push({ kind: 'hunk', text: `⏺ ${toolSummary(use.tool, use.input)}`, role: 'tool', path: toolPath })

      const output = stripAnsi(use.text ?? '').split('\n').filter(line => line.trim() !== '')
      output.slice(0, TOOL_OUTPUT_LINES).forEach((line, n) => {
        rows.push({ kind: 'line', text: line, role: 'tool', path: toolPath, line: n + 1, no: use.isError ? '!' : '' })
      })
      if (output.length > TOOL_OUTPUT_LINES) {
        rows.push({ kind: 'note', text: `  … ${output.length - TOOL_OUTPUT_LINES} more lines`, role: 'tool' })
      }
    }
  })

  const doc: Doc =
    rows.length === 0
      ? emptyDoc('chat', 'Conversation', 'Nothing said yet.')
      : { source: 'chat', title: 'Conversation', rows }

  return { doc, index }
}

export function fileDoc(path: string, content: string): Doc {
  const lines = content.split('\n')
  if (lines.at(-1) === '') {
    lines.pop()
  }

  const rows: Row[] = [{ kind: 'title', text: path, path }]
  lines.slice(0, MAX_ROWS).forEach((line, n) => {
    rows.push({ kind: 'line', text: stripAnsi(line), path, line: n + 1, no: String(n + 1) })
  })

  return { source: 'file', title: path, rows }
}

// ── crit's review.json ──────────────────────────────────────────────────

type CritReply = { id?: string; body?: string; author?: string; created_at?: string }
type CritComment = CritReply & {
  start_line?: number
  end_line?: number
  quote?: string
  anchor?: string
  resolved?: boolean
  replies?: CritReply[]
}

/** Reads crit's review.json into code threads. */
export function critThreadsOf(json: string): Thread[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(json)
  } catch {
    return []
  }

  const files = (parsed as { files?: Record<string, { comments?: CritComment[] }> })?.files ?? {}
  const threads: Thread[] = []

  for (const [path, file] of Object.entries(files)) {
    for (const comment of file?.comments ?? []) {
      if (!comment.id) {
        continue
      }
      const author = comment.author ?? 'you'
      threads.push({
        id: comment.id,
        kind: 'code',
        path,
        start: comment.start_line ?? 0,
        end: comment.end_line ?? comment.start_line ?? 0,
        quote: comment.quote || comment.anchor || '',
        body: comment.body ?? '',
        author,
        fromClaude: isClaude(author),
        at: comment.created_at ?? '',
        resolved: comment.resolved === true,
        replies: (comment.replies ?? []).map(
          (reply): Reply => ({
            id: reply.id ?? '',
            author: reply.author ?? 'you',
            body: reply.body ?? '',
            at: reply.created_at ?? '',
            fromClaude: isClaude(reply.author ?? ''),
          }),
        ),
      })
    }
  }

  return threads
}

// ── The conversation as a crit plan ─────────────────────────────────────

/** The plan a session's chat comments live in. */
export const chatPlanSlug = (sessionId: string) => `claude-chat-${sessionId.replace(/[^a-z0-9]/gi, '').slice(0, 8).toLowerCase()}`

/** What a chat row adds before its text in the saved markdown. */
export function chatLinePrefix(row: Row): string {
  if (row.kind === 'title') return '### '
  if (row.role === 'tool' || row.kind === 'note') return '> '
  return ''
}

/**
 * The conversation as markdown for `crit plan`: row i is line i + 1, so a
 * comment's line numbers in crit are row indices plus one.
 */
export function chatMarkdownOf(doc: Doc): string {
  return doc.rows.map(row => `${chatLinePrefix(row)}${row.text}`.replace(/\s+$/, '')).join('\n') + '\n'
}

/** Plan threads (lines of the saved markdown) put back on their messages. */
export function chatThreadsFromPlan(planThreads: readonly Thread[], rows: readonly Row[]): Thread[] {
  return planThreads.map(thread => {
    const first = rows[thread.start - 1]
    const last = rows[thread.end - 1]
    if (!last?.path) {
      return { ...thread, kind: 'chat' }
    }
    const start = first?.path === last.path ? (first.line ?? 0) : (last.line ?? 0)
    return { ...thread, kind: 'chat', path: last.path, start, end: last.line ?? 0 }
  })
}

// ── Review state ────────────────────────────────────────────────────────

export type Pending = {
  comments: Thread[]
  replies: { thread: Thread; reply: Reply }[]
  unread: number
}

/** What the person wrote that Claude has not seen, and what Claude wrote that the person has not. */
export function pendingOf(threads: readonly Thread[], sent: readonly string[], seen: readonly string[]): Pending {
  const isSent = new Set(sent)
  const isSeen = new Set(seen)
  const comments: Thread[] = []
  const replies: { thread: Thread; reply: Reply }[] = []
  let unread = 0

  for (const thread of threads) {
    if (thread.fromClaude && !isSeen.has(thread.id)) {
      unread += 1
    }
    for (const reply of thread.replies) {
      if (reply.fromClaude && !isSeen.has(reply.id)) {
        unread += 1
      }
    }
    if (thread.resolved) {
      continue
    }
    if (!thread.fromClaude && !isSent.has(thread.id)) {
      comments.push(thread)
    }
    for (const reply of thread.replies) {
      if (!reply.fromClaude && !isSent.has(reply.id) && reply.id !== '') {
        replies.push({ thread, reply })
      }
    }
  }

  return { comments, replies, unread }
}

/** Every Claude-authored id, to mark as seen. */
export const claudeIdsOf = (threads: readonly Thread[]) =>
  threads.flatMap(thread => [
    ...(thread.fromClaude ? [thread.id] : []),
    ...thread.replies.filter(reply => reply.fromClaude).map(reply => reply.id),
  ])

export function whereOf(thread: Pick<Thread, 'path' | 'start' | 'end'>, index: ChatIndex = []): string {
  const lines =
    thread.end === 0 ? 'as a whole' : thread.start === thread.end ? `line ${thread.start}` : `lines ${Math.max(1, thread.start)}-${thread.end}`
  const chat = /^chat:(\d+)(?:\/(.+))?$/.exec(thread.path)
  if (!chat) {
    return thread.start > 0 ? `${thread.path}:${thread.start}${thread.end !== thread.start ? `-${thread.end}` : ''}` : thread.path
  }
  const role = index.find(entry => entry.index === Number(chat[1]))?.role
  const whose = role === 'user' ? 'my message' : 'your reply'
  return chat[2] ? `tool output in message #${chat[1]}, ${lines}` : `${whose} #${chat[1]}${thread.end === 0 ? ' ' : ', '}${lines}`
}

const quoted = (text: string) =>
  text
    .split('\n')
    .slice(0, 8)
    .map(line => `   > ${line}`)
    .join('\n')

/** The review as one prompt for Claude. */
export function feedbackOf(pending: Pending, index: ChatIndex): string {
  const parts: string[] = []
  let n = 0

  for (const thread of pending.comments) {
    n += 1
    parts.push(
      [`${n}. ${whereOf(thread, index)} [id ${thread.id}]`, thread.quote ? quoted(thread.quote) : '', `   ${thread.body}`]
        .filter(Boolean)
        .join('\n'),
    )
  }

  for (const { thread, reply } of pending.replies) {
    n += 1
    const before = thread.replies.filter(one => one.id !== reply.id && one.at <= reply.at).at(-1)
    parts.push(
      [
        `${n}. Reply in thread [id ${thread.id}] on ${whereOf(thread, index)}`,
        `   Thread: ${thread.body}`,
        before ? `   ${before.fromClaude ? 'You' : 'I'} said: ${before.body}` : '',
        `   My reply: ${reply.body}`,
      ]
        .filter(Boolean)
        .join('\n'),
    )
  }

  const counts = [
    pending.comments.length ? `${pending.comments.length} comment${pending.comments.length === 1 ? '' : 's'}` : '',
    pending.replies.length ? `${pending.replies.length} repl${pending.replies.length === 1 ? 'y' : 'ies'}` : '',
  ]
    .filter(Boolean)
    .join(', ')

  return [
    `${REVIEW_HEAD} ${counts}.`,
    '',
    parts.join('\n\n'),
    '',
    `Reply to each item with the ${TOOL_PREFIX}reply tool: pass its id and a short reply. Make any code change an item asks for before you reply to it. I resolve threads myself. The ids are for the tool only: don't mention them in your answer to me.`,
  ].join('\n')
}

// ── What the pane's Client draws ────────────────────────────────────────

/** Characters of rows sent to the Client at once; its props are capped near 100k. */
export const WINDOW_CHARS = 60000
const ROW_CHARS = 400

/** The row each thread hangs under: the last row of its range, a `+`/context line first. */
export function anchorRowOf(rows: readonly Row[], thread: Thread): number {
  let fallback = -1
  for (let i = 0; i < rows.length; i += 1) {
    const row = rows[i]
    if (!row || row.path !== thread.path) {
      continue
    }
    if (row.line === thread.end || (thread.end === 0 && row.kind === 'title')) {
      if (row.sign !== '-') {
        return i
      }
      fallback = fallback === -1 ? i : fallback
    }
  }
  return fallback
}

/** The rows around `center` that fit the budget, as [from, to). */
export function windowOf(rows: readonly Row[], center: number, budget = WINDOW_CHARS): [number, number] {
  const cost = (i: number) => Math.min(rows[i]?.text.length ?? 0, ROW_CHARS) + 40
  const mid = Math.min(Math.max(center, 0), Math.max(0, rows.length - 1))
  let from = mid
  let to = Math.min(mid + 1, rows.length)
  let spent = rows.length ? cost(mid) : 0
  while (spent < budget && (from > 0 || to < rows.length)) {
    if (to < rows.length) {
      spent += cost(to)
      to += 1
    }
    if (from > 0 && spent < budget) {
      from -= 1
      spent += cost(from)
    }
  }
  return [from, to]
}

/** The props of the review Client: a window of rows, the threads hung in it, and the header. */
export function clientPropsOf(args: {
  doc: Doc
  threads: readonly Thread[]
  ui: UiState
  sent: readonly string[]
  seen: readonly string[]
  status: string
  toSend?: number
  unread?: number
}): ReviewProps & { orphans: number } {
  const { doc, ui } = args
  const sent = new Set(args.sent)
  const seen = new Set(args.seen)
  const [from, to] = windowOf(doc.rows, ui.center)
  const rows: ClientRow[] = doc.rows.slice(from, to).map((row, n) => ({
    i: from + n,
    k: row.kind,
    t: row.text.slice(0, ROW_CHARS),
    c: row.path !== undefined,
    ...(row.no ? { no: row.no } : {}),
    ...(row.sign ? { s: row.sign } : {}),
    ...(row.role ? { r: row.role } : {}),
  }))

  const head = (author: string, fromClaude: boolean, id: string, resolved: boolean) =>
    [
      fromClaude ? 'claude' : author === '' ? 'you' : author,
      !fromClaude && !sent.has(id) && !resolved ? 'draft' : '',
      fromClaude && !seen.has(id) ? 'new' : '',
    ]
      .filter(Boolean)
      .join(' · ')

  let orphans = 0
  let hidden = 0
  const threads: ClientThread[] = []
  for (const thread of args.threads) {
    const row = anchorRowOf(doc.rows, thread)
    if (row === -1) {
      orphans += 1
      continue
    }
    if (row < from || row >= to) {
      continue
    }
    const isNew = thread.replies.some(reply => reply.fromClaude && !seen.has(reply.id))
    if (thread.resolved && ui.resolvedView === 'hidden' && !isNew) {
      hidden += 1
      continue
    }
    if (thread.resolved && ui.resolvedView !== 'full') {
      const lastWord = thread.replies.at(-1)
      threads.push({
        id: thread.id,
        row,
        tone: isNew ? 'claude' : 'you',
        head: `✓ resolved${isNew ? ' · new' : ''} · ${thread.body.split('\n')[0]?.slice(0, 40) ?? ''}${lastWord ? ` → ${lastWord.fromClaude ? 'claude' : 'you'}: ${lastWord.body.split('\n')[0]?.slice(0, 80) ?? ''}` : ''}`,
        body: '',
        replies: [],
        collapsed: true,
      })
      continue
    }
    threads.push({
      id: thread.id,
      row,
      tone: thread.fromClaude ? 'claude' : sent.has(thread.id) || thread.resolved ? 'you' : 'draft',
      head: `${head(thread.author, thread.fromClaude, thread.id, thread.resolved)}${thread.resolved ? ' · resolved ✓' : ''}`,
      body: thread.body,
      replies: thread.replies.map(reply => ({
        tone: reply.fromClaude ? 'claude' : sent.has(reply.id) ? 'you' : 'draft',
        head: head(reply.author, reply.fromClaude, reply.id, thread.resolved),
        body: reply.body,
      })),
    })
  }

  return {
    docKey: `${ui.source}:${ui.docRev}`,
    startAt: ui.startAt,
    source: ui.source,
    title: doc.title,
    status: [args.status, orphans ? `${orphans} off-view` : '', hidden ? `${hidden} resolved hidden (h)` : ''].filter(Boolean).join(' · '),
    toSend: args.toSend ?? 0,
    unread: args.unread ?? 0,
    error: doc.error ?? null,
    total: doc.rows.length,
    rows,
    threads,
    wheel: ui.wheel,
    wheelBy: ui.wheelBy,
    orphans,
  }
}

// ── Selection ───────────────────────────────────────────────────────────

/** The last title row before `from`, or the first row. */
export function sectionBefore(rows: readonly Row[], from: number): number {
  for (let i = Math.min(from, rows.length) - 1; i >= 0; i -= 1) {
    if (rows[i]?.kind === 'title') {
      return i
    }
  }
  return 0
}

/**
 * The anchor a comment on rows `lo`..`hi` takes, or why it can't. With
 * columns (a mouse selection) the quote is the exact text: from `startCol`
 * in the first row to `endCol` in the last.
 */
export function selectionOf(
  doc: Doc,
  lo: number,
  hi: number,
  cols: { startCol: number | null; endCol: number | null } = { startCol: null, endCol: null },
): { path: string; start: number; end: number; quote: string; isExact: boolean } | string {
  const picked = doc.rows
    .map((row, i) => ({ row, i }))
    .slice(Math.min(lo, hi), Math.max(lo, hi) + 1)
    .filter(({ row }) => row.path !== undefined)
  if (picked.length === 0) {
    return 'Move to a line to comment on it.'
  }
  const path = picked[picked.length - 1]?.row.path as string
  const same = picked.filter(({ row }) => row.path === path)
  // A title row (line 0: the whole message or file) only counts when nothing else is picked.
  const numbered = same.map(({ row }) => row.line ?? 0).filter(line => line > 0)
  const lines = numbered.length > 0 ? numbered : [0]
  const isExact = cols.startCol !== null && cols.endCol !== null
  const quote = same
    .filter(({ row }) => row.kind === 'line')
    .map(({ row, i }) => {
      if (isExact) {
        const from = i === Math.min(lo, hi) ? (cols.startCol ?? 0) : 0
        const to = i === Math.max(lo, hi) ? (cols.endCol ?? row.text.length) : row.text.length
        return row.text.slice(from, to)
      }
      return row.sign && row.sign !== ' ' ? `${row.sign}${row.text}` : row.text
    })
    .join('\n')
  return { path, start: Math.min(...lines), end: Math.max(...lines), quote, isExact }
}

const collapse = (text: string) => text.replace(/\s+/g, ' ')

/**
 * Where an exact quote starts in its lines' text, as crit's web UI records
 * `quote_offset`: the lines trimmed and joined by newlines, whitespace
 * collapsed. Undefined when the quote is not found there.
 */
export function quoteOffsetOf(lines: readonly string[], startCol: number, quote: string): number | undefined {
  const first = lines[0] ?? ''
  const lead = first.length - first.trimStart().length
  const full = collapse(lines.map(line => line.trim()).join('\n'))
  const wanted = collapse(quote)
  const guess = collapse(first.trimStart().slice(0, Math.max(0, startCol - lead))).length
  if (full.slice(guess, guess + wanted.length) === wanted) return guess
  const found = full.indexOf(wanted)
  return found === -1 ? undefined : found
}

export function newId(): string {
  const bytes = new Uint8Array(4)
  crypto.getRandomValues(bytes)
  return 'c' + [...bytes].map(b => b.toString(16).padStart(2, '0')).join('')
}
