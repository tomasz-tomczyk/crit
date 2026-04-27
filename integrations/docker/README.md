# Crit in Docker (sandboxed agents)

A recipe for running `crit` inside a container alongside an AI coding agent (Claude Code, etc.), with the review UI accessible from your host browser.

**Mental model:** the agent lives in the container, edits files in a mounted repo, and runs `crit` to request review. You open `http://localhost:8080` on the host to comment on the review. The container is the agent's sandbox; the browser stays on your machine.

## The 127.0.0.1 catch

`crit`'s HTTP server binds to `127.0.0.1` only. This is intentional — there's no auth, and the API can read repo files and (if `agent_cmd` is configured) shell out. Loopback-only keeps the threat model honest for a localhost CLI.

That means a plain `docker run -p 8080:8080` won't reach it: the host port forwards into the container, but crit isn't listening on the container's external interface. The fix is a tiny `socat` bridge inside the container that accepts on `0.0.0.0:8080` and forwards to crit's loopback port. Crit stays loopback-only; only the explicit `-p` mapping exposes anything.

## Files in this directory

- [`Dockerfile`](./Dockerfile) — multi-stage build: golang to fetch `crit`, node:22-slim runtime with claude code, socat, git
- [`entrypoint.sh`](./entrypoint.sh) — starts the socat bridge, then execs your command

Copy both into a directory and build:

```bash
docker build -t crit-agent .
```

## Running it

Mount your repo and map the bridge port:

```bash
docker run -it --rm \
    -v "$PWD":/workspace/repo \
    -w /workspace/repo \
    -p 8080:8080 \
    crit-agent
```

Inside the container, run your agent normally. When it invokes `crit plan.md` (or any other crit command that starts the server), open `http://localhost:8080` in your host browser.

For non-interactive agent runs, replace `bash` with the agent command:

```bash
docker run --rm -v "$PWD":/workspace/repo -w /workspace/repo -p 8080:8080 \
    crit-agent claude -p "review the diff with crit"
```

## Multiple agents in parallel

Each container needs a distinct host port. Crit's internal port can stay the same:

```bash
docker run -d --name agent-a -v "$PWD/a":/workspace/repo -w /workspace/repo -p 8080:8080 crit-agent ...
docker run -d --name agent-b -v "$PWD/b":/workspace/repo -w /workspace/repo -p 8081:8080 crit-agent ...
```

Open `http://localhost:8080` for agent-a, `http://localhost:8081` for agent-b.

## What crit needs at runtime

- The mounted repo (read/write — crit writes review files)
- `~/.crit/reviews/<key>.json` — review storage. Persists inside the container; mount a volume at `/root/.crit` if you want reviews to survive `docker rm`.
- Network: nothing, unless you use `crit share` (which talks to the hosted relay at crit.md) or `crit pull/push` (which uses the `gh` CLI for GitHub PR sync — install it separately if needed).

No daemon, no database, no background services. The `crit` binary is ~20 MB static Go, and reviews are plain JSON files.

## Customising

- **Pin a crit version:** change `go install github.com/tomasz-tomczyk/crit@latest` to `@v0.10.1` (or whichever release tag).
- **Different agent:** swap the `npm install -g @anthropic-ai/claude-code` line for whatever your tool uses (e.g. `pnpm add -g opencode`, or `pip install` something).
- **Persist auth:** mount `~/.claude` (or the agent's config dir) as a volume so login state survives container restarts.
- **Different ports:** override `BRIDGE_PORT` and/or `CRIT_PORT` via `-e`. The entrypoint shifts `CRIT_PORT` by +1 if you accidentally set them equal.
