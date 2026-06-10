#!/usr/bin/env bash
# Bridges the container's external port (BRIDGE_PORT) to crit's loopback
# server on CRIT_PORT. crit binds 127.0.0.1 only by design — no auth, can
# read repo files and trigger configured agent_cmd. socat keeps that
# threat model intact: only the explicit `docker -p` mapping exposes
# crit, and only inside the container's network namespace.
set -euo pipefail

: "${CRIT_PORT:=8081}"
: "${BRIDGE_PORT:=8080}"

if [[ "$BRIDGE_PORT" == "$CRIT_PORT" ]]; then
  # Can't bind the same port twice — shift crit internally.
  export CRIT_PORT=$((CRIT_PORT + 1))
fi

socat TCP-LISTEN:"$BRIDGE_PORT",fork,reuseaddr,bind=0.0.0.0 \
      TCP:127.0.0.1:"$CRIT_PORT" &

exec "$@"
