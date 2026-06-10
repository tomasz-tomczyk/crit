// crit plugin for opencode.
//
// Injects crit's "Sharing" instructions into the model's system prompt only
// when the user has opted into sharing by setting `share_url` in their crit
// config. Without this gate, the sharing block is dead weight when sharing
// is disabled and can trip enterprise information-handling reviews.
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

import type { Plugin } from "@opencode-ai/plugin"

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

export const CritSharingPlugin: Plugin = async ({ $ }) => {
  return {
    "experimental.chat.system.transform": async (_input, output) => {
      if (isTitleGenerator(output.system)) return
      const shareURL = await loadShareURL($)
      if (!shareURL) return
      output.system.push(SHARING_BLOCK)
    },
  }
}
