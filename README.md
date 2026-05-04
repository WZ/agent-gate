# agent-gate

A personal audit gate for Claude Code outbound traffic. Runs locally on your machine,
intercepts every HTTPS request the agent makes, persists request + response to a
durable JSONL log indexed by SQLite, attaches policy flags (host allowlist,
secret detection, etc.), and lets you review everything in a local web dashboard.

This is **Plan 1 + Plan 2** (MVP backbone + policy + dashboard). The sandboxed
launcher (`agent-gate run --airtight`) ships in Plan 3.

## What you get

**Getting started:**
- `agent-gate init` — one-command bootstrap: writes config, mints the local CA,
  detects installed agents (claude / codex / aider / opencode) and seeds their
  upstream hosts into your allowlist, installs the CA into your system trust
  stores (Keychain on macOS, ca-certificates + Firefox NSS on Linux, wincrypt
  on Windows).
- `agent-gate doctor` — validates every part of the install (CA files, ports,
  lockfile, host-list permissions, agents detected, CA trusted across all
  stores). `--auto-repair=safe|aggressive` to apply fixes.

**Daily use:**
- `agent-gate run -- <cmd>` — launch a command with airtight network capture
  (per-OS network jail forces all egress through the proxy).
- `agent-gate dashboard` — local web app on `127.0.0.1:7878` for review.
- `agent-gate proxy` — TLS-MITM proxy on `127.0.0.1:8888` (foreground; usually
  spawned by `agent-gate run`).
- `agent-gate tail` — polling tail of captured events.
- `agent-gate stop` — SIGTERM a stuck `agent-gate run`.

**Maintenance:**
- `agent-gate cert install` / `cert uninstall` / `cert path` — manage the local
  CA in your trust stores (macOS Keychain, Linux ca-certificates, Windows
  wincrypt, Firefox NSS).
- `agent-gate help allowlist|denylist|passthrough` — explain the three-list
  policy model.
- `agent-gate version`.

## Install

### Download the binary (recommended)

Grab the archive for your platform from the
[latest release](https://github.com/WZ/agent-gate/releases/latest):

| Platform | Archive |
|---|---|
| macOS Apple Silicon | `agent-gate_<ver>_darwin_arm64.tar.gz` |
| macOS Intel | `agent-gate_<ver>_darwin_x86_64.tar.gz` |
| Linux arm64 | `agent-gate_<ver>_linux_arm64.tar.gz` |
| Linux x86_64 | `agent-gate_<ver>_linux_x86_64.tar.gz` |
| Windows x86_64 | `agent-gate_<ver>_windows_x86_64.zip` |

Extract, then move `agent-gate` to a directory on your `PATH`. On
macOS, the first run may need `xattr -d com.apple.quarantine ./agent-gate`
to clear Gatekeeper.

### Or build from source

```bash
go build -o agent-gate ./cmd/agent-gate
sudo mv agent-gate /usr/local/bin/
```

### Or `go install`

```bash
go install agent-gate/cmd/agent-gate@latest
```

## First-time setup

```bash
agent-gate init
```

That's it. `agent-gate init` writes the config, mints the local CA,
detects which AI agents you have installed (`claude`, `codex`, `aider`,
`opencode`), pre-checks their upstream hosts in an interactive allowlist
prompt, and installs the CA into your system trust stores (Keychain on
macOS, `ca-certificates` + Firefox NSS on Linux, wincrypt on Windows) via
[`smallstep/truststore`](https://github.com/smallstep/truststore).

For headless / CI use:

```bash
agent-gate init --non-interactive --allow-host api.anthropic.com --install-cert=false
agent-gate cert install   # run later from a TTY
```

To validate the install at any time:

```bash
agent-gate doctor
```

`doctor` checks every moving part — CA files, trust-store presence,
port availability, lockfile freshness, host-list permissions, agents
detected — and prints a one-line-per-check report with suggested fixes.
Pass `--auto-repair=safe` for filesystem-perm fixes (never sudos);
`--auto-repair=aggressive` will attempt `cert install` on a missing
trust-store entry.

On Windows, `agent-gate init` additionally registers the WFP provider and
sublayer used by airtight mode. Run from an elevated PowerShell once.

## Upgrading from older versions

agent-gate v0.6.0 changes a few things you might notice:

1. **The runtime no longer auto-adds `api.anthropic.com` to your
   allowlist on every load.** If you removed it from the dashboard
   previously and it "kept coming back," that bug is gone. Hosts you
   remove stay removed. Run `agent-gate doctor` to see whether your
   allowlist is what you expect.
2. **`config.toml` no longer accepts `[allowlist] file = "..."`.** That
   field was always ignored by the runtime; v0.6.0 removes it from the
   schema. Existing config files with the field are silently tolerated.
   The allowlist always lives at `~/.config/agent-gate/allowlist.txt`.
3. **`cert install` no longer prints "manual install" instructions for
   Linux/Windows.** The new truststore-backed installer handles
   ca-certificates / Firefox NSS / wincrypt automatically. macOS still
   prompts for sudo to write the System Keychain.
4. **You don't need to run `cert install` separately after `init`**
   unless you skipped it with `--skip-cert-install` or it failed.

To regenerate your CA: `agent-gate init --force --regenerate-ca`. With
`--force`, agent-gate overlays your existing user-set ports / storage /
capture mode onto the new commented template — no values lost.

## CLI summary

```
Getting started:
  agent-gate init                  one-command bootstrap (config + CA + agent detection + cert install)
  agent-gate doctor                validate the install; suggest or apply repairs

Daily use:
  agent-gate run -- <cmd>          launch a command with airtight network capture
  agent-gate dashboard             run the local web dashboard (foreground)
  agent-gate proxy                 run the proxy alone (foreground)
  agent-gate tail                  live-follow events in the terminal
  agent-gate stop                  send SIGTERM to a stuck `agent-gate run`

Maintenance:
  agent-gate cert install          install the local CA into trust stores
  agent-gate cert uninstall        remove the local CA from trust stores
  agent-gate cert path             print the CA cert path
  agent-gate uninstall             (Windows) remove the WFP provider/sublayer
  agent-gate version               print version info

Help topics:
  agent-gate help allowlist        explain allowlist semantics
  agent-gate help denylist         explain denylist semantics
  agent-gate help passthrough      explain passthrough semantics
```

Each takes `--config PATH` to point at a non-default `config.toml`.

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
  --permissive                       env-only enforcement (HTTPS_PROXY exported, no kernel jail)
  --airtight-fail                    refuse to fall back to permissive if airtight unsupported
  --enforce-allowlist                proxy returns 403 for hosts not in the allowlist
                                       (overrides [allowlist].enforce in config.toml)
  --upstream-ca PEM                  extra root CA(s) to trust on proxy→upstream
                                       (use for self-signed ANTHROPIC_BASE_URL)
  --upstream-insecure-skip-verify    skip upstream cert verification entirely
                                       (testing only; captures still happen)
  --config PATH                      config.toml path
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

### Recommended: launch through `agent-gate run`

One command starts the proxy + dashboard + jail and runs your agent inside it:

```bash
agent-gate run -- claude
```

While Claude is running, open <http://127.0.0.1:7878> — sessions, events, and
flags appear live. When Claude exits, agent-gate tears everything down and
releases the lockfile.

If your shell aliases `claude` with flags (fish/zsh aliases aren't visible to
`exec`), invoke through your shell so the alias resolves:

```bash
agent-gate run -- fish -ic 'claude'
```

For a self-hosted Anthropic-compatible endpoint with a non-standard cert:

```bash
echo | openssl s_client -connect your-anthropic-endpoint.example:443 -servername your-anthropic-endpoint.example 2>/dev/null \
  | openssl x509 -outform PEM > /tmp/upstream.pem
ANTHROPIC_BASE_URL=https://your-anthropic-endpoint.example \
ANTHROPIC_API_KEY=$YOUR_KEY \
  agent-gate run --upstream-ca /tmp/upstream.pem -- claude
```

### Alternative: standalone proxy + dashboard

For ad-hoc captures (curl, an existing daemon, a script), run the proxy and
dashboard separately and point a client at the proxy via env:

```bash
agent-gate proxy --capture-mode permissive   # terminal 1
agent-gate dashboard                          # terminal 2

HTTPS_PROXY=http://127.0.0.1:8888 \
HTTP_PROXY=http://127.0.0.1:8888 \
NO_PROXY="" \
  curl https://api.anthropic.com/v1/messages \
       -H "x-api-key: $ANTHROPIC_API_KEY" \
       -H "anthropic-version: 2023-06-01" \
       -H "content-type: application/json" \
       -d '{"model":"claude-opus-4-7","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

Same dashboard at <http://127.0.0.1:7878>. No kernel jail in this mode — the
client must honor `HTTPS_PROXY`.

## Where things live

- Config: `~/.config/agent-gate/config.toml`
- CA: `~/.config/agent-gate/ca/{cert.pem,key.pem}`
- Allowlist: `~/.config/agent-gate/allowlist.txt` — hosts you've Trusted (no flag, optionally let-through under enforce mode)
- Denylist: `~/.config/agent-gate/denylist.txt` — hosts you've Blocked (always 403, regardless of enforce)
- Passthrough: `~/.config/agent-gate/passthrough.txt` — cert-pinned hosts where TLS won't be intercepted (raw TCP tunnel)
- Dismissals: `~/.config/agent-gate/dismissals.json` (also logs `raw_peek` events)
- Lockfile: `~/.local/share/agent-gate/agent-gate.lock` — `agent-gate run`'s PID; auto-reclaimed if stale
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

Each event detail page has three host-policy buttons + per-flag Dismiss:

- **Trust this host** (dark) → appends to `allowlist.txt`. The
  `host_not_allowlisted` flag stops firing, and under `--enforce-allowlist` the
  proxy lets the request through. Effective immediately.
- **Block this host** (red) → appends to `denylist.txt`. Future requests
  receive 403 from the proxy, never reach upstream. Always wins, regardless of
  enforce mode. Effective immediately.
- **Passthrough (no MITM)** (amber) → appends to `passthrough.txt`. agent-gate
  tunnels TCP raw to that host without TLS interception. Use for cert-pinned
  upstreams (e.g. `mcp-proxy.anthropic.com`) that reject MITM. Effective on
  next `agent-gate run` restart.

Per-flag **Dismiss** writes to `dismissals.json` with a free-text reason and a
timestamp. Priority: denylist > passthrough > allowlist > default (forward + flag).

## What's next

See `TODOS.md` for the next two release cuts:

- **Plan 4 (`v0.4.0-windows-airtight`)** — Windows airtight runtime: Job
  Object + WFP per-exe filters + completion-port listener for descendants.
  Removes the "pending Plan 4" stub from `agent-gate run` on Windows.
- **Plan 5 (`v0.5.0-multi-vendor`)** — first-class parser branches for
  OpenAI Chat Completions, OpenAI Responses, Anthropic Bedrock, and
  Vertex `generateContent`. Other agents (OpenClaw, Aider, anything
  that talks HTTP) already work via the generic parser; Plan 5 makes
  their dashboard view as rich as Anthropic's.

## Limitations (Plan 3)

- HTTP/1.1 client-facing; upstream HTTP/2 transparent.
- Windows airtight is stubbed — Windows targets fall back to `--permissive`
  with a clear message. Plan 4 lands the Job Object + WFP filter runtime path.
- macOS airtight only matches loopback to the proxy port (no other localhost
  ports). MCP-over-localhost-HTTP works because the proxy reaches into the
  child's loopback, not the other direction.
- Linux airtight requires `kernel.unprivileged_userns_clone=1`. Hardened
  distros (Ubuntu 24+ with `kernel.apparmor_restrict_unprivileged_userns=1`,
  hardened kernels) fall back to `--permissive` unless `--airtight-fail` is set.
- Cert-pinned upstreams (`mcp-proxy.anthropic.com` and similar) reject TLS
  interception. Add them to `passthrough.txt` (or click **Passthrough** on an
  event) so agent-gate tunnels TCP raw — body capture is skipped, only the
  CONNECT host + byte counts get audited.
- Some TUIs (Claude Code) catch SIGINT and don't propagate exit on Ctrl-C.
  Type `/exit` inside Claude or run `agent-gate stop` from another terminal.
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
