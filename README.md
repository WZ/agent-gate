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
agent-gate init           # bootstrap config + CA + dirs (one-time)
agent-gate cert install   # macOS only; Linux/Windows print manual steps.
```

On Windows, `agent-gate init` additionally registers the WFP provider and
sublayer used by airtight mode. Run from an elevated PowerShell once.

## Airtight launcher

`agent-gate run -- <cmd>` spawns the target inside a per-platform network jail
that physically forces all egress through the proxy.

- **macOS**: a `sandbox-exec` profile denies all `network*` ops except
  loopback to the proxy port. No installation step needed; descendants
  inherit the sandbox automatically.
- **Linux**: a hidden `__netns-helper` subprocess enters an unprivileged
  user + network namespace, binds the proxy port inside it, and passes the
  listener FD back via `SCM_RIGHTS`. Requires
  `kernel.unprivileged_userns_clone=1` (default on Ubuntu/Fedora; some
  hardened distros disable it — enable with
  `sudo sysctl -w kernel.unprivileged_userns_clone=1`).
- **Windows**: pending Plan 4. The runtime path is currently stubbed; use
  `--permissive`. `agent-gate init` already lands the persistent WFP
  provider/sublayer that Plan 4's runtime will rely on.

### Flags

```
agent-gate run [flags] -- <cmd> [args...]
  --permissive       env-only enforcement (HTTPS_PROXY exported, no kernel jail)
  --airtight-fail    refuse to fall back to permissive if airtight unsupported
  --config PATH      config.toml path
```

Default mode is **airtight**. If airtight isn't available on this OS or this
host's config, agent-gate prints a warning and falls back to permissive — pass
`--airtight-fail` to refuse the fallback.

### Threat model

agent-gate's airtight mode defends against:
- Tools that don't honor `HTTPS_PROXY`. They get kernel-level network deny.
- Subprocess descendants of the target. The jail is inherited (macOS sandbox
  profile, Linux network namespace).

It does **not** defend against:
- Local IPC channels (UNIX domain sockets, named pipes, abstract sockets,
  shared memory). These are out of the proxy's view by design.
- An agent running as root or admin. The user can lift the jail.
- An agent that reads files (e.g., your `.env`) directly. The proxy is a
  network audit, not a filesystem audit.
- Steganographic exfiltration through allowed hosts.

If your threat model needs filesystem isolation or RBAC, agent-gate alone is
insufficient.

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

## Limitations (Plan 3)

- HTTP/1.1 client-facing; upstream HTTP/2 transparent.
- Windows airtight is stubbed — Windows targets fall back to `--permissive`
  with a clear message. Plan 4 lands the Job Object + WFP filter runtime path.
- macOS airtight only matches loopback to the proxy port (no other localhost
  ports). MCP-over-localhost-HTTP works because the proxy reaches into the
  child's loopback, not the other direction.
- Linux airtight requires `kernel.unprivileged_userns_clone=1`. Hardened
  distros that disable this fall back to `--permissive` unless
  `--airtight-fail` is set.
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
