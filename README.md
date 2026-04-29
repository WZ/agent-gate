# agent-gate

A personal audit gate for Claude Code outbound traffic. Runs locally on your machine,
intercepts every HTTPS request the agent makes, persists request + response to a
durable JSONL log indexed by SQLite, attaches policy flags (host allowlist,
secret detection, etc.), and lets you review everything in a local web dashboard.

This is **Plan 1 + Plan 2** (MVP backbone + policy + dashboard). The sandboxed
launcher (`agent-gate run --airtight`) ships in Plan 3.

## What you get

- `agent-gate proxy` — TLS-MITM proxy on `127.0.0.1:8888`. Every captured event
  is run through the policy engine before being persisted.
- `agent-gate dashboard` — local web app on `127.0.0.1:7878` for review.
- `agent-gate cert install` — installs the local root CA on macOS.
- `agent-gate tail` — polling tail of captured events.
- `agent-gate cert path` / `version`.

## Install

```bash
go build -o agent-gate ./cmd/agent-gate
sudo mv agent-gate /usr/local/bin/
```

## First-time setup

```bash
agent-gate cert install   # macOS only; Linux/Windows print manual steps.
```

## Workflow

In one terminal:

```bash
agent-gate proxy --capture-mode permissive
```

In another:

```bash
agent-gate dashboard
```

Now point a client at the proxy:

```bash
HTTPS_PROXY=http://127.0.0.1:8888 \
HTTP_PROXY=http://127.0.0.1:8888 \
NO_PROXY="" \
  curl https://api.anthropic.com/v1/messages \
       -H "x-api-key: $ANTHROPIC_API_KEY" \
       -H "anthropic-version: 2023-06-01" \
       -H "content-type: application/json" \
       -d '{"model":"claude-opus-4-7","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

Open <http://127.0.0.1:7878> — your sessions, events, and flags appear live.

## Where things live

- Config: `~/.config/agent-gate/config.toml`
- CA: `~/.config/agent-gate/ca/{cert.pem,key.pem}`
- Allowlist: `~/.config/agent-gate/allowlist.txt`
- Dismissals: `~/.config/agent-gate/dismissals.json` (also logs `raw_peek` events)
- Data: `~/.local/share/agent-gate/{events.db, YYYY-MM-DD.jsonl}`

## Built-in policy rules

| Code | Severity | Fires when |
|---|---|---|
| `host_not_allowlisted` | high | request host is not in the allowlist |
| `secret_in_request` | high | request body matches a credential pattern |
| `env_in_tool_result` | high | tool_result contains ≥3 KEY=VALUE lines |
| `oversized_request` | medium | request body > 5 MB |
| `oversized_response` | low | response body > 5 MB |
| `unknown_mcp_endpoint` | medium | response is `text/event-stream` and host is unknown |
| `permissive_capture` | info | session captured under env-only enforcement |
| `parse_error` | info | parser annotated an error on the flow |

Trust a host with the **Trust** button; dismiss a flag with the **Dismiss** button.
Both write to disk; trust appends to `allowlist.txt`, dismiss appends to `dismissals.json`.
Every dismissal includes a free-text reason and a timestamp.

## Limitations (Plans 1+2)

- HTTP/1.1 client-facing; upstream HTTP/2 transparent.
- No sandboxed launcher yet — `--permissive` only.
- Filter chips only (no full-text body search; defer to FTS5 later).
- Custom rules via TOML config: schema is reserved but not yet wired.

## Project layout

```
cmd/agent-gate/        CLI entrypoint
internal/types/        Shared types
internal/config/       TOML config loader
internal/idgen/        ULID generator
internal/ca/           Local root CA
internal/proxy/        TLS-intercepting proxy
internal/parser/       Anthropic Messages decoder + generic
internal/store/        JSONL + SQLite persistence
internal/secrets/      Canonical regex set
internal/redactor/     Render-time secret masking
internal/allowlist/    File-backed host trust list
internal/dismissals/   File-backed dismissal log
internal/policy/       Rule engine + 8 built-ins
internal/dashboard/    HTTP server + HTMX templates
internal/e2e/          End-to-end integration tests
```

## Development

```bash
make build   # build the binary
make test    # unit tests
make e2e     # integration tests
make lint    # go vet
```
