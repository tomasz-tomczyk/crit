import type { SessionMessage } from 'claude-code'
import { describe, expect, test } from 'claude-code/testing'

import {
  EMPTY_UI,
  chatDoc,
  clientPropsOf,
  critThreadsOf,
  feedbackOf,
  parseDiff,
  quoteOffsetOf,
  whereOf,
  pendingOf,
  selectionOf,
} from '../hooks/model'
import type { Thread } from '../types'

const DIFF = [
  'diff --git a/src/a.go b/src/a.go',
  'index 1..2 100644',
  '--- a/src/a.go',
  '+++ b/src/a.go',
  '@@ -10,3 +10,3 @@ func main() {',
  ' keep',
  '-old',
  '+new',
  ' tail',
].join('\n')

const MESSAGES: SessionMessage[] = [
  { role: 'user', text: 'fix the bug', toolUses: [] },
  {
    role: 'assistant',
    text: 'Plan:\n1. read a.go\n2. patch it',
    toolUses: [{ tool_use_id: 't1', tool: 'Bash', input: { command: 'go test ./...' }, text: 'ok  pkg 0.1s' }],
  },
]

const thread = (over: Partial<Thread>): Thread => ({
  id: 'c1',
  kind: 'chat',
  path: 'chat:1',
  start: 2,
  end: 2,
  quote: '1. read a.go',
  body: 'why read first?',
  author: 'you',
  fromClaude: false,
  at: '2026-10-02T10:00:00Z',
  resolved: false,
  replies: [],
  ...over,
})

describe('documents', () => {
  test('diff lines anchor to new-side line numbers', () => {
    const rows = parseDiff(DIFF)
    expect(rows[0]).toEqual({ kind: 'title', text: 'src/a.go', path: 'src/a.go' })
    const lines = rows.filter(row => row.kind === 'line').map(row => [row.sign, row.line])
    expect(lines).toEqual([
      [' ', 10],
      ['-', 11],
      ['+', 11],
      [' ', 12],
    ])
  })

  test('the conversation keeps message text and tool output', () => {
    const { doc, index } = chatDoc(MESSAGES)
    expect(index.map(one => one.index)).toEqual([0, 1])
    expect(doc.rows.some(row => row.text.includes('Bash(go test ./...)'))).toBe(true)
    expect(doc.rows.find(row => row.text === '2. patch it')?.line).toBe(3)
  })

  test('crit review.json becomes code threads', () => {
    const json = JSON.stringify({
      files: {
        'src/a.go': {
          comments: [
            { id: 'x1', start_line: 11, end_line: 11, body: 'nit', author: 'Tomasz', replies: [{ id: 'r1', body: 'done', author: 'Claude' }] },
          ],
        },
      },
    })
    const [one] = critThreadsOf(json)
    expect(one?.path).toBe('src/a.go')
    expect(one?.replies[0]?.fromClaude).toBe(true)
  })
})

describe('review', () => {
  test('drafts and unread replies are counted apart', () => {
    const threads = [
      thread({}),
      thread({ id: 'c2', replies: [{ id: 'r1', author: 'claude', body: 'ok', at: '', fromClaude: true }] }),
    ]
    const pending = pendingOf(threads, ['c2'], [])
    expect(pending.comments.map(one => one.id)).toEqual(['c1'])
    expect(pending.unread).toBe(1)
  })

  test('feedback names each comment by id and asks for the reply tool', () => {
    const { index } = chatDoc(MESSAGES)
    const text = feedbackOf(pendingOf([thread({})], [], []), index)
    expect(text).toContain('your reply #1, line 2 [id c1]')
    expect(text).toContain('> 1. read a.go')
    expect(text).toContain('mcp__crit-tui__reply')
  })

  test('a marked range quotes every line in it', () => {
    const { doc } = chatDoc(MESSAGES)
    const at = doc.rows.findIndex(row => row.text === '1. read a.go')
    const picked = selectionOf(doc, at + 1, at)
    expect(picked).toEqual({ path: 'chat:1', start: 2, end: 3, quote: '1. read a.go\n2. patch it', isExact: false })
  })

  test('a big document goes to the Client as a window around the centre', () => {
    const big = Array.from({ length: 5000 }, (_, n) => ` ${'x'.repeat(60)} ${n}`).join('\n')
    const rows = parseDiff(DIFF + '\n' + big)
    const doc = { source: 'diff' as const, title: 'd', rows }
    const props = clientPropsOf({ doc, threads: [], ui: { ...EMPTY_UI, source: 'diff', center: 3000 }, sent: [], seen: [], status: '' })
    expect(JSON.stringify(props).length).toBeLessThan(100000)
    expect(props.total).toBe(rows.length)
    expect(props.rows.some(row => row.i === 3000)).toBe(true)
    expect(props.rows[0]?.i ?? 0).toBeGreaterThan(0)
  })

  test('threads hang under their row with draft and new marks', () => {
    const { doc } = chatDoc(MESSAGES)
    const props = clientPropsOf({
      doc,
      threads: [thread({ replies: [{ id: 'r1', author: 'claude', body: 'ok', at: '', fromClaude: true }] })],
      ui: EMPTY_UI,
      sent: [],
      seen: [],
      status: '',
    })
    const hung = props.threads[0]
    expect(doc.rows[hung?.row ?? -1]?.text).toBe('1. read a.go')
    expect(hung?.head).toContain('draft')
    expect(hung?.replies[0]?.head).toContain('new')
  })

  test('a comment on a message title is about the whole message', () => {
    expect(whereOf({ path: 'chat:2', start: 0, end: 0 }, [{ index: 2, role: 'assistant', text: 'x' }])).toBe('your reply #2 as a whole')
  })

  test('a resolved thread with a new Claude reply stays visible as one line', () => {
    const { doc } = chatDoc(MESSAGES)
    const props = clientPropsOf({
      doc,
      threads: [thread({ resolved: true, replies: [{ id: 'r1', author: 'claude', body: 'Got it, fixed.', at: '', fromClaude: true }] })],
      ui: EMPTY_UI,
      sent: ['c1'],
      seen: [],
      status: '',
    })
    expect(props.threads[0]?.collapsed).toBe(true)
    expect(props.threads[0]?.head).toContain('✓ resolved · new')
    expect(props.threads[0]?.head).toContain('claude: Got it, fixed.')
  })

  test('the review prompt and the reply tool calls are left out of the chat', () => {
    const { doc } = chatDoc([
      { role: 'user', text: 'Crit review: 2 comments.\n\n1. ...', toolUses: [] },
      {
        role: 'assistant',
        text: 'Done.',
        toolUses: [
          { tool_use_id: 't1', tool: 'ToolSearch', input: { query: 'select:mcp__crit-tui__reply' }, text: '' },
          { tool_use_id: 't2', tool: 'mcp__crit-tui__reply', input: { id: 'c1', body: 'ok' }, text: 'Replied' },
        ],
      },
    ])
    const text = doc.rows.map(row => row.text).join('\n')
    expect(text).toContain('↳ you sent a crit review: 2 comments.')
    expect(text).not.toContain('ToolSearch')
    expect(text).not.toContain('crit-tui')
  })

  test('resolved threads show as one line, hide, or show in full', () => {
    const { doc } = chatDoc(MESSAGES)
    const resolved = [thread({ resolved: true, replies: [{ id: 'r1', author: 'claude', body: 'ok', at: '', fromClaude: true }] })]
    const view = (resolvedView: 'line' | 'hidden' | 'full') =>
      clientPropsOf({ doc, threads: resolved, ui: { ...EMPTY_UI, resolvedView }, sent: ['c1'], seen: ['r1'], status: '' })
    expect(view('line').threads[0]?.collapsed).toBe(true)
    expect(view('hidden').threads).toHaveLength(0)
    expect(view('hidden').status).toContain('1 resolved hidden (h)')
    expect(view('full').threads[0]?.collapsed).toBeUndefined()
  })

  test('quote_offset matches crit: lines trimmed, joined, whitespace collapsed', () => {
    expect(quoteOffsetOf(['  1. read a.go', '2. patch it'], 5, 'read a.go\n2. patch')).toBe(3)
    expect(quoteOffsetOf(['a a a'], 4, 'a')).toBe(4)
    expect(quoteOffsetOf(['abc'], 0, 'zzz')).toBeUndefined()
  })
})
