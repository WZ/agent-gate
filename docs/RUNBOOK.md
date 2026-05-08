# agent-gate runbook

How to install agent-gate, how to run it, and what every flag does.

> Source of truth for flags is `agent-gate <subcommand> --help` from the
> binary itself. The blocks below mirror what `--help` prints.

## Contents

1. [Install](#install)
2. [Run an agent](#run-an-agent)
3. [Other ways to run](#other-ways-to-run)
4. [Flag reference](#flag-reference)
5. [Where things live on disk](#where-things-live-on-disk)
6. [Operational caveats](#operational-caveats)
7. [Appendix: full command list](#appendix-full-command-list)

## Install

### macOS — Homebrew

```bash
brew tap WZ/tap
brew install agent-gate
```

Upgrade later with `brew upgrade agent-gate`. The tap formula tracks
every `vX.Y.Z` tag automatically.

### Linux / Windows / macOS without Homebrew

Grab the archive for your platform from the
[latest release](https://github.com/WZ/agent-gate/releases/latest), extract,
and move the binary onto your `PATH`:

| Platform | Archive |
|---|---|
| macOS Apple Silicon | `agent-gate_<ver>_darwin_arm64.tar.gz` |
| macOS Intel | `agent-gate_<ver>_darwin_x86_64.tar.gz` |
| Linux arm64 | `agent-gate_<ver>_linux_arm64.tar.gz` |
| Linux x86_64 | `agent-gate_<ver>_linux_x86_64.tar.gz` |
| Windows x86_64 | `agent-gate_<ver>_windows_x86_64.zip` (permissive capture today; airtight is Plan 4) |

On macOS, the first run may need `xattr -d com.apple.quarantine ./agent-gate` to clear Gatekeeper.

### Build from source

```bash
git clone https://github.com/WZ/agent-gate.git
cd agent-gate
go build -o agent-gate ./cmd/agent-gate
sudo mv agent-gate /usr/local/bin/
```

For a build with version metadata baked in (matching goreleaser):

```bash
VERSION=$(git describe --tags --always)
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o agent-gate ./cmd/agent-gate
```

> `go install agent-gate/cmd/agent-gate@latest` does **not** work — the
> module path in `go.mod` is the bare name `agent-gate`, so the toolchain
> can't fetch it from a remote. Use the binary download or `go build`
> from a clone.

### One-time bootstrap

After installing, run this once on each machine:

```bash
agent-gate init
```

Walks a short interactive wizard that:

- writes `~/.config/agent-gate/config.toml`
- mints a local CA
- detects which agents you have (`claude`, `codex`, `aider`, `opencode`)
  and seeds their upstream hosts into your allowlist
- installs the CA into your OS trust store (Keychain on macOS,
  ca-certificates + Firefox NSS on Linux, wincrypt on Windows)

For headless / CI machines:

```bash
agent-gate init --non-interactive --allow-host api.anthropic.com --install-cert=false
agent-gate cert install   # run later from a TTY (sudo prompt)
```

To validate the install at any time: `agent-gate doctor`.

## Run an agent

```bash
agent-gate run -- claude
```

That's it. agent-gate spawns the proxy, the dashboard, and a per-OS
network jail around `claude`. While the agent runs, open
<http://127.0.0.1:7878> — sessions, events, and flags appear live.
When the agent exits, agent-gate tears everything down.

Substitute `codex`, `aider`, `opencode`, or any HTTPS-using command
for `claude`.

> If your shell aliases the agent name (fish/zsh aliases aren't visible
> to `exec`), wrap through your shell so the alias resolves:
> `agent-gate run -- fish -ic 'claude'`.

### Want the dashboard always on?

Run it standalone in its own terminal:

```bash
agent-gate dashboard
```

Subsequent `agent-gate run` invocations will detect and reuse it
instead of starting a second one. Captures from every `run` flow into
the same store the dashboard is reading from, so you see them live.

## Other ways to run

### Ad-hoc captures (curl, scripts, existing daemons)

Run the proxy and dashboard in separate terminals and point any
HTTP client at the proxy via env vars:

```bash
# terminal 1
agent-gate proxy --capture-mode permissive

# terminal 2
agent-gate dashboard

# any other terminal
HTTPS_PROXY=http://127.0.0.1:8888 \
HTTP_PROXY=http://127.0.0.1:8888 \
NO_PROXY="" \
  curl https://api.anthropic.com/v1/messages \
       -H "x-api-key: $ANTHROPIC_API_KEY" \
       -H "anthropic-version: 2023-06-01" \
       -H "content-type: application/json" \
       -d '{"model":"claude-opus-4-7","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}'
```

No kernel jail in this mode — the client must honor `HTTPS_PROXY`.

### Self-hosted Anthropic-compatible endpoint

If `ANTHROPIC_BASE_URL` points at a self-hosted endpoint with a cert
that isn't in your OS trust store, fetch the cert and pass it via
`--upstream-ca`:

```bash
echo | openssl s_client -connect your-endpoint.example:443 -servername your-endpoint.example 2>/dev/null \
  | openssl x509 -outform PEM > /tmp/upstream.pem

ANTHROPIC_BASE_URL=https://your-endpoint.example \
ANTHROPIC_API_KEY=$YOUR_KEY \
  agent-gate run --upstream-ca /tmp/upstream.pem -- claude
```

If the upstream cert is structurally broken (no SANs, etc.), Go's TLS
stack rejects it regardless of trust. `--upstream-insecure-skip-verify`
is the last-resort lever; captures still happen but upstream identity
is unverified.

## Flag reference

### `agent-gate run`

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

**Picking a mode:**

| Situation | Use |
|---|---|
| You just want to capture your agent's traffic | `--mode airtight` (default — no flag needed) |
| CI run where partial capture is worse than no run | `--mode airtight-strict` |
| Platform without jail support, or a one-off you trust | `--mode permissive` |

**When to reach for the advanced flags:**

- **`--upstream-ca`** — only when the proxy needs to talk to a self-signed upstream whose cert isn't in your OS trust store. Common case: a self-hosted `ANTHROPIC_BASE_URL` or an internal LiteLLM-style gateway.
- **`--upstream-insecure-skip-verify`** — last resort when the upstream cert is structurally non-compliant (missing SANs, etc.) and `--upstream-ca` can't fix it. Captures still happen; upstream identity is unverified. Prints a `⚠ upstream TLS verification DISABLED` warning each run.
- **`--hijack-host`** — for non-pinned WebSocket upstreams where you want frame bodies. claude / codex / aider don't need it; their traffic is captured by default. codex on `chatgpt.com` pins TLS on its WS transport, so the flag is ineffective there — codex's HTTP fallback is captured without it.

### `agent-gate init`

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

### `agent-gate doctor`

```
agent-gate doctor [flags]

Flags:
  --auto-repair safe|aggressive    safe: filesystem fixes only;
                                   aggressive: also retry trust-store install
  --json                           machine-readable output
  --config PATH                    path to config.toml
```

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

## Appendix: full command list

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
