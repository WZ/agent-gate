<p align="center">
  <img src="docs/img/hero-banner.svg" alt="agent-gate" width="900"/>
</p>

<p align="center">
  <a href="https://github.com/WZ/agent-gate/releases/latest"><img src="https://img.shields.io/github/v/release/WZ/agent-gate?style=for-the-badge&color=0e7c5a&include_prereleases&label=release" alt="Latest release"/></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg?style=for-the-badge" alt="License: Apache 2.0"/></a>
  <img src="https://img.shields.io/badge/go-%E2%89%A51.25-00ADD8?style=for-the-badge" alt="Go >= 1.25"/>
  <img src="https://img.shields.io/badge/airtight-macOS%20%C2%B7%20Linux-0e7c5a?style=for-the-badge" alt="Airtight on macOS and Linux"/>
  <img src="https://img.shields.io/badge/Windows-permissive%20today-f59e0b?style=for-the-badge" alt="Windows permissive today"/>
</p>

<p align="center">
  <strong>Single-user audit gate for AI agents that talk HTTP.</strong> Whether you run Claude Code, Codex, Aider, OpenCode, an MCP client, or a plain <code>curl</code> in a script, agent-gate sits on your laptop, captures outbound HTTPS through a local proxy, flags + persists each captured flow, and lets you review it all in a local web dashboard. Airtight capture is supported on macOS and Linux today; Windows supports permissive proxy capture while the airtight runtime is in Plan 4. No backend, no telemetry, no cross-machine collation — everything stays on your disk.
</p>

<p align="center">
  <img src="docs/img/screenshots/01-operations.png" alt="Operations dashboard — session catalog, captured-event metrics, risk feed" width="900"/>
</p>

<p align="center"><em>The Operations dashboard — session catalog with flag counts, captured-event metrics, and a live risk feed.</em></p>

## Support Matrix

agent-gate support has three separate layers:

- **Airtight capture** means `agent-gate run -- <cmd>` puts the agent in an OS-level network jail, so subprocesses and tools that ignore `HTTPS_PROXY` still cannot bypass the local proxy.
- **Permissive capture** means the proxy, dashboard, policy engine, and store work, but the target must honor `HTTP_PROXY` / `HTTPS_PROXY`.
- **Rich parsing** means the dashboard extracts AI-specific fields such as model, token counts, tool calls, tool results, streamed message content, or protocol-specific inventory counts. Today that rich path applies to Anthropic Messages traffic on `api.anthropic.com`, OpenAI-compatible Chat Completions and Responses HTTP calls, and selected Codex/ChatGPT backend setup endpoints on `chatgpt.com`; everything else still gets audited as generic HTTP.

## Features

- **Airtight launcher** — per-OS network jail forces every byte of egress through the local proxy. Subprocesses inherit it; tools that ignore `HTTPS_PROXY` get kernel-level deny.
- **TLS-MITM proxy with passthrough escape hatch** — decrypts and parses every request. Cert-pinned hosts get raw TCP tunneling so MITM-rejecters still work.
- **Three-list policy model** — `allowlist.txt`, `denylist.txt`, `passthrough.txt`. Mutated only by `init`, the dashboard, or your editor — never by the runtime.
- **Nine built-in policy rules** — host not allowlisted, secrets in body, env leakage, oversize, parse errors, more. Per-flag dismiss with reason.
- **PII detection across the wire** — SSN, cards, email, phone, JWT, UUID, IPv4, etc. The Explore page colors body text by kind.
- **Parser registry** — Anthropic Messages SSE reassembled into single events; OpenAI Chat Completions and Responses HTTP surface model, tokens, tool calls. Generic HTTP fallback for everything else.
- **One-command bootstrap** — `agent-gate init` writes config, mints a CA, detects your agents, seeds their hosts, installs the CA into your trust store.
- **`doctor` validate-and-repair** — one line per check across config, CA, ports, lockfile, agents, trust stores. `--auto-repair` for filesystem fixes.
- **JSONL + SQLite store** — JSONL is the source of truth; SQLite is an index. `agent-gate reindex` rebuilds the index whenever.
- **Single binary, pure Go** — no CGO. Cross-compiles to darwin/linux/windows × amd64/arm64.

## Quick Start

```bash
brew tap WZ/tap
brew install agent-gate
agent-gate init               # one-time bootstrap: config + CA + agent detection + cert install
agent-gate run -- claude      # launch your agent through the gate
```

Then open <http://127.0.0.1:7878> to review what your agent is doing.

On macOS and supported Linux hosts this runs in airtight mode by default; Windows currently falls back to permissive capture. To validate the install at any time: `agent-gate doctor`.

For binary download, build-from-source, headless/CI install, and every flag the binary takes, see the [runbook](docs/RUNBOOK.md).

### Platforms

| Platform | Supported today | Current behavior | TODO |
|---|---|---|---|
| **macOS** | Yes | <ul><li>Airtight `agent-gate run` via `sandbox-exec`</li><li>Proxy, dashboard, `init`, `doctor`, cert install all supported</li></ul> | <ul><li>✅ Platform parity complete</li></ul> |
| **Linux** | Yes, when unprivileged user namespaces are allowed | <ul><li>Airtight `agent-gate run` via user + network namespace</li><li>Hardened hosts fall back to permissive unless `--mode=airtight-strict` is set</li></ul> | <ul><li>Better hardened-distro story if demand justifies it</li></ul> |
| **Windows** | Partial | <ul><li>Binaries, `init`, `doctor`, cert install, proxy, dashboard, permissive capture all work</li><li>Airtight `agent-gate run` not wired yet — falls back to permissive with a clear message</li></ul> | <ul><li>**Plan 4:** Job Object + WFP per-exe filters + completion-port listener for descendants</li></ul> |

### Agents and API Parsing

| Agent / client | Supported today | What is rich today | TODO |
|---|---|---|---|
| **Claude Code** (`claude`) | Yes, primary path | <ul><li>`init` detects `claude` + seeds `api.anthropic.com`</li><li>Anthropic Messages parser: SSE streams, tool calls, tool results, system prompts, token usage</li></ul> | <ul><li>✅ None for the local CLI</li><li>Bedrock / Vertex transports would land as their own fixture-driven parsers when a real capture turns up</li></ul> |
| **Codex** (`codex`) | Capture yes (via HTTP fallback) | <ul><li>`init` detects `codex` + seeds `chatgpt.com` (OAuth) and `api.openai.com` (API-key)</li><li>Setup endpoints on `chatgpt.com/backend-api/...` parsed (model catalog, MCP tool inventory, etc.)</li><li>Model conversation captured via the HTTP fallback `POST /backend-api/codex/responses` (zstd request, SSE response) — same Responses parser as `api.openai.com`</li><li>WebSocket transport pins TLS in 0.128.0 → empty 101 + `ws_pinned_upstream` flag</li></ul> | <ul><li>✅ None — HTTP fallback covers the model conversation</li><li>WS hijack handler ships as infrastructure for future non-pinning agents</li></ul> |
| **Aider** (`aider`) | Capture yes | <ul><li>`init` detects `aider` + seeds `api.anthropic.com` and `api.openai.com`</li><li>Anthropic Messages + OpenAI Chat Completions / Responses get rich parsing (model, tokens, tool calls, tool results)</li><li>Other providers parse as generic HTTP</li></ul> | <ul><li>More providers as fixtures land</li></ul> |
| **OpenCode** (`opencode`) | Capture yes | <ul><li>`init` detects `opencode` + seeds `api.anthropic.com`</li><li>Anthropic Messages + OpenAI Chat Completions / Responses get rich parsing</li><li>Other configured providers may need manual trust + parse as generic HTTP</li></ul> | <ul><li>Plan 5's parser registry makes new vendor decoders one-file additions</li></ul> |
| **OpenClaw / Hermes Agent** | Manual capture; not first-class today | <ul><li>Launch via `agent-gate run -- <cmd>` or point at the proxy manually</li><li>`init` does not detect their binaries</li><li>Anthropic + OpenAI HTTP calls get rich parsing; OpenRouter / custom providers parse as generic</li></ul> | <ul><li>Binary detection</li><li>Provider host seeding</li><li>More vendor fixtures</li><li>Codex/ChatGPT WebSocket validation</li></ul> |
| **curl, scripts, MCP clients, custom agents** | Capture yes | <ul><li>Any HTTPS client works through airtight mode or `HTTPS_PROXY`</li><li>Anthropic + OpenAI HTTP calls get rich parsing; other hosts parse as generic HTTP</li></ul> | <ul><li>First-class decoders as real fixtures are captured</li></ul> |

## How It Works

Three subsystems share one binary. On macOS and supported Linux hosts, the launcher spawns the target inside a per-OS network jail. The proxy decrypts each HTTPS request, runs it through a parser → policy → store pipeline. The dashboard reads from the store. Everything is loopback. Nothing leaves your disk.

<p align="center">
  <img src="docs/img/system-overview.svg" alt="agent-gate system overview" width="900"/>
</p>

The audit-log is non-negotiable: if the storage consumer falls behind, the proxy slows down (and upstream may time out). Drop-on-full would be a correctness bug, not a knob — every captured flow lands on disk, period.

## Operations: what work did the agent do?

The default landing page (`/`) is the **session catalog** — agent sessions grouped by host, with event counts, latest activity, and a flag rollup. Filter by host or time window. Click into any session for the timeline.

Right column is the **risk feed**: every active flag code, severity, and hit count. One glance tells you if anything new fired.

<p align="center">
  <img src="docs/img/screenshots/04-session-flags.png" alt="Session detail — every event in chronological order, with flags inline" width="900"/>
</p>

<p align="center"><em>Session detail — 22 hits to <code>downloads.claude.ai</code>, all flagged <code>host_not_allowlisted</code> because Claude Code auto-updates from a host nobody trusted yet. Decide once, trust the host, move on.</em></p>

## Explore: what data slipped through?

Every captured event in one searchable, filterable table at `/explore`. Filter by PII kind (SSN, credit card, email, JWT, UUID, …), time window, or host. Substring-search request bodies, URLs, and hosts. Host chips show how many events per host so you can scope quickly without typing.

<p align="center">
  <img src="docs/img/screenshots/02-explore.png" alt="Explore page — host chips, PII filter pills, time-window pills, captured-traffic table" width="900"/>
</p>

<p align="center"><em>254 events across 5 hosts. Each row's PII chip (<code>10 UUID</code>, <code>3 EMAIL</code>, etc.) tells you what's in the body before you click.</em></p>

## Event detail: trust, block, or pass through

Click any event for the inspection page. Status, capture mode, the full request/response payload side-by-side. Credential-like values are masked by default — toggle to raw bytes when you actually need them (and the toggle itself logs a `raw_peek` event, so you can't peek silently).

Three host-policy buttons sit at the top of the page: **Trust** (allowlist), **Block** (denylist, returns 403), **Passthrough** (raw TCP for cert-pinned upstreams). Per-flag **Dismiss** writes to `dismissals.json` with a free-text reason.

<p align="center">
  <img src="docs/img/screenshots/03-event-detail.png" alt="Event detail — POST URL, status, capture mode, host policy buttons, redacted view, request/response payloads with PII coloring" width="900"/>
</p>

<p align="center"><em>A real <code>host_not_allowlisted</code> hit: the proxy returned a synthetic 403 to the agent and saved both sides for review. The body's UUIDs are highlighted by the PII coloring layer.</em></p>

## Airtight launcher

`agent-gate run -- <cmd>` spawns the target inside a per-OS network jail that physically forces all egress through the proxy. macOS and Linux work today; Windows falls back to permissive capture until Plan 4.

| Platform | Mechanism | Notes |
|---|---|---|
| **macOS** | `sandbox-exec` profile denies all `network*` ops except loopback to the proxy port | <ul><li>No installation step</li><li>Descendants inherit the sandbox automatically</li></ul> |
| **Linux** | `__netns-helper` subprocess enters an unprivileged user + network namespace, binds the proxy port inside it, passes the listener FD back via `SCM_RIGHTS` | <ul><li>Requires `kernel.unprivileged_userns_clone=1` (default on Ubuntu/Fedora)</li><li>Falls back to `--mode=permissive` on hardened distros unless `--mode=airtight-strict` is set</li></ul> |
| **Windows** | WFP provider/sublayer registered by `agent-gate init`; runtime path scaffolded for Plan 4 | <ul><li>Currently stubs to `--mode=permissive` with a clear message</li></ul> |

For the full flag reference, mode-picking guide, and run recipes, see the [runbook](docs/RUNBOOK.md#agent-gate-run).

## Threat model

agent-gate is a **network audit gate**, not a sandbox or RBAC system. It captures and audits HTTP egress; it does not isolate filesystem, IPC, or privileges.

**Defends against:**

- Tools that ignore `HTTPS_PROXY` — kernel-level network deny in airtight mode
- Subprocess descendants — the jail is inherited (sandbox profile / network namespace)

**Does NOT defend against:**

- Local IPC — UNIX sockets, named pipes, abstract sockets, shared memory are out of the proxy's view by design
- Root or admin agents — the user can lift the jail
- Filesystem reads — agent-gate sees the network, not the disk; an agent reading `.env` and writing it elsewhere on disk is invisible
- Steganographic exfiltration through allowed hosts

If your threat model needs filesystem isolation or RBAC, agent-gate alone is insufficient — pair it with OS-level sandboxing.

## Three-list policy model

Each request hits the proxy and gets routed by three plain-text host lists. Resolution order is fixed:

```
1. denylist hit                          → 403 (always wins)
2. passthrough hit (and not denylisted)  → raw TCP tunnel; no TLS interception
3. enforce mode + not allowlisted        → 403
4. default                               → MITM, decrypt, capture, forward
```

The lists live in `~/.config/agent-gate/` and are mutated only by `init`, the dashboard, or your editor — never by the runtime:

| File | Add a host when… | How to add |
|---|---|---|
| `allowlist.txt` | …it's a known-good upstream you don't want flagged | Dashboard **Trust** → `POST /api/trust`, or `agent-gate init --allow-host HOST` |
| `denylist.txt` | …you want it blocked at the proxy with a 403, no upstream contact | Dashboard **Block** → `POST /api/block` |
| `passthrough.txt` | …it pins TLS or otherwise rejects MITM (e.g. `mcp-proxy.anthropic.com`) and you still want the connection metadata audited | Dashboard **Passthrough** → `POST /api/passthrough` |

`agent-gate help allowlist|denylist|passthrough` prints the long-form explanation in your terminal.

## Built-in policy rules

Every captured event runs through nine rules. Severity drives the dashboard's risk feed and sort order — `high` floats to the top, `info` stays out of your way unless filtered.

**High** — likely worth a look:

| Code | Fires when |
|---|---|
| `host_not_allowlisted` | Request host is not in the allowlist |
| `secret_in_request` | Request body matches a credential pattern |
| `env_in_tool_result` | Tool result contains ≥3 `KEY=VALUE` lines |

**Medium** — usually benign but unusual:

| Code | Fires when |
|---|---|
| `oversized_request` | Request body > 5 MB |
| `unknown_mcp_endpoint` | Response is `text/event-stream` and host is unknown |

**Low** — noise tracking:

| Code | Fires when |
|---|---|
| `oversized_response` | Response body > 5 MB |

**Info** — context, not concern:

| Code | Fires when |
|---|---|
| `permissive_capture` | Session captured under env-only enforcement |
| `parse_error` | Parser annotated an error on the flow |
| `ws_pinned_upstream` | WebSocket upgrade succeeded (101) but the upstream client pins TLS — empty body capture is expected here (e.g. codex on `chatgpt.com`) |

Per-flag dismiss-with-reason on the dashboard writes to `dismissals.json` so the same flag doesn't keep nagging you on subsequent runs.

## What we explicitly don't do

- **Block at the network layer for non-allowlisted hosts unless `--enforce-allowlist` is on.** The default posture is *audit, don't drop*. Allowlist is an annotation, not a firewall, by default.
- **Decrypt cert-pinned upstreams.** When the agent's MCP client pins `mcp-proxy.anthropic.com`, agent-gate's MITM fails. We tunnel TCP raw via passthrough — body audit isn't possible, only connection metadata.
- **Detect filesystem exfiltration.** agent-gate audits network. If the agent reads `.env` and writes it somewhere on disk, we don't see it.
- **Run as a service / agent / daemon.** Single-shot supervisor + dashboard per `agent-gate run`. The lockfile enforces "one instance at a time."
- **Ship to a remote backend.** No telemetry, no upload, no cross-machine collation. Everything stays local.

## Development

```bash
go build -o /tmp/agent-gate ./cmd/agent-gate    # build
go test ./...                                   # unit tests
go test -race ./...                             # race detector
go vet ./...
gofmt -l .                                      # MUST be empty before commit

# cross-compile sanity
GOOS=linux   go build ./...
GOOS=windows go build ./...
GOOS=darwin  go build ./...
```

CI matrix at `.github/workflows/ci.yml` runs Go 1.25 across ubuntu / macos / windows, plus a `vet-race-fmt` job on Linux.

## License

[Apache 2.0](LICENSE)
