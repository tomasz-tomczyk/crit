import type { On, RenderPropsOf, SessionMessage } from 'claude-code'
import { describe, expect, mock, test } from 'claude-code/testing'

const PANE_PROPS: RenderPropsOf['Pane'] = {
  title: 'crit',
  isFocused: true,
  bodyColumns: 100,
  placement: 'dock',
  scroll: { offset: 0, bodyRows: 24 },
  view: {},
}

const MOUNT = {
  plugin: 'crit-tui',
  surface: 'terminal',
  component: 'Pane',
  requestId: 'crit',
  props: PANE_PROPS,
  viewport: { columns: 160, rows: 40 },
} as const

const MESSAGES: SessionMessage[] = [
  { role: 'user', text: 'fix the bug', toolUses: [] },
  { role: 'assistant', text: 'Plan:\n1. read a.go\n2. patch it', toolUses: [] },
]

const DIFF = 'diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,1 @@\n-old\n+new\n'

function textOf(tree: unknown): string {
  if (typeof tree === 'string' || typeof tree === 'number') return String(tree)
  if (Array.isArray(tree)) return tree.map(textOf).join('')
  if (typeof tree !== 'object' || !tree) return ''
  const props = Reflect.get(tree, 'props') as Record<string, unknown> | undefined
  const label = typeof props?.label === 'string' ? props.label : ''
  const kids = Reflect.get(tree, 'children') ?? props?.children ?? []
  return `${label}${textOf(kids)}${Reflect.get(tree, 'type') === 'Box' ? '\n' : ''}`
}

function world(on: On) {
  const submitted: string[] = []
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('command.register', ($, e) => ({ value: { command: e.name } }))
  on('tool.register', ($, e) => ({ value: { tool: `mcp__crit-tui__${e.name}` } }))
  on('process.run', ($, e) => {
    if (e.argv[0] === 'crit') return { deny: 'crit is not installed' }
    const out = (stdout: string) => ({
      value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false },
    })
    if (e.argv.includes('rev-parse')) return out('true\n')
    if (e.argv.includes('ls-files')) return out('')
    if (e.argv.includes('diff')) return out(DIFF)
    return out('')
  })
  on('session.messages', () => ({ value: MESSAGES }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('ui.status', () => ({ value: undefined }))
  on('ui.toast', () => ({ value: undefined }))
  on('prompt.submit', ($, e) => {
    submitted.push(e.text)
    return { text: e.text }
  })
  return { submitted }
}

const SESSION = { surface: 'terminal', isInteractive: true, cwd: '/work' } as const
const ct = (args: string) =>
  ({ command: 'crit-tui', args, origin: { kind: 'composer' }, presentation: { isFullscreen: true, columns: 160 } }) as const

function critWorld(on: On) {
  const sent: string[] = []
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('command.register', ($, e) => ({ value: { command: e.name } }))
  on('tool.register', ($, e) => ({ value: { tool: `mcp__crit-tui__${e.name}` } }))
  on('process.run', ($, e) => {
    const out = (stdout: string) => ({
      value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false },
    })
    if (e.argv[0] === 'crit' && e.argv[1] === 'status') return out(JSON.stringify({ review_file: '/reviews/r.json' }))
    if (e.argv[0] === 'crit' && e.argv[1] === 'comment') {
      sent.push(e.init?.stdin ?? '')
      return out('Added 1 comment')
    }
    if (e.argv[0] === 'crit') return out('')
    if (e.argv.includes('rev-parse')) return out('true\n')
    if (e.argv.includes('ls-files')) return out('')
    if (e.argv.includes('diff')) return out(DIFF)
    return out('')
  })
  on('fs.read', () => ({ value: '{"files":{}}' }))
  on('session.messages', () => ({ value: MESSAGES }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('ui.status', () => ({ value: undefined }))
  on('ui.toast', () => ({ value: undefined }))
  return { sent }
}

/** A crit with `plan --no-wait`: chat comments go to the session's plan. */
function planWorld(on: On) {
  const saved: string[] = []
  const comments: { argv: readonly string[]; stdin: string }[] = []
  let planReview = ''
  on('session.start', ($, e) => ({ cwd: e.cwd }))
  on('session.id', () => ({ value: 'ABCDEF12-3456' }))
  mock.env(on, { HOME: '/home/t' })
  on('command.register', ($, e) => ({ value: { command: e.name } }))
  on('tool.register', ($, e) => ({ value: { tool: `mcp__crit-tui__${e.name}` } }))
  on('process.run', ($, e) => {
    const out = (stdout: string) => ({
      value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false },
    })
    const [bin, verb] = e.argv
    if (bin === 'crit' && verb === 'status') return out(JSON.stringify({ review_file: '/reviews/r.json' }))
    if (bin === 'crit' && verb === 'plan' && e.argv.includes('--help')) return out('  --no-wait   Save the version and exit')
    if (bin === 'crit' && verb === 'plan') {
      saved.push(e.init?.stdin ?? '')
      return out('claude-chat-abcdef12')
    }
    if (bin === 'crit' && verb === 'comment') {
      comments.push({ argv: e.argv, stdin: e.init?.stdin ?? '' })
      const entry = JSON.parse(e.init?.stdin ?? '[{}]')[0] as { line: number | string; body: string; quote?: string }
      const [from, to = from] = String(entry.line).split('-').map(Number)
      planReview = JSON.stringify({
        files: {
          'claude-chat-abcdef12.md': {
            comments: [{ id: 'c_plan1', start_line: from, end_line: to, body: entry.body, quote: entry.quote, author: 'Tomasz' }],
          },
        },
      })
      return out('Added 1 comment')
    }
    return out('')
  })
  on('fs.read', ($, e) => {
    if (e.path === '/home/t/.crit/plans/claude-chat-abcdef12/.crit/review.json') {
      return planReview ? { value: planReview } : { deny: 'ENOENT' }
    }
    return { value: '{"files":{}}' }
  })
  on('session.messages', () => ({ value: MESSAGES }))
  on('ui.open', () => ({ value: { isPlaced: true } }))
  on('ui.status', () => ({ value: undefined }))
  on('ui.toast', () => ({ value: undefined }))
  return { saved, comments }
}

describe('chat comments in a crit plan', () => {
  test('a chat comment saves the conversation as a plan and comments on its lines', async ($, on) => {
    const { saved, comments } = planWorld(on)
    await $.session.start(SESSION)
    await $.command.run(ct('chat'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    // Rows: 0 you title, 1 "fix the bug", 2 claude title, 3 Plan:, 4 "1. read a.go", 5 "2. patch it".
    expect(saved).toHaveLength(0)
    await ui.pointer({ type: 'down', x: 8 + 3, y: 2 + 4, button: 'left', in: 'review' })
    await ui.pointer({ type: 'move', x: 8 + 7, y: 2 + 4, button: 'left', in: 'review' })
    await ui.pointer({ type: 'up', x: 8 + 7, y: 2 + 4, button: 'left', in: 'review' })
    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'w', in: 'review' })
    await ui.key({ key: 'return', in: 'review' })

    expect(saved[0]).toBe('### you · #0\nfix the bug\n### claude · #1\nPlan:\n1. read a.go\n2. patch it\n')
    expect(comments[0]?.argv).toEqual(['crit', 'comment', '--plan', 'claude-chat-abcdef12', '--json'])
    expect(JSON.parse(comments[0]?.stdin ?? '[]')).toEqual([
      { file: 'claude-chat-abcdef12.md', line: 5, body: 'w', quote: 'read', quote_offset: 3 },
    ])

    // The thread comes back from the plan's review file, on its message.
    const drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toMatch(/1\. read a\.go\s*\n\s+┌ Tomasz · draft\s+│ w\s+└/)
  })
})

describe('crit-tui pane', () => {
  test('a partial selection in the diff goes to crit with quote and quote_offset', async ($, on) => {
    const { sent } = critWorld(on)
    await $.session.start(SESSION)
    await $.command.run(ct('diff'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    // Rows: 0 a.go, 1 hunk, 2 "-old", 3 "+new" (line 1); select "ew".
    await ui.pointer({ type: 'down', x: 8 + 1, y: 2 + 3, button: 'left', in: 'review' })
    await ui.pointer({ type: 'move', x: 8 + 3, y: 2 + 3, button: 'left', in: 'review' })
    await ui.pointer({ type: 'up', x: 8 + 3, y: 2 + 3, button: 'left', in: 'review' })
    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'q', in: 'review' })
    await ui.key({ key: 'return', in: 'review' })

    expect(JSON.parse(sent[0] ?? '[]')).toEqual([{ file: 'a.go', line: 1, body: 'q', quote: 'ew', quote_offset: 1 }])
  })

  test('a whole-line comment goes to crit without a quote', async ($, on) => {
    const { sent } = critWorld(on)
    await $.session.start(SESSION)
    await $.command.run(ct('diff'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })
    await ui.key({ key: 'end', in: 'review' })
    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'q', in: 'review' })
    await ui.key({ key: 'return', in: 'review' })
    expect(JSON.parse(sent[0] ?? '[]')).toEqual([{ file: 'a.go', line: 1, body: 'q' }])

    // A selection that starts on the file's title row still sends real line numbers.
    await ui.key({ key: 'home', in: 'review' })
    await ui.key({ key: 'end', shift: true, in: 'review' })
    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'q', in: 'review' })
    await ui.key({ key: 'return', in: 'review' })
    expect(JSON.parse(sent[1] ?? '[]')).toEqual([{ file: 'a.go', line: 1, body: 'q' }])
  })

  test('opens on the last message with the cursor on it', async ($, on) => {
    world(on)
    await $.session.start(SESSION)
    await $.command.run(ct(''))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })
    const drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toContain('▶── claude · #1')
    expect(drawn).toContain('2. patch it')
  })

  test('keys write a comment, s sends it, and Claude replies in the thread', async ($, on) => {
    const { submitted } = world(on)
    await $.session.start(SESSION)
    await $.command.run(ct('chat'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    await ui.key({ key: 'down', in: 'review' })
    await ui.key({ key: 'c', in: 'review' })
    for (const key of [...'why first']) {
      await ui.key({ key: key === ' ' ? 'space' : key, in: 'review' })
    }
    expect(textOf(await ui.drawn({ in: 'review' }))).toMatch(/Plan:\n\s+┌ new comment\n\s+│ why first█/)
    await ui.key({ key: 'return', in: 'review' })

    let drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toContain('why first')
    expect(drawn).toContain('draft')

    expect(drawn).toContain('send 1 to Claude (s)')
    await ui.press({ key: 'send' })
    expect(submitted).toHaveLength(1)
    expect(submitted[0]).toContain('your reply #1, line 1')
    expect(textOf(await ui.drawn({ in: 'review' }))).not.toContain('send 1 to Claude')
    const id = /\[id (c[0-9a-f]+)\]/.exec(submitted[0] ?? '')?.[1] ?? ''

    await $.tool.call({ tool: 'mcp__crit-tui__reply', id, body: 'To see the call sites.' } as never)
    drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toContain('To see the call sites.')
    expect(drawn).not.toContain('draft')
  })

  test('a mouse drag selects exact text across lines and the comment quotes just that', async ($, on) => {
    const { submitted } = world(on)
    await $.session.start(SESSION)
    await $.command.run(ct('chat'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    // Rows: 0 you title, 1 "fix the bug", 2 claude title, 3 Plan:, 4 "1. read a.go", 5 "2. patch it".
    // The header takes 2 rows; a line's text starts at column 8.
    await ui.pointer({ type: 'down', x: 8 + 3, y: 2 + 4, button: 'left', in: 'review' })
    await ui.pointer({ type: 'move', x: 8 + 8, y: 2 + 5, button: 'left', in: 'review' })
    await ui.pointer({ type: 'up', x: 8 + 8, y: 2 + 5, button: 'left', in: 'review' })
    expect(textOf(await ui.drawn({ in: 'review' }))).toContain('17 characters selected')

    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'x', in: 'review' })
    await ui.key({ key: 'return', in: 'review' })
    await ui.key({ key: 's', in: 'review' })
    expect(submitted[0]).toContain('lines 2-3')
    expect(submitted[0]).toContain('> read a.go\n   > 2. patch\n')
  })

  test('a drag inside one line quotes part of it; a click alone selects nothing', async ($, on) => {
    const { submitted } = world(on)
    await $.session.start(SESSION)
    await $.command.run(ct('chat'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    await ui.pointer({ type: 'down', x: 8 + 3, y: 2 + 5, button: 'left', in: 'review' })
    await ui.pointer({ type: 'up', x: 8 + 3, y: 2 + 5, button: 'left', in: 'review' })
    expect(textOf(await ui.drawn({ in: 'review' }))).not.toContain('selected')

    await ui.pointer({ type: 'down', x: 8 + 3, y: 2 + 5, button: 'left', in: 'review' })
    await ui.pointer({ type: 'move', x: 8 + 8, y: 2 + 5, button: 'left', in: 'review' })
    await ui.pointer({ type: 'up', x: 8 + 8, y: 2 + 5, button: 'left', in: 'review' })
    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'y', in: 'review' })
    await ui.key({ key: 'return', in: 'review' })
    await ui.key({ key: 's', in: 'review' })
    expect(submitted[0]).toContain('line 3')
    expect(submitted[0]).toContain('> patch\n')
  })

  test('shift+down still selects whole rows', async ($, on) => {
    world(on)
    await $.session.start(SESSION)
    await $.command.run(ct('chat'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })
    await ui.key({ key: 'down', shift: true, in: 'review' })
    expect(textOf(await ui.drawn({ in: 'review' }))).toContain('2 rows selected')
  })

  test('backspace on an empty box and the cancel button both cancel', async ($, on) => {
    world(on)
    await $.session.start(SESSION)
    await $.command.run(ct('chat'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'a', in: 'review' })
    await ui.key({ key: 'backspace', in: 'review' })
    expect(textOf(await ui.drawn({ in: 'review' }))).toContain('┌ new comment')
    await ui.key({ key: 'backspace', in: 'review' })
    expect(textOf(await ui.drawn({ in: 'review' }))).not.toContain('┌ new comment')

    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'a', in: 'review' })
    await ui.press({ key: 'cancel' })
    const drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).not.toContain('┌ new comment')
    expect(drawn).not.toContain('draft')
  })

  test('shift+enter makes a real second line, delete on empty cancels, ids stay hidden', async ($, on) => {
    world(on)
    await $.session.start(SESSION)
    await $.command.run(ct('chat'))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'a', in: 'review' })
    await ui.key({ key: 'return', shift: true, in: 'review' })
    await ui.key({ key: 'b', in: 'review' })
    let drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toMatch(/│ a\n\s+│ b█/)
    expect(drawn).not.toContain('⏎')
    await ui.key({ key: 'return', in: 'review' })

    drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toContain('you · draft')
    expect(drawn).not.toMatch(/#c[0-9a-f]{6}/)

    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'delete', in: 'review' })
    expect(textOf(await ui.drawn({ in: 'review' }))).not.toContain('┌ new comment')
  })

  test('2 switches to the diff; without crit the comment stays in the pane', async ($, on) => {
    world(on)
    await $.session.start(SESSION)
    await $.command.run(ct(''))
    const ui = await $.ui.mount(MOUNT)
    await ui.resize({ columns: 100, rows: 24, in: 'review' })

    await ui.key({ key: '2', in: 'review' })
    let drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toContain('── a.go')
    expect(drawn).toContain('1 file changed')

    await ui.key({ key: 'G', shift: true, in: 'review' })
    await ui.key({ key: 'c', in: 'review' })
    await ui.key({ key: 'z', in: 'review' })
    await ui.key({ key: 'return', in: 'review' })
    drawn = textOf(await ui.drawn({ in: 'review' }))
    expect(drawn).toContain('you · draft')
    expect(drawn).toContain('no crit review file')
  })
})
