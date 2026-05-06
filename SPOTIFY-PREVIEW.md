# Crit popup-relay preview for SSO-protected crit-web

This is a work-in-progress build that adds a browser-popup share flow to
`crit`, intended for self-hosted `crit-web` deployments that sit behind
an SSO reverse proxy the terminal can't authenticate against.

**Status:** preview / not yet merged. Feedback welcome — see the bottom
of this doc for what to send back.

---

## What's different in this build

`crit share`, `crit fetch`, and `crit unpublish` (terminal commands)
still won't work behind your SSO proxy — the binary can't complete the
proxy's interactive auth flow.

What *does* work, when you opt in via config:

- **Share button in the browser UI** — opens a popup against your
  `crit-web`, which authenticates through your SSO normally. Once the
  popup is logged in, a `MessagePort` lets the local Go server proxy
  share / pull / re-share / unpublish API calls through the popup. No
  bearer tokens, no terminal auth.
- **Pull comments** button in the share-notice banner — fetches comments
  from `crit-web` via the same popup.
- **Re-share** button — pulls latest comments and uploads a new round in
  one popup session.
- **Better error message** when the local Go server hits the SSO login
  page directly (it now points at `share_flow: "popup"` instead of
  giving a cryptic JSON-decode error).

Terminal `crit share` / `crit fetch` / `crit unpublish` are intentionally
left unchanged. The long-term answer for terminal commands behind SSO is
proxy bearer-token passthrough plus the existing `crit auth` device-code
flow — that's a separate piece of work.

---

## Components you need

You need both pieces, paired by branch / commit:

| Component | Source | Pin to |
|---|---|---|
| `crit` binary | https://github.com/tomasz-tomczyk/crit/releases/tag/spotify-preview-1 | commit `bb4d9fb` of branch `share-receiver` |
| `crit-web` server | https://github.com/tomasz-tomczyk/crit-web/tree/share-receiver-elixir | commit `ed01b25` of branch `share-receiver-elixir` |

The crit-web branch adds a new route, `GET /share-receiver`, which is
the page the popup opens. The crit binary won't be able to use
`share_flow: "popup"` against a `crit-web` that doesn't have this
route deployed.

---

## Setup

### 1. Deploy `crit-web` from branch `share-receiver-elixir`

Self-host as you normally would (Phoenix release / Docker / direct
mix), but check out the `share-receiver-elixir` branch instead of
`main`:

```bash
git clone https://github.com/tomasz-tomczyk/crit-web.git
cd crit-web
git checkout share-receiver-elixir   # commit ed01b25 or later on this branch
# ... your usual build / migrate / start steps
```

Make sure your reverse proxy passes `GET /share-receiver` through to
the app the same way it does any other authenticated route. The receiver
page renders with no app layout / no JS bundle from `app.js` — it loads
its own minimal script — so any rewriting your proxy does for the main
app should be benign here.

### 2. Install the `crit` preview binary

Pick the right binary for your platform from
https://github.com/tomasz-tomczyk/crit/releases/tag/spotify-preview-1:

- `crit-darwin-arm64` — Apple Silicon Mac
- `crit-darwin-amd64` — Intel Mac
- `crit-linux-amd64` — most Linux x86_64
- `crit-linux-arm64` — Linux ARM (e.g. cloud workstations)

Verify the checksum (optional but recommended):

```bash
curl -L -o crit-darwin-arm64 https://github.com/tomasz-tomczyk/crit/releases/download/spotify-preview-1/crit-darwin-arm64
shasum -a 256 crit-darwin-arm64
# compare to checksums.txt from the release
chmod +x crit-darwin-arm64
mv crit-darwin-arm64 ~/bin/crit   # or wherever your PATH points
```

If macOS Gatekeeper complains about the binary being unsigned, run:

```bash
xattr -d com.apple.quarantine ~/bin/crit
```

(This is a preview build outside the normal Homebrew distribution, so
it isn't notarised.)

### 3. Configure `crit` to use the popup flow

`share_flow` is **config-only** — there's no flag and no env var. It
describes a property of the deployment (your crit-web is behind SSO),
not a per-invocation choice.

Edit `~/.crit.config.json` (create it if it doesn't exist):

```json
{
  "share_url": "https://crit-web.your-spotify-domain.example",
  "share_flow": "popup"
}
```

Or, if you want to scope this to a single repo, put the same keys in
`.crit.config.json` at the repo root.

Verify with:

```bash
crit config
# should print share_url and share_flow as you set them
```

---

## Using it

1. `crit some-file.md` (or `crit` for git-mode review) — opens the
   review surface in your browser like normal.
2. Click **Share**. A popup opens against
   `${share_url}/share-receiver#nonce=…`. Your SSO redirects the popup
   through its login flow. Authenticate.
3. Once the popup is on `/share-receiver`, the local server hands it a
   payload; the popup posts it to `${share_url}/api/reviews` (which
   succeeds because the popup is authenticated). The popup posts the
   resulting review URL back through the `MessagePort`. The local server
   records share state.
4. Share-notice banner appears with **Copy**, **Pull comments**,
   **Re-share**, and **Unpublish** buttons. Each runs through the same
   popup session if it's still open, or opens a fresh popup on demand.

If the popup is blocked on first try, your browser should surface a
"popup blocked" prompt. Allow popups for the local crit URL and click
Share again.

---

## Things to look for / send back

We'd love feedback on:

1. **The popup auth flow itself** — does Spotify SSO complete inside
   the popup window without weirdness? Does it remember the session
   between subsequent share / pull / re-share operations, or does the
   user have to re-auth each time? (Both are interesting answers.)
2. **The "popup-blocked" first-time experience** — is the failure mode
   tolerable, or does it need explicit guidance in the UI?
3. **Terminal commands** — confirm that `crit share` / `crit fetch` /
   `crit unpublish` still fail (expected) but produce the new
   `share_flow: "popup"` hint in the error message. If you see a
   different error, send the exact stderr.
4. **Anything else surprising** — share state desync, stale comments
   after Re-share, popup orphaned after closing the main browser tab,
   etc.

Send findings to the issue:
https://github.com/tomasz-tomczyk/crit-web/issues/50

---

## Known gaps in this preview

- **No JS unit test runner** in the receiver bundle. The handler module
  (`assets/js/share_receiver/handlers.js`) is structured for vitest
  but tests aren't wired up yet. Logic is exercised manually via
  `test/manual/share_receiver_qa.md` in the crit-web repo.
- **No COOP regression test** — there's nothing currently asserting
  that the `/share-receiver` page doesn't accidentally pick up a
  `Cross-Origin-Opener-Policy: same-origin` header (which would silently
  break `window.opener.postMessage`).
- **No Playwright E2E** for the popup flow — the receiver page can only
  be exercised in a real browser.

These don't block testing, but mean that future changes in this area
might silently regress. We'll close them before final merge.
