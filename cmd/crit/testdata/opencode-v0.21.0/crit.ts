// crit plugin for opencode.
//
// 1. Injects crit's "Sharing" instructions into the model's system prompt only
//    when the user has opted into sharing by setting `share_url` in their crit
//    config. Without this gate, the sharing block is dead weight when sharing
//    is disabled and can trip enterprise information-handling reviews.
// 2. Toasts when the agent starts a blocking `crit` wait so Attention-style
//    users notice a review is ready even while the tool call is still running.
//
// Notes captured during implementation:
//   - opencode auto-loads .ts files dropped into `.opencode/plugins/` (project)
//     or `~/.config/opencode/plugins/` (global). No registration in
//     opencode.jsonc is required for local files.
//   - The hook used here is `experimental.chat.system.transform`, which
//     receives a mutable `output.system: string[]`. We append a single entry.
//   - The hook fires for every chat turn including opencode's internal
//     title-generator subagent. We skip injection there so the title model
//     isn't seeded with sharing copy.
//   - Crit config is read by shelling out to `crit config`, which prints a
//     JSON object. We parse `share_url` and bail out if empty.

import { createRequire } from "node:module"
import type { Plugin } from "@opencode-ai/plugin"

const require = createRequire(import.meta.url)
const { isCritWaitCommand, roundReadyToast } = require("./lib/crit-wait-notify.js") as {
  isCritWaitCommand: (command: string) => boolean
  roundReadyToast: (url?: string) => { title: string; message: string }
}

const SHARING_BLOCK = `## Sharing

If the user asks for a URL, a shareable link, or a QR code for the review:

\`\`\`bash
crit share <file> [file...]   # Upload and print URL
crit share --qr <file>        # Also print QR code (terminal only)
crit unpublish [file...]                              # Remove shared review
\`\`\`

- **Always relay the output** — copy the URL (and QR if used) into your response. Don't make the user dig through tool output.
- **\`--qr\` is terminal-only** — skip in mobile apps, web chat UIs, or anywhere Unicode block characters won't render correctly.
- **Unpublish uses the persisted delete token** in the review file — no extra args needed.
`

type CritConfig = { share_url?: string }

let cachedShareURL: string | null | undefined

async function loadShareURL($: any): Promise<string | null> {
  if (cachedShareURL !== undefined) return cachedShareURL
  try {
    const result = await $`crit config`.quiet()
    const text = result.stdout.toString()
    const parsed = JSON.parse(text) as CritConfig
    cachedShareURL = parsed.share_url && parsed.share_url.length > 0 ? parsed.share_url : null
  } catch {
    cachedShareURL = null
  }
  return cachedShareURL
}

function isTitleGenerator(system: string[]): boolean {
  for (const entry of system) {
    const lower = entry.toLowerCase()
    if (lower.includes("title generator") || lower.includes("generate a title")) {
      return true
    }
  }
  return false
}

function bashCommandFromToolInput(input: any, output: any): string {
  const fromOutput = output?.args?.command ?? output?.args?.cmd
  if (typeof fromOutput === "string") return fromOutput
  const fromInput = input?.args?.command ?? input?.args?.cmd ?? input?.command
  if (typeof fromInput === "string") return fromInput
  return ""
}

function showToast(client: any, title: string, message: string): void {
  try {
    const result = client?.tui?.showToast?.({
      body: { title, message, variant: "info" },
    })
    if (result && typeof result.catch === "function") result.catch(() => {})
  } catch {
    // Toast delivery is best-effort.
  }
  try {
    // SDK expects { body: { service, level, message } } — a flat payload
    // rejects with "Expected object, got undefined" and never reaches the log.
    const result = client?.app?.log?.({
      body: {
        service: "crit",
        level: "info",
        message: `[Crit] ${message}`,
      },
    })
    if (result && typeof result.catch === "function") result.catch(() => {})
  } catch {
    // Logging is best-effort.
  }
}

export const CritSharingPlugin: Plugin = async ({ $, client }) => {
  return {
    "experimental.chat.system.transform": async (_input, output) => {
      if (isTitleGenerator(output.system)) return
      const shareURL = await loadShareURL($)
      if (!shareURL) return
      output.system.push(SHARING_BLOCK)
    },
    "tool.execute.before": async (input, output) => {
      const tool = String(input?.tool || "").toLowerCase()
      if (tool !== "bash" && tool !== "shell") return
      const command = bashCommandFromToolInput(input, output)
      if (!isCritWaitCommand(command)) return
      const toast = roundReadyToast()
      showToast(client, toast.title, toast.message)
    },
  }
}
