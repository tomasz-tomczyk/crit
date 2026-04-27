# Reference Dockerfile for running crit alongside an AI coding agent
# in a sandboxed container. See README.md in this directory for the full
# explanation. The image installs:
#   - crit (built from latest main via `go install`)
#   - claude code (npm)
#   - socat (bridges crit's loopback-bound server to 0.0.0.0 inside the
#     container so docker -p forwarding works — crit itself stays bound
#     to 127.0.0.1)

FROM golang:1.26-bookworm AS builder
RUN go install github.com/tomasz-tomczyk/crit@latest

FROM node:22-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        git ca-certificates socat \
    && rm -rf /var/lib/apt/lists/*
RUN npm install -g @anthropic-ai/claude-code
COPY --from=builder /go/bin/crit /usr/local/bin/crit
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# BRIDGE_PORT: container-external port (what you map with `docker -p`)
# CRIT_PORT:   crit's internal loopback port
ENV CRIT_PORT=8081 BRIDGE_PORT=8080

WORKDIR /workspace
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["bash"]
