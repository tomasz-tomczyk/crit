# Live mode

Live mode (`crit live <url>` or `crit <url>`) proxies a running local app into
Crit’s review UI so you can pin comments on the live DOM.

Crit does **not** magically share your normal browser tab with the app. It
loads the app inside an iframe on Crit’s proxy port, injects a small agent
bundle into HTML responses, and talks to that agent over `postMessage`. When
injection or the agent handshake fails, Browse/Navigate still works, but
Comment/Pin stays unavailable (navbar chip + Flash: **Commenting unavailable —
Crit could not connect to this page**, with a link to this guide).

This guide covers how that works, what Crit already rewrites for you, common
failure modes, and framework-specific setup for local apps.

## How injection works

1. Crit starts an API server (review chrome) and a **proxy** port.
2. The chrome iframe loads `http://<host>:<proxy>/…`, which reverse-proxies
   your upstream origin.
3. For `text/html` responses, the proxy:
   - strips `Content-Security-Policy` and `X-Frame-Options` **response headers**
   - rewrites `Set-Cookie` so cookies work on the proxy host
   - injects a service-worker shim + route announcer into `<head>`
   - injects the agent script bundle just before the last `</body>`
4. The agent loads scripts from Crit’s API origin (e.g.
   `http://localhost:3195/crit-agent.js`), then posts `agent-ready` to the
   parent chrome.
5. Only after `agent-ready` does Crit enable Comment/Pin. Live mode launches
   in Comment mode by default; until the agent is ready the Comment button
   stays disabled.

If step 3 or 4 fails, you get the connection-unavailable Flash (Browse still
works). Fix the upstream issue and reload Crit — see the checklist below.

## What Crit already handles

You usually do **not** need to change these yourself — the proxy does them:

| Concern | Behavior |
| ------- | -------- |
| `Content-Security-Policy` header | Stripped on proxied HTML |
| `X-Frame-Options` | Stripped on proxied HTML |
| Vite HMR WebSockets | Proxied with the reverse proxy |
| Service workers | Register is blocked; existing SWs for the loopback origin are unregistered (avoids a prior direct visit “stealing” requests) |
| Upstream `Set-Cookie` | `Domain` stripped; `Secure` stripped on loopback; `SameSite=None` downgraded to `Lax` |

Startup may print notes when it detects Phoenix, Vite, or Next.js, or when
upstream had `frame-ancestors` CSP (still stripped by the proxy).

## Common problems

### Commenting unavailable / agent never connects

Typical causes:

1. **Upstream not reachable from the proxy** — connection refused, wrong host
   (`localhost` vs `127.0.0.1` / IPv6), or the app died. The iframe may show a
   502 (`upstream unreachable`).
2. **Non-HTML response** — API JSON, empty body, or a redirect chain that never
   returns HTML with a `</body>`. Crit needs a real HTML document to inject
   into.
3. **Missing `</body>`** — some minimal or streaming responses never emit a
   body close tag; injection is skipped (`X-Crit-Agent-Injection: failed`).
4. **Oversized HTML** — responses larger than the proxy buffer are passed
   through without injection (`skipped-oversized`).
5. **Meta-tag CSP** — Crit strips the **header**, not `<meta
   http-equiv="Content-Security-Policy">` in the document. A strict meta CSP
   can still block the injected scripts.
6. **`localhost` vs `127.0.0.1` mismatch in the Crit tab** — open Crit via
   `http://localhost:<port>/live` (not `http://127.0.0.1:…`). The proxy injects
   agent `<script src="http://localhost:…">` tags; if the chrome tab is on
   `127.0.0.1`, `postMessage` target origins do not match and `agent-ready` is
   dropped. Point `--live` / `crit live` at whatever host your app actually
   binds (often `127.0.0.1`), but keep the Crit UI on `localhost`.

**Quick checks**

- Direct URL works in a normal tab?
- Crit stderr smoke notes / warnings?
- DevTools → iframe document → Network: do `agent-protocol.js` / `crit-agent.js`
  load from the Crit API origin?
- Console errors about CSP / blocked script?

### App loads but shows login / wrong session

The iframe origin is Crit’s proxy, not your app’s host, so **host-scoped
session cookies are not shared** with a tab where you already logged in.

Forward cookies with `--cookie`, `--cookie-file`, or `--cdp-url` (see the
[README live mode section](../README.md#live-mode)).

### Hydration / LiveView / HMR oddities

- Prefer reviewing the same URL path you use day-to-day (not a bare API
  prefix).
- After changing cookie forwarding, hard-refresh the Crit live tab.
- If the app depends on a service worker for assets, remember Crit disables
  SW registration inside the proxied iframe by design.

## Framework setups (local / dev)

Recipes below are for **local development** only. Do not weaken CSP or framing
controls in production to satisfy Crit.

### Phoenix (LiveView)

Crit detects LiveView markers (`phx-hook`, `phx-main`, `phx-track-static`) and
reminds you that the dev endpoint must allow iframing.

**Usually enough:** run Crit against the endpoint URL; the proxy strips CSP /
`X-Frame-Options` headers.

**If the agent still cannot connect**, check for a **meta CSP** or a custom
plug that re-adds framing restrictions after the proxy’s view of the world.
For local-only debugging you can relax secure headers in `endpoint.ex` (dev):

```elixir
# config/dev.exs — example: do not ship this to prod
config :my_app, MyAppWeb.Endpoint,
  # existing config...
  # Prefer fixing meta CSP / plugs over disabling all security plugs.
```

And in the browser pipeline / `put_secure_browser_headers`, ensure you are not
setting a document meta CSP that blocks scripts from Crit’s API origin.

LiveView WebSockets go through the same reverse proxy as HTTP. If the socket
URL is hard-coded to an absolute `ws://127.0.0.1:4000/…` that bypasses the
proxy, prefer relative `/live/websocket` (Phoenix default) so it stays on the
proxy host.

**Cookies:** Phoenix session cookies (`_my_app_key`, etc.) need `--cookie` /
`--cookie-file` / `--cdp-url` like any other app.

### Vite (Vue, React, Svelte, …)

Crit detects `/@vite/client` and proxies HMR WebSockets automatically.

```bash
crit live http://localhost:5173
```

Tips:

- Use the Vite **dev** server URL, not a `file://` open or a preview that
  omits HTML shell tags.
- If you set a custom `server.origin` / absolute asset URLs, keep them
  relative or pointed at the proxied host so scripts still load through Crit.
- Strict meta CSP in `index.html` will block injection even though response
  CSP headers are stripped.

### Next.js

Crit detects `id="__next"` and supports client navigations that use
`history.pushState` (the injected route announcer keeps Crit’s chrome in sync).

```bash
crit live http://localhost:3000
```

Tips:

- App Router and Pages Router both work when the response is HTML with a
  `</body>`.
- Middleware that returns non-HTML (JSON, opaque redirects to an IdP) will
  prevent injection on that URL — start Crit on a path that already renders
  the app shell while authenticated (and forward cookies).
- `next start` / production builds may send stricter headers; prefer `next
  dev` for live review.

### Generic Node / Express / Rails / Django / static servers

As long as the URL returns HTML with a `</body>`, Crit can inject. Checklist:

1. Dev server bound and reachable (`curl -I` the same URL you pass to Crit).
2. No meta CSP blocking external scripts.
3. Session cookies forwarded if the page requires auth.
4. Open Crit’s UI at `http://localhost:<crit-port>/live`.

## Debugging checklist

```text
[ ] App URL returns 200 text/html with </body>
[ ] crit live stderr: no fatal smoke errors
[ ] Crit tab is http://localhost:<port>/live (not 127.0.0.1)
[ ] Iframe Network: agent-*.js / crit-agent.js load (200)
[ ] No CSP / blocked-script errors in the iframe console
[ ] After login walls: cookies forwarded (--cookie / --cookie-file / --cdp-url)
[ ] Reload Crit after fixing upstream (no Retry button — structural failures need a real fix)
```

## Related

- [README — Live mode](../README.md#live-mode) (cookies, config keys)
