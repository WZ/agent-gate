# agent-gate runbook

Operational reference for the CLI: every command, every flag, and the
recipes for the common ways to run agent-gate.

The README is the product overview ("what is this, why would I use it,
how do I get the screenshots in 30 seconds"). This file is the
reference manual ("which flag does what, and how do I do _X_").

> Source of truth for flags is `agent-gate <subcommand> --help` from the
> binary itself. The blocks below mirror what `--help` prints.

## Table of contents

- [Command list](#command-list)
- [`agent-gate run`](#agent-gate-run)
- [`agent-gate init`](#agent-gate-init)
- [`agent-gate doctor`](#agent-gate-doctor)
- [Workflows](#workflows)
  - [Recommended: launch through `agent-gate run`](#recommended-launch-through-agent-gate-run)
  - [Alternative: standalone proxy + dashboard](#alternative-standalone-proxy--dashboard)
  - [Self-hosted Anthropic-compatible endpoint](#self-hosted-anthropic-compatible-endpoint)
- [Install](#install)
  - [Homebrew (recommended on macOS)](#homebrew-recommended-on-macos)
  - [Download the binary](#download-the-binary)
  - [Build from source](#build-from-source)
- [Where things live on disk](#where-things-live-on-disk)
- [Operational caveats](#operational-caveats)

## Command list

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
  agent-gate reindex               rebuild the PII count index from JSONL
  agent-gate uninstall             (Windows) remove the WFP provider/sublayer
  agent-gate version               print version info

Help topics:
  agent-gate help allowlist        explain allowlist semantics
  agent-gate help denylist         explain denylist semantics
  agent-gate help passthrough      explain passthrough semantics
```

Every subcommand accepts `--config PATH` to point at a non-default
`config.toml`.

## `agent-gate run`

Launches a command with the proxy + dashboard + per-OS network jail
spun up around it. This is the recommended entry point.

```
agent-gate run [flags] -- <cmd> [args...]

Flags:
  --mode VALUE             egress enforcement mode (default: airtight). Choose:
                             airtight        per-OS network jail; falls back to
                                               permissive with a stderr warning if
                                               unsupported (e.g. hardened Linux,
                                               Windows today)
                             airtight-strict require the jail; abort with non-zero
                                               exit if unsupported (use in CI)
                             permissive      skip jail; just sets HTTPS_PROXY env
                                               vars on the child. Agent CAN ignore
                                               them and bypass capture
  --enforce-allowlist      proxy returns 403 for hosts not in allowlist.txt;
                             overrides [allowlist].enforce in config.toml
  --config PATH            path to config.toml

Advanced flags (most users don't need these):
  --upstream-ca PEM                  extra root CA(s) to trust on proxy→upstream
                                       (use for self-signed ANTHROPIC_BASE_URL etc.)
  --upstream-insecure-skip-verify    skip upstream cert verification entirely
                                       (testing only; captures still happen)
  --hijack-host HOST                 capture WebSocket message bodies for HOST.
                                       claude / codex / aider are captured by
                                       default and don't need this. Reach for it
                                       only when auditing a custom or internal
                                       agent that talks to your own WebSocket
                                       backend (repeatable)
```

### Picking a mode

| Situation | Use |
|---|---|
| You just want to capture your agent's traffic | `--mode airtight` (default — no flag needed) |
| CI run where partial capture is worse than no run | `--mode airtight-strict` |
| Platform without jail support, or a one-off you trust | `--mode permissive` |

### When to reach for the advanced flags

- **`--upstream-ca`** — only when the proxy needs to talk to a self-signed upstream whose cert isn't in your OS trust store. Common case: a self-hosted `ANTHROPIC_BASE_URL` or an internal LiteLLM-style gateway.
- **`--upstream-insecure-skip-verify`** — last resort when the upstream cert is structurally non-compliant (missing SANs, etc.) and `--upstream-ca` can't fix it. Captures still happen; upstream identity is unverified. Prints a `⚠ upstream TLS verification DISABLED` warning each run.
- **`--hijack-host`** — for non-pinned WebSocket upstreams where you want frame bodies. claude / codex / aider don't need it; their traffic is captured by default. codex on `chatgpt.com` pins TLS on its WS transport, so the flag is ineffective there — codex's HTTP fallback path is captured without it.

## `agent-gate init`

```
agent-gate init [flags]

Flags:
  --non-interactive          skip all prompts; use defaults / flags
  --install-cert auto|true|false   install local CA into system trust stores
  --skip-cert-install        equivalent to --install-cert=false
  --regenerate-ca            force-regenerate the local CA (rotates the cert)
  --allow-host HOST          seed allowlist with HOST (repeatable; replaces detection)
  --force                    overwrite existing config.toml
  --quiet                    skip welcome and policy summary notes
  --dry-run                  print planned writes; change nothing
  --print-config             emit the would-be config.toml on stdout; exit 0
  --config PATH              path to config.toml
```

Headless / CI:

```bash
agent-gate init --non-interactive --allow-host api.anthropic.com --install-cert=false
agent-gate cert install   # run later from a TTY (sudo prompt)
```

## `agent-gate doctor`

```
agent-gate doctor [flags]

Flags:
  --auto-repair safe|aggressive    safe: filesystem fixes only;
                                   aggressive: also retry trust-store install
  --json                           machine-readable output
  --config PATH                    path to config.toml
```

## Workflows

### Recommended: launch through `agent-gate run`

One command starts the proxy + dashboard + jail and runs the agent inside it:

```bash
agent-gate run -- claude
```

While the agent is running, open <http://127.0.0.1:7878> — sessions, events,
and flags appear live. When the agent exits, agent-gate tears everything
down and releases the lockfile.

If your shell aliases the agent name (fish/zsh aliases aren't visible to
`exec`), invoke through your shell so the alias resolves:

```bash
agent-gate run -- fish -ic 'claude'
```

### Alternative: standalone proxy + dashboard

For ad-hoc captures (curl, an existing daemon, a script), run the proxy
and dashboard in separate terminals and point clients at the proxy via
env vars:

```bash
# terminal 1
agent-gate proxy --capture-mode permissive

# terminal 2
agent-gate dashboard

# any other terminal — point a client at the proxy
HTTPS_PROXY=http://127.0.0.1:8888 \
HTTP_PROXY=http://127.0.0.1:8888 \
NO_PROXY="" \
  curl https://api.anthropic.com/v1/messages \
       -H "x-api-key: $ANTHROPIC_API_KEY" \
       -H "anthropic-version: 2023-06-01" \
       -H "content-type: application/json" \
       -d '{"model":"claude-opus-4-7","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

Same dashboard at <http://127.0.0.1:7878>. No kernel jail in this mode —
the client must honor `HTTPS_PROXY`.

### Self-hosted Anthropic-compatible endpoint

If you point `ANTHROPIC_BASE_URL` at a self-hosted endpoint with a cert
that isn't in your OS trust store, fetch the cert and pass it via
`--upstream-ca`:

```bash
echo | openssl s_client -connect your-endpoint.example:443 -servername your-endpoint.example 2>/dev/null \
  | openssl x509 -outform PEM > /tmp/upstream.pem

ANTHROPIC_BASE_URL=https://your-endpoint.example \
ANTHROPIC_API_KEY=$YOUR_KEY \
  agent-gate run --upstream-ca /tmp/upstream.pem -- claude
```

If the upstream cert is structurally broken (e.g. no SANs), Go's TLS
stack will reject it regardless of trust. Use
`--upstream-insecure-skip-verify` as a last resort.

## Install

### Homebrew (recommended on macOS)

```bash
brew tap WZ/tap
brew install agent-gate
```

To upgrade in place:

```bash
brew upgrade agent-gate
```

The formula publishes automatically on every `vX.Y.Z` tag, so
`brew upgrade` always tracks the newest release.

### Download the binary

Grab the archive for your platform from the
[latest release](https://github.com/WZ/agent-gate/releases/latest):

| Platform | Archive |
|---|---|
| macOS Apple Silicon | `agent-gate_<ver>_darwin_arm64.tar.gz` |
| macOS Intel | `agent-gate_<ver>_darwin_x86_64.tar.gz` |
| Linux arm64 | `agent-gate_<ver>_linux_arm64.tar.gz` |
| Linux x86_64 | `agent-gate_<ver>_linux_x86_64.tar.gz` |
| Windows x86_64 | `agent-gate_<ver>_windows_x86_64.zip` (permissive capture today; airtight is Plan 4) |

Extract, then move `agent-gate` to a directory on your `PATH`. On macOS,
the first run may need `xattr -d com.apple.quarantine ./agent-gate` to
clear Gatekeeper.

### Build from source

```bash
git clone https://github.com/WZ/agent-gate.git
cd agent-gate

# Plain build — produces "agent-gate 0.0.1-dev (commit unknown, built unknown)"
go build -o agent-gate ./cmd/agent-gate

# Or with version metadata baked in (matches what goreleaser ships):
VERSION=$(git describe --tags --always)
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o agent-gate ./cmd/agent-gate

sudo mv agent-gate /usr/local/bin/
```

> `go install agent-gate/cmd/agent-gate@latest` does **not** work today
> — the module path in `go.mod` is the bare name `agent-gate` rather
> than a domain-prefixed path, so the toolchain can't fetch it from a
> remote. Use the binary download or `go build` from a clone.

## Where things live on disk

- Config: `~/.config/agent-gate/config.toml`
- CA: `~/.config/agent-gate/ca/{cert.pem,key.pem}` (key is `0600` on macOS/Linux; Windows skips the unix-mode check)
- Allowlist / denylist / passthrough: `~/.config/agent-gate/{allowlist,denylist,passthrough}.txt`
- Dismissals: `~/.config/agent-gate/dismissals.json` (also logs `raw_peek` events)
- Lockfile: `~/.local/share/agent-gate/agent-gate.lock` — auto-reclaimed if stale
- Data: `~/.local/share/agent-gate/{events.db, YYYY-MM-DD.jsonl}`

## Operational caveats

- HTTP/1.1 client-facing; upstream HTTP/2 transparent.
- Windows airtight is stubbed — Windows targets fall back to `--mode=permissive` with a clear message. Plan 4 lands the Job Object + WFP filter runtime path.
- macOS airtight only matches loopback to the proxy port (no other localhost ports). MCP-over-localhost-HTTP works because the proxy reaches into the child's loopback, not the other direction.
- Linux airtight requires `kernel.unprivileged_userns_clone=1`. Hardened distros (Ubuntu 24+ with `apparmor_restrict_unprivileged_userns=1`, hardened kernels) fall back to `--mode=permissive` unless `--mode=airtight-strict` is set.
- Codex 0.128.0 client-pins TLS on its WebSocket transport. Empty 101 upgrades on `chatgpt.com` are expected and get flagged `ws_pinned_upstream` for clarity. The actual model conversation lands via codex's HTTP fallback path (`POST /backend-api/codex/responses`), which agent-gate decodes through the same OpenAI Responses parser used for `api.openai.com`.
- Cert-pinned upstreams reject TLS interception. Add them to `passthrough.txt` so agent-gate tunnels TCP raw — body capture is skipped, only the CONNECT host + byte counts get audited.
- Some TUIs (Claude Code) catch SIGINT and don't propagate exit on Ctrl-C. Type `/exit` inside the agent or run `agent-gate stop` from another terminal.
- Custom rules via TOML config: schema is reserved but not yet wired.
