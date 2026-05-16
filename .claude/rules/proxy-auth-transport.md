# Proxy Auth Transport Rules

<important if="you are adding or modifying any code path that communicates with crit-web (share, pull, fetch, unpublish, or any new crit-web API interaction)">

## Rule 1: Both transport modes required

Every CLI command or browser UI action that calls a crit-web API must work in both modes:
- **Direct** (default): Go server makes HTTP requests with bearer token
- **Browser relay** (`proxy_auth: true`): Browser popup mediates the same API call through an authenticated session

When adding a new crit-web interaction:
1. Add the direct HTTP path in Go (share.go or relevant file)
2. Add a server endpoint that returns the payload for the popup relay (server.go)
3. Add a popup handler in `crit-web/assets/js/share_receiver/handlers.js` (note: the directory/route is named `share_receiver` / `/share-receiver` — this describes what the component *does* (receives share data), while `proxy_auth` describes *why* it's needed. Both names are correct at their level.)
4. Add the relay call in `crit/frontend/app.js` gated on `proxy_auth`

## Rule 2: Relay is transport, not protocol

The popup relay must hit the **same crit-web API endpoints** with the **same payload shapes** as the direct path. No relay-specific endpoints on crit-web. The popup handlers are same-origin fetch proxies.

</important>
