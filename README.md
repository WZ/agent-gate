# agent-gate

A personal audit gate for Claude Code outbound traffic. Runs locally on your machine,
intercepts every HTTPS request the agent makes, and persists request + response to a
durable JSONL log indexed by SQLite — so you can review what the agent actually sent
and received.

This is the **MVP backbone** (Plan 1 of 4). Policy/flag rules and a web dashboard
ship in Plan 2. The sandboxed launcher (`agent-gate run --airtight`) ships in Plan 3.

## What you get today (after Plan 1)

- `agent-gate proxy` — TLS-MITM proxy on `127.0.0.1:8888` (config-overridable).
- `agent-gate cert install` — installs the local root CA on macOS.
- `agent-gate tail` — polling tail of captured events.
- `agent-gate cert path` — print the CA path for manual install on Linux/Windows.
- `agent-gate version` — version.

## Install

```bash
go build -o agent-gate ./cmd/agent-gate
sudo mv agent-gate /usr/local/bin/
```

## First-time setup

```bash
agent-gate cert install        # macOS — prompts for password.
                               # Linux/Windows: prints manual steps.
```

## Run the proxy

```bash
agent-gate proxy --capture-mode permissive
```

In another terminal, point a client at it:

```bash
HTTPS_PROXY=http://127.0.0.1:8888 \
HTTP_PROXY=http://127.0.0.1:8888 \
NO_PROXY="" \
  curl https://api.anthropic.com/v1/messages \
       -H "x-api-key: $ANTHROPIC_API_KEY" \
       -H "anthropic-version: 2023-06-01" \
       -d '{"model":"claude-opus-4-7","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

Then:

```bash
agent-gate tail
```

You should see one line for the request you just made.

## Where things live

- Config: `~/.config/agent-gate/config.toml`
- CA: `~/.config/agent-gate/ca/{cert.pem,key.pem}`
- Data: `~/.local/share/agent-gate/{events.db, YYYY-MM-DD.jsonl}`

## Limitations (Plan 1)

- HTTP/1.1 client-facing (Anthropic SDKs auto-fallback). Upstream HTTP/2 transparent.
- No policy / flag rules yet (Plan 2).
- No web dashboard yet (Plan 2).
- No sandboxed launcher yet — use only with the env-var capture mode (`--permissive`).

## Project layout

```
cmd/agent-gate/      CLI entrypoint
internal/types/      Shared types (RawFlow, ParsedEvent, …)
internal/config/     TOML config loader
internal/idgen/      ULID generator
internal/ca/         Local root CA + leaf signing
internal/proxy/      TLS-intercepting proxy (goproxy)
internal/parser/     Anthropic Messages decoder + generic fallback
internal/store/      JSONL + SQLite persistence
internal/e2e/        End-to-end integration test
testdata/flows/      Recorded flow fixtures
```

## Development

```bash
make build   # build the binary
make test    # unit tests
make e2e     # integration test
make lint    # go vet
```
