/** Where a review looks: the conversation, the working-tree diff, or one file. */
export type Source = 'chat' | 'diff' | 'file'

/** One line of a review document. */
export type Row = {
  kind: 'title' | 'hunk' | 'line' | 'note'
  text: string
  /** Comment anchor: a repo path, or `chat:<message index>` for the conversation. */
  path?: string
  line?: number
  sign?: '+' | '-' | ' '
  role?: 'user' | 'assistant' | 'tool'
  /** Gutter label (a line number). */
  no?: string
}

export type Doc = { source: Source; title: string; rows: Row[]; error?: string }

export type Reply = {
  id: string
  author: string
  body: string
  at: string
  fromClaude: boolean
}

export type Thread = {
  id: string
  /** `chat` threads live in this mod; `code` threads live in crit's review.json. */
  kind: 'chat' | 'code'
  path: string
  start: number
  end: number
  quote: string
  body: string
  author: string
  fromClaude: boolean
  at: string
  resolved: boolean
  replies: Reply[]
}

export type UiState = {
  source: Source
  /** Where the Client puts its cursor when a document (re)loads. */
  startAt: number
  /** The row the Client's window of rows is centred on. */
  center: number
  /** Resolved threads: one line each, hidden, or in full. */
  resolvedView: 'line' | 'hidden' | 'full'
  filePath: string | null
  /** Bumped when the shown document changes, so the Client resets its cursor. */
  docRev: number
  /** Bumped per wheel tick over the pane; the Client moves its cursor by wheelBy. */
  wheel: number
  wheelBy: number
}

/** A row as the Client gets it: short keys keep the props small. */
export type ClientRow = {
  i: number
  k: Row['kind']
  t: string
  /** Has a comment anchor. */
  c: boolean
  no?: string
  s?: '+' | '-' | ' '
  r?: 'user' | 'assistant' | 'tool'
}

export type ClientThread = {
  id: string
  row: number
  tone: 'you' | 'claude' | 'draft'
  head: string
  body: string
  replies: { tone: 'you' | 'claude' | 'draft'; head: string; body: string }[]
  /** A resolved thread drawn as one line; h expands it. */
  collapsed?: boolean
}

export type ReviewProps = {
  docKey: string
  startAt: number
  source: Source
  title: string
  status: string
  /** Comments and replies not yet sent to Claude. */
  toSend: number
  /** Claude's comments and replies not yet seen. */
  unread: number
  error: string | null
  total: number
  rows: ClientRow[]
  threads: ClientThread[]
  wheel: number
  wheelBy: number
}

export type ChatIndex = { index: number; role: 'user' | 'assistant'; text: string }[]

declare module 'claude-code' {
  interface PluginState {
    'crit-tui': {
      threads: Thread[]
      critThreads: Thread[]
      sent: string[]
      seen: string[]
      ui: UiState
      docs: { chat: Doc; diff: Doc; file: Doc }
      chatIndex: ChatIndex
      isOpen: boolean
    }
  }
}
