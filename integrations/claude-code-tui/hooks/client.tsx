import type { ClientKeyEvent, ClientPointerEvent, ClientSurface } from 'claude-code'

import type { ClientRow, ClientThread, ReviewProps } from '../types'

// The review pane's interactive region. It owns the cursor, the selection,
// scrolling and the comment box, and posts every change to the review to
// the hooks module, which owns the data.

type Mode = 'browse' | 'comment' | 'reply'

type State = {
  docKey: string
  cursor: number
  mark: number | null
  /** Columns of a mouse selection, in the mark row and the cursor row; null selects whole rows. */
  markCol: number | null
  cursorCol: number | null
  isMarking: boolean
  top: number
  mode: Mode
  draft: string
  replyTo: string | null
  thread: string | null
  isDragging: boolean
  wheel: number
  want: number | null
  asked: string
  showHelp: boolean
}

type Line =
  | { kind: 'row'; row: ClientRow }
  | { kind: 'thread'; id: string; text: string; tone: string }
  | { kind: 'composer'; text: string; isCursor: boolean }

const HEADER = 2
const FOOTER = 1
const EDGE = 40

const HELP = [
  'click        give the pane the keys (Esc gives them back)',
  'click/drag   move the cursor / select rows',
  'j k ↑ ↓      move          shift+↑ ↓ or v   select rows',
  'space pgup   page          g G home end      top, bottom',
  'n p → ←      next, previous message or file',
  'c enter      comment on the line or selection',
  'l tab        next thread   click a thread    pick it',
  'r            reply to the picked thread, or the one on this line',
  'x            resolve it    h   resolved: one line, hidden, full',
  's            send comments and replies to Claude',
  'a            ask Claude to review the diff and comment',
  '1 2 3        chat, diff, file          f   reload',
  '?            close this help',
]

const TONE: Record<string, string> = { you: 'cyan', claude: 'magenta', draft: 'yellow', meta: 'gray' }

// One review pane, one instance: its state lives here. The engine ignores a
// setState made while drawing, so handlers write here and call setState only
// to ask for the redraw.
const live: { props: ReviewProps | null; surface: ClientSurface<State> | null; lines: Line[]; state: State | null } = {
  props: null,
  surface: null,
  lines: [],
  state: null,
}

function commit(next: State) {
  live.state = next
  live.surface?.setState(next)
}

function wrap(text: string, width: number): string[] {
  const out: string[] = []
  const room = Math.max(8, width)
  for (const para of text.split('\n')) {
    let line = ''
    for (const word of para.split(/\s+/)) {
      if (word === '') continue
      if (line === '') line = word
      else if (line.length + 1 + word.length <= room) line += ' ' + word
      else {
        out.push(line)
        line = word
      }
      while (line.length > room) {
        out.push(line.slice(0, room))
        line = line.slice(room)
      }
    }
    out.push(line)
  }
  return out
}

/** The comment or reply box, drawn where its thread will be. */
function composerLines(state: State, isReply: boolean): Line[] {
  const drafted = state.draft.split('\n')
  const shown = drafted.slice(-COMPOSER_MAX)
  const lines: Line[] = [{ kind: 'composer', text: isReply ? '├ your reply' : '┌ new comment', isCursor: false }]
  if (drafted.length > shown.length) lines.push({ kind: 'composer', text: `│ … ${drafted.length - shown.length} more lines`, isCursor: false })
  shown.forEach((text, n) => lines.push({ kind: 'composer', text: `│ ${text}`, isCursor: n === shown.length - 1 }))
  if (!isReply) lines.push({ kind: 'composer', text: '└', isCursor: false })
  return lines
}

/** Where a row's text starts in the region: the lead, then the gutter or the title rule. */
const textStart = (row: ClientRow) => 1 + (row.k === 'title' ? 3 : 7)

const colOf = (row: ClientRow, x: number) => Math.min(Math.max(x - textStart(row), 0), row.t.length)

/** The selection in reading order: rows lo..hi, and columns when the mouse made it. */
function rangeOf(state: State) {
  const mark = state.mark ?? state.cursor
  const isForward = mark < state.cursor || (mark === state.cursor && (state.markCol ?? 0) <= (state.cursorCol ?? 0))
  const hasCols = state.mark !== null && state.markCol !== null && state.cursorCol !== null
  return isForward
    ? { lo: mark, hi: state.cursor, loCol: hasCols ? state.markCol : null, hiCol: hasCols ? state.cursorCol : null }
    : { lo: state.cursor, hi: mark, loCol: hasCols ? state.cursorCol : null, hiCol: hasCols ? state.markCol : null }
}

/** The part of row `i` the selection covers, as [from, to) columns, or null. */
function selectedIn(state: State, row: ClientRow): [number, number] | null {
  if (state.mark === null) return null
  const { lo, hi, loCol, hiCol } = rangeOf(state)
  if (row.i < lo || row.i > hi) return null
  const from = row.i === lo && loCol !== null ? loCol : 0
  const to = row.i === hi && hiCol !== null ? hiCol : row.t.length
  return [from, Math.max(from, to)]
}

const selectionEnd = (state: State) => Math.max(state.mark ?? state.cursor, state.cursor)

function linesOf(props: ReviewProps, width: number, state: State): Line[] {
  const hung = new Map<number, ClientThread[]>()
  for (const thread of props.threads) {
    hung.set(thread.row, [...(hung.get(thread.row) ?? []), thread])
  }
  const inner = width - 10
  const lines: Line[] = []
  const isReplying = (id: string) => state.mode === 'reply' && state.replyTo === id
  for (const row of props.rows) {
    lines.push({ kind: 'row', row })
    for (const thread of hung.get(row.i) ?? []) {
      if (thread.collapsed) {
        lines.push({ kind: 'thread', id: thread.id, tone: thread.tone === 'claude' ? 'claude' : 'meta', text: `· ${thread.head}` })
        if (isReplying(thread.id)) lines.push(...composerLines(state, true), { kind: 'composer', text: '└', isCursor: false })
        continue
      }
      lines.push({ kind: 'thread', id: thread.id, tone: thread.tone, text: `┌ ${thread.head}` })
      for (const text of wrap(thread.body, inner)) lines.push({ kind: 'thread', id: thread.id, tone: thread.tone === 'claude' ? 'claude' : 'you', text: `│ ${text}` })
      for (const reply of thread.replies) {
        lines.push({ kind: 'thread', id: thread.id, tone: reply.tone, text: `├ ${reply.head}` })
        for (const text of wrap(reply.body, inner)) lines.push({ kind: 'thread', id: thread.id, tone: reply.tone === 'claude' ? 'claude' : 'you', text: `│ ${text}` })
      }
      if (isReplying(thread.id)) lines.push(...composerLines(state, true))
      lines.push({ kind: 'thread', id: thread.id, tone: 'meta', text: '└' })
    }
    if (state.mode === 'comment' && row.i === selectionEnd(state)) lines.push(...composerLines(state, false))
  }
  return lines
}

const first = (props: ReviewProps) => props.rows[0]?.i ?? 0
const last = (props: ReviewProps) => props.rows.at(-1)?.i ?? 0

const COMPOSER_MAX = 6

function bodyRows(surface: ClientSurface<State>) {
  return Math.max(3, surface.rows - HEADER - FOOTER)
}

/** Keeps the cursor row, and the threads under it where they fit, in the window. */
function scrolled(state: State, lines: Line[], height: number): State {
  const focus = state.mode === 'comment' ? selectionEnd(state) : state.cursor
  let at = lines.findIndex(line => line.kind === 'row' && line.row.i === focus)
  if (at === -1) return state
  let end = at
  while (end + 1 < lines.length && lines[end + 1]?.kind === 'thread' && end - at < height - 2) end += 1
  const box = lines.findLastIndex(line => line.kind === 'composer')
  if (box !== -1) {
    end = box
    at = Math.max(Math.min(at, box), box - height + 1)
  }
  let top = Math.min(state.top, Math.max(0, lines.length - height))
  if (at < top) top = at
  else if (end >= top + height) top = end - height + 1
  return top === state.top ? state : { ...state, top: Math.max(0, top) }
}

function post(data: Record<string, string | number | boolean | null>) {
  live.surface?.post(data)
}

/** Moves the cursor, asking the hooks module for more rows near the edges. */
function moved(state: State, to: number, extend: boolean): State {
  const props = live.props
  if (!props) return state
  const target = Math.min(Math.max(to, 0), Math.max(0, props.total - 1))
  const cursor = Math.min(Math.max(target, first(props)), last(props))
  const nearEdge = (cursor - first(props) < EDGE && first(props) > 0) || (last(props) - cursor < EDGE && last(props) < props.total - 1)
  const want = target !== cursor ? target : null
  if (nearEdge || want !== null) {
    post({ type: 'window', center: want ?? cursor })
  }
  return {
    ...state,
    cursor,
    want,
    thread: null,
    mark: extend || state.isMarking ? (state.mark ?? state.cursor) : null,
    markCol: null,
    cursorCol: null,
  }
}

function sectionFrom(props: ReviewProps, from: number, step: 1 | -1): number {
  const rows = step === 1 ? props.rows.filter(row => row.i > from) : props.rows.filter(row => row.i < from).reverse()
  return rows.find(row => row.k === 'title')?.i ?? (step === 1 ? last(props) : first(props))
}

function threadTarget(state: State): string | null {
  const props = live.props
  if (!props) return null
  if (state.thread) return state.thread
  return props.threads.filter(thread => thread.row === state.cursor).at(-1)?.id ?? null
}

function onBrowseKey(state: State, event: ClientKeyEvent, page: number): State {
  const props = live.props
  if (!props) return state
  const { key } = event
  const extend = event.shift === true

  if (key === 'down' || key === 'j' || key === 'J') return moved(state, state.cursor + 1, extend)
  if (key === 'up' || key === 'k' || key === 'K') return moved(state, state.cursor - 1, extend)
  if (key === 'pagedown' || (event.ctrl && key === 'd') || key === ' ' || key === 'space') return moved(state, state.cursor + page, extend)
  if (key === 'pageup' || (event.ctrl && key === 'u')) return moved(state, state.cursor - page, extend)
  if (key === 'home' || key === 'g') return moved(state, 0, extend)
  if (key === 'end' || key === 'G') return moved(state, props.total - 1, extend)
  if (key === 'n' || key === 'right') return moved(state, sectionFrom(props, state.cursor, 1), false)
  if (key === 'p' || key === 'left') return moved(state, sectionFrom(props, state.cursor, -1), false)
  if (key === 'v') return state.isMarking ? { ...state, isMarking: false, mark: null, markCol: null, cursorCol: null } : { ...state, isMarking: true, mark: state.cursor, markCol: null, cursorCol: null }

  if (key === 'l' || key === 'tab') {
    const ids = props.threads
    if (ids.length === 0) return state
    const now = ids.findIndex(thread => thread.id === state.thread)
    const next = ids[(now + 1) % ids.length] ?? ids[0]
    return next ? { ...moved(state, next.row, false), thread: next.id } : state
  }

  if (key === 'c' || key === 'return') {
    const lo = Math.min(state.mark ?? state.cursor, state.cursor)
    const hi = Math.max(state.mark ?? state.cursor, state.cursor)
    const can = props.rows.some(row => row.i >= lo && row.i <= hi && row.c)
    return can ? { ...state, mode: 'comment', draft: '' } : state
  }
  if (key === 'r') {
    const id = threadTarget(state)
    return id ? { ...state, mode: 'reply', draft: '', replyTo: id } : state
  }
  if (key === 'x') {
    const id = threadTarget(state)
    if (id) post({ type: 'resolve', id })
    return state
  }
  if (key === '?') return { ...state, showHelp: !state.showHelp }
  if (key === 's') post({ type: 'send' })
  if (key === 'a') post({ type: 'ask' })
  if (key === 'h') post({ type: 'toggleResolved' })
  if (key === 'f') post({ type: 'refresh' })
  if (key === '1' || key === '2' || key === '3') post({ type: 'source', source: ['chat', 'diff', 'file'][Number(key) - 1] ?? 'chat' })
  return state
}

// ctrl+c never reaches a Client (Claude Code takes it), nor does Escape, so
// an empty Enter, a Backspace on an empty box, or the cancel button cancels.
const cancelled = (state: State): State => ({ ...state, mode: 'browse', draft: '', replyTo: null })

const ERASE = new Set(['backspace', 'delete', 'Backspace', 'Delete', '\x7f', '\b'])

function onComposeKey(state: State, event: ClientKeyEvent): State {
  const { key } = event
  const isErase = ERASE.has(key) || (event.ctrl === true && key === 'h')
  if (key === 'return' && !event.shift) {
    const body = state.draft.trim()
    if (body !== '') {
      if (state.mode === 'reply' && state.replyTo) {
        post({ type: 'reply', id: state.replyTo, body })
      } else {
        const { lo, hi, loCol, hiCol } = rangeOf(state)
        post({ type: 'comment', start: lo, end: hi, startCol: loCol, endCol: hiCol, body })
      }
    }
    return body === '' ? { ...state, mode: 'browse', draft: '', replyTo: null } : { ...state, mode: 'browse', draft: '', replyTo: null, mark: null, markCol: null, cursorCol: null, isMarking: false }
  }
  if (key === 'return') return { ...state, draft: state.draft + '\n' }
  if (event.ctrl && key === 'u') return { ...state, draft: '' }
  if (isErase && state.draft === '') return cancelled(state)
  if (isErase) return { ...state, draft: [...state.draft].slice(0, -1).join('') }
  if (key === 'space') return { ...state, draft: state.draft + ' ' }
  if ([...key].length === 1 && key >= ' ' && !event.ctrl && !event.meta) return { ...state, draft: state.draft + key }
  return state
}

/** Tells the hooks module Claude's new words were on screen when the person acted. */
function noteSeen() {
  if ((live.props?.unread ?? 0) > 0) post({ type: 'seen' })
}

function onKey(event: ClientKeyEvent) {
  noteSeen()
  const surface = live.surface
  const state = live.state
  if (!surface || !state) return
  const page = Math.max(1, Math.floor(bodyRows(surface) / 2))
  const next = state.mode === 'browse' ? onBrowseKey(state, event, page) : onComposeKey(state, event)
  if (next !== state) commit(next)
}

function onPointer(event: ClientPointerEvent) {
  const surface = live.surface
  const state = live.state
  if (!surface || !state || state.mode !== 'browse') return

  const height = bodyRows(surface)
  let top = state.top
  if (event.type === 'move' && state.isDragging) {
    if (event.y < HEADER && top > 0) top -= 1
    if (event.y >= HEADER + height && top + height < live.lines.length) top += 1
  }
  const y = Math.min(Math.max(event.y - HEADER, 0), height - 1)
  const line = live.lines[top + y]

  if (event.type === 'down' && event.button === 'left') {
    noteSeen()
    if (event.y < HEADER || event.y >= HEADER + height || !line) return
    if (line.kind === 'thread') {
      const thread = live.props?.threads.find(one => one.id === line.id)
      commit({ ...state, thread: line.id, cursor: thread?.row ?? state.cursor, mark: null, markCol: null, cursorCol: null, isMarking: false })
      return
    }
    if (line.kind !== 'row') return
    const col = colOf(line.row, event.x)
    commit(
      event.shift
        ? { ...state, cursor: line.row.i, cursorCol: col, mark: state.mark ?? state.cursor, markCol: state.markCol ?? (state.mark === null ? 0 : null), isDragging: true, thread: null }
        : { ...state, cursor: line.row.i, cursorCol: col, mark: line.row.i, markCol: col, isMarking: false, isDragging: true, thread: null },
    )
    return
  }

  if (event.type === 'move' && state.isDragging && line?.kind === 'row') {
    const col = colOf(line.row, event.x)
    if (line.row.i !== state.cursor || col !== state.cursorCol || top !== state.top) commit({ ...state, cursor: line.row.i, cursorCol: col, top })
    return
  }

  if (event.type === 'up' && state.isDragging) {
    // A click without a drag only moves the cursor.
    const isClick = state.mark === state.cursor && state.markCol === state.cursorCol
    commit(isClick ? { ...state, isDragging: false, mark: null, markCol: null, cursorCol: null } : { ...state, isDragging: false })
  }
}

export default function Review(props: ReviewProps, surface: ClientSurface<State>) {
  live.props = props
  live.surface = surface
  const { Box, Text, Button } = surface.elements

  surface.onKey(onKey)
  surface.onPointer(onPointer)
  let state = live.state
  if (!state) {
    state = {
      docKey: props.docKey,
      cursor: props.startAt,
      mark: null,
      markCol: null,
      cursorCol: null,
      isMarking: false,
      top: 0,
      mode: 'browse',
      draft: '',
      replyTo: null,
      thread: null,
      isDragging: false,
      wheel: props.wheel,
      want: null,
      asked: '',
      showHelp: false,
    }
  }

  let next = state
  if (props.docKey !== next.docKey) {
    next = { ...next, docKey: props.docKey, cursor: props.startAt, mark: null, markCol: null, cursorCol: null, isMarking: false, top: 0, thread: null, want: null, mode: 'browse' }
  }
  if (next.want !== null && props.rows.some(row => row.i === next.want)) {
    next = { ...next, cursor: next.want as number, want: null }
  }
  if (props.wheel !== next.wheel) {
    next = { ...moved(next, next.cursor + props.wheelBy * 3, false), wheel: props.wheel }
  }
  if (props.rows.length > 0 && (next.cursor < first(props) || next.cursor > last(props))) {
    next = { ...next, cursor: Math.min(Math.max(next.cursor, first(props)), last(props)) }
  }

  const width = Math.max(30, surface.columns || 80)
  const height = bodyRows(surface)
  const lines = linesOf(props, width, next)
  live.lines = lines
  next = scrolled(next, lines, height)
  live.state = next
  const view = next

  const lo = Math.min(view.mark ?? view.cursor, view.cursor)
  const hi = Math.max(view.mark ?? view.cursor, view.cursor)
  const shown = lines.slice(view.top, view.top + height)

  const chars = props.rows.reduce((sum, row) => {
    const part = selectedIn(view, row)
    return sum + (part ? part[1] - part[0] : 0)
  }, 0)
  const where =
    view.mark === null ? `${view.cursor + 1}/${props.total}` : view.markCol !== null ? `${chars} characters selected` : `${hi - lo + 1} rows selected`
  const counts = [props.unread ? `${props.unread} new from Claude` : '', props.status].filter(Boolean).join(' · ')

  return (
    <Box flexDirection="column" width={width}>
      <Box flexDirection="row" gap={2}>
        {(['chat', 'diff', 'file'] as const).map((source, n) => (
          <Button
            key={`tab-${source}`}
            plain
            label={`${n + 1} ${source}`}
            dimColor={source !== props.source}
            onPress={() => post({ type: 'source', source })}
          />
        ))}
        <Box flexGrow={1}>
          <Text dimColor wrap="truncate-end">
            {props.title}
            {counts ? ` · ${counts}` : ''}
          </Text>
        </Box>
        {props.toSend > 0 ? (
          <Button key="send" variant="primary" label={`send ${props.toSend} to Claude (s)`} onPress={() => post({ type: 'send' })} />
        ) : null}
      </Box>
      <Text dimColor>{'─'.repeat(width)}</Text>
      {props.error && props.rows.length === 0 ? <Text dimColor>{props.error}</Text> : null}
      {view.showHelp ? HELP.map(line => <Text>{line}</Text>) : null}
      {(view.showHelp ? [] : shown).map(line => {
        if (line.kind === 'composer') {
          return (
            <Box flexDirection="row">
              <Text color="yellow" wrap="truncate-start">
                {'        '}
                {line.text}
                {line.isCursor ? '█' : ''}
              </Text>
              {line.isCursor ? <Text> </Text> : null}
              {line.isCursor ? <Button key="cancel" label="cancel" dimColor onPress={() => live.state && commit(cancelled(live.state))} /> : null}
            </Box>
          )
        }
        if (line.kind === 'thread') {
          const isPicked = line.id === view.thread
          return (
            <Text color={TONE[line.tone]} dimColor={line.tone === 'meta'} inverse={isPicked && line.tone !== 'meta'} wrap="truncate-end">
              {'        '}
              {line.text}
            </Text>
          )
        }
        const { row } = line
        const isCursor = row.i === view.cursor
        const picked = selectedIn(view, row)
        const isWholeRow = picked !== null && view.markCol === null
        const hasThread = props.threads.some(thread => thread.row === row.i)
        const lead = isCursor ? '▶' : hasThread ? '●' : ' '
        const gutter = row.k === 'title' ? '' : `${(row.no ?? '').padStart(4)} ${row.s ?? ' '} `
        const color =
          row.s === '+' ? 'green' : row.s === '-' ? 'red' : row.k === 'hunk' ? 'cyan' : row.r === 'user' ? 'blue' : row.r === 'assistant' && row.k === 'title' ? 'magenta' : undefined
        const prefix = row.k === 'title' ? '── ' : gutter
        const style = { bold: row.k === 'title', color, dimColor: row.r === 'tool' && row.k !== 'hunk' && !isCursor }
        if (picked && !isWholeRow) {
          // Draw before | selected | after, cut to the width by hand so the
          // pieces line up with what the mouse pressed.
          const room = width - 1
          const full = `${prefix}${row.t}`.slice(0, room)
          const from = Math.min(prefix.length + picked[0], full.length)
          const to = Math.min(prefix.length + picked[1], full.length)
          return (
            <Box flexDirection="row">
              <Text color={hasThread ? 'magenta' : undefined}>{lead}</Text>
              <Text {...style}>{full.slice(0, from)}</Text>
              <Text {...style} inverse>{full.slice(from, to)}</Text>
              <Text {...style}>{full.slice(to) || ' '}</Text>
            </Box>
          )
        }
        return (
          <Box flexDirection="row">
            <Text color={hasThread ? 'magenta' : undefined}>{lead}</Text>
            <Text {...style} inverse={(isCursor && view.mark === null) || isWholeRow} wrap="truncate-end">
              {row.k === 'title' ? `── ${row.t} ` : `${gutter}${row.t || ' '}`}
            </Text>
          </Box>
        )
      })}
      <Text dimColor wrap="truncate-end">
        {view.mode !== 'browse'
          ? 'enter save · shift+enter new line · empty enter or backspace cancels'
          : view.mark !== null
            ? `${where} · c comment · v or click clears`
            : `c comment · r reply · x resolve · s send · ? keys · ${where}`}
      </Text>
    </Box>
  )
}
