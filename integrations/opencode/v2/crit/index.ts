import { execFileSync } from "node:child_process"
import { Plugin } from "@opencode/plugin"
import { CritRPC } from "./lib/crit-rpc.js"
import { critWaitTimeout, isCritWaitCommand } from "./lib/crit-wait-notify.js"

const SHARING_BLOCK = `## Sharing

If the user asks for a URL, a shareable link, or a QR code for the review:

\`\`\`bash
crit share <file> [file...]   # Upload and print URL
crit share --qr <file>        # Also print QR code (terminal only)
crit unpublish [file...]      # Remove shared review
\`\`\`

- **Always relay the output** — copy the URL (and QR if used) into your response. Don't make the user dig through tool output.
- **\`--qr\` is terminal-only** — skip in mobile apps, web chat UIs, or anywhere Unicode block characters won't render correctly.
- **Unpublish uses the persisted delete token** in the review file — no extra args needed.
`

let sharingEnabledCache: boolean | undefined

function sharingEnabled(): boolean {
  if (sharingEnabledCache !== undefined) return sharingEnabledCache
  try {
    const text = execFileSync("crit", ["config"], { encoding: "utf8", timeout: 2000 })
    const config = JSON.parse(text) as { share_url?: string }
    sharingEnabledCache = typeof config.share_url === "string" && config.share_url.length > 0
  } catch {
    sharingEnabledCache = false
  }
  return sharingEnabledCache
}

export default Plugin.define({
  id: "crit",
  async setup(ctx) {
    const registration = await ctx.rpc.register(CritRPC, {})
    await ctx.shell.hook("create.before", (event) => {
      if (!isCritWaitCommand(event.command)) return
      event.timeout = critWaitTimeout(event.timeout)
    })
    await ctx.session.hook("context", (event) => {
      if (!sharingEnabled()) return
      event.system.push({ type: "text", text: SHARING_BLOCK })
    })
    await ctx.tool.hook("execute.before", async (event) => {
      if (event.tool !== "shell") return
      const input = event.input as { command?: unknown }
      if (typeof input.command !== "string" || !isCritWaitCommand(input.command)) return
      await registration.events.emit("waitStarted", {})
    })
  },
})
