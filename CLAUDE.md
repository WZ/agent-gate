# CLAUDE.md — agent-gate

Project memory for AI assistants working on this repo. Read this before
making non-trivial changes.

## What this is

`agent-gate` is a personal, single-user audit gate for Claude Code's outbound
HTTPS. It TLS-intercepts every request the agent (and its subprocesses) makes,
parses + flags + persists the flow, and serves a local web dashboard for
review. It does **not** ship as a service, ship to a backend, or run as
multi-tenant. Everything is loopback, single-user, local disk.

Three subsystems share one binary:

```
┌──────────────────────── agent-gate ────────────────────────┐
│                                                            │
│   launcher  ──spawn──▶  agent (claude, curl, gh, ...)      │
│   (sandbox/jail)              │                            │
│                               │ all egress forced here     │
│                               ▼                            │
│   proxy :8888  ──parse──▶ policy ──▶ store (jsonl+sqlite)  │
│                                            │               │
│   dashboard :7878 ◀────────────────────────┘               │
└────────────────────────────────────────────────────────────┘
```

The launcher physically forces egress through the proxy via a per-platform
network jail (macOS sandbox-exec, Linux user namespace, Windows scaffolded for
Plan 4). The proxy is a TLS-MITM forward proxy. The dashboard is read-only
against the store.

## Critical rules — never break these

- **Audit-log completeness.** Every captured flow MUST land in the JSONL +
  SQLite store. The proxy emits on a buffered channel; if the consumer falls
  behind, the proxy slows down (and upstream may time out). **Drop-on-full is
  a correctness bug, not a knob.** See `internal/proxy/proxy.go`.
- **Fail-closed posture.** If the proxy crashes during `agent-gate run`, the
  agent's network goes dark. The supervisor's teardown path may not silently
  swallow this; the child must die when the proxy dies. See
  `internal/launcher/supervisor.go`.
- **Loopback only.** The proxy refuses to bind a non-loopback address; same
  for the dashboard. See `isLoopbackAddr` in `cmd/agent-gate/proxy_cmd.go`
  and `isLoopback` in `internal/dashboard/server.go`. Never relax this.
- **File modes.** CA private key + JSONL + dismissals.json must be 0600;
  config + data dirs must be 0700. The CA package refuses to start if
  `key.pem` is wider than 0600 — gated on `runtime.GOOS != "windows"` since
  Windows ignores unix mode bits. macOS + Linux enforce; Windows skips both
  in the production check (`internal/ca/ca.go`) and the unit test
  (`internal/ca/ca_test.go`).
- **No secrets in commits.** `git add` files by name, never `git add -A` or
  `git add .`. The user's data dir contains real captured prompts and
  responses; an accidental `add -A` would commit them.
- **Don't push without permission.** Local merge + tag is fine; pushing to
  `origin` always needs explicit user authorization (per the user's global
  CLAUDE.md).
- **Always execute in a git worktree.** Every code change, test run with
  side effects, and binary build happens inside `.worktrees/<branch>/`,
  never in the main checkout. The main checkout is reserved for reading,
  git operations, and merging PRs. The worktree is created BEFORE work
  starts — never skip it "just for a small change" and never infer it
  later. When invoking subagents to execute a plan, instruct them to cd
  into the worktree as their first step.

## Three host policy lists, with priority

agent-gate has three file-backed host lists. They live in
`~/.config/agent-gate/`:

| File | Effect | Mutated via |
|---|---|---|
| `allowlist.txt` | host is OK; suppresses `host_not_allowlisted` flag, lets through under `--enforce-allowlist` | dashboard **Trust** / **Untrust** buttons → `POST /api/trust`, `POST /api/untrust`; or `agent-gate init --allow-host HOST` |
| `denylist.txt` | proxy returns synthetic 403 to agent; never contacts upstream | dashboard **Block this host** button → `POST /api/block` |
| `passthrough.txt` | proxy tunnels TCP raw, no TLS interception (for cert-pinned upstreams like `mcp-proxy.anthropic.com`) | dashboard **Passthrough (no MITM)** button → `POST /api/passthrough` |

The runtime never mutates these files. Only `init`, the dashboard, or a
human editor with `$EDITOR` may write to them. (See Plan 6's auto-seed
removal in `internal/runtime/runtime.go`.)

**Resolution order in `mitmConnect` + `HostGuard`:**

```
denylist hit  → 403 (always wins)
passthrough hit (and not denylisted) → raw TCP tunnel
enforce mode + not allowlisted → 403
default → MITM, decrypt, capture, forward
```

Add new list semantics here, not as ad-hoc proxy logic.

## Architecture: where to find what

```
cmd/agent-gate/         CLI entry; one file per subcommand
internal/runtime/       Shared startup (Common); paths.go (XDG-aware); lockfile.go
internal/launcher/      Cross-platform supervisor + per-OS jail (build tags)
internal/proxy/         goproxy-based TLS-intercepting forward proxy
internal/parser/        RawFlow → ParsedEvent (Anthropic-aware + generic)
internal/policy/        Rule engine + 8 built-in rules
internal/store/         JSONL writer + SQLite index + Body() reader
internal/dashboard/     HTTP server, HTMX templates, embedded assets
internal/{allowlist,denylist,passthrough}/    file-backed host lists (Add + Remove)
internal/dismissals/    flag dismissals (JSON file)
internal/redactor/      secret-mask render layer
internal/pii/           PII detection (used by dashboard for body coloring)
internal/secrets/       single regex set (shared by policy + redactor)
internal/ca/            local CA mint + leaf signing; truststore.go (cross-platform install)
internal/agentdetect/   detect installed agents via $PATH + env vars (IDN-safe)
internal/initwizard/    `agent-gate init` orchestrator + huh-backed Prompter
internal/doctor/        `agent-gate doctor` checks + repair + output (human + JSON)
internal/idgen/         ULID
internal/types/         shared structs (RawFlow, ParsedEvent, StoredEvent, Flag)
internal/e2e/           end-to-end tests that build the binary
```

Per-platform code uses `_darwin.go` / `_linux.go` / `_windows.go` build tags.
Cross-cutting code is unsuffixed. The `_other.go` files cover
`!darwin && !linux && !windows` so it still cross-compiles.

## CLI surface (current)

```
agent-gate init [flags]              one-command bootstrap (CA + agent detection + truststore install)
                                       flags: --non-interactive, --install-cert=auto|true|false,
                                       --skip-cert-install, --regenerate-ca, --allow-host HOST
                                       (repeatable), --force, --dry-run, --print-config, --config PATH
agent-gate doctor [flags]            validate the install; suggest or apply repairs
                                       flags: --auto-repair=safe|aggressive, --json, --config PATH
agent-gate run -- <cmd>              launch with airtight network capture
agent-gate proxy                     standalone proxy (foreground)
agent-gate dashboard                 standalone dashboard (foreground)
agent-gate tail                      live-follow events
agent-gate stop                      SIGTERM a stuck `agent-gate run`
agent-gate cert install              install local CA into all trust stores (Keychain / ca-cert / wincrypt / Firefox NSS)
agent-gate cert uninstall            remove local CA from all trust stores
agent-gate cert path                 print the CA cert path
agent-gate uninstall                 (Windows) remove WFP provider/sublayer
agent-gate version                   print version, commit, build date
agent-gate help allowlist|denylist|passthrough     explain the three-list policy model
```

When adding a subcommand, register it in `cmd/agent-gate/main.go`. Keep all
shared startup in `internal/runtime` so subcommands stay thin.

## Development commands

```bash
# build the binary into /tmp (don't pollute the worktree)
go build -o /tmp/agent-gate ./cmd/agent-gate

# tests
go test ./...                                # all packages
go test -race ./...                          # race detector
go vet ./...
gofmt -l .                                   # MUST be empty before commit

# cross-compile (we ship pure-Go for these; CI verifies)
GOOS=linux   go build ./...
GOOS=windows go build ./...
GOOS=darwin  go build ./...

# run a Linux-only test from darwin (it'll skip cleanly)
go test -run TestSandboxLinux ./internal/launcher/... -v
```

CI matrix is at `.github/workflows/ci.yml` — Go 1.25 across
ubuntu/macos/windows, plus a `vet-race-fmt` job (Linux) that runs `go vet`,
`go test -race`, and `gofmt -l .`.

## Plan-based development cadence

Work lands in **plans**, each plan a self-contained branch + PR + tag.
Tags use the simple `vX.Y.Z` form; goreleaser publishes binaries to a
GitHub Release on every `v*` tag push.

Currently shipped (oldest → newest):

| Tag | Plan | Focus |
|---|---|---|
| (pre-launch) | Plan 1 | proxy + parser + store + CLI scaffold |
| (pre-launch) | Plan 2 | policy rules + dismissals + dashboard |
| (pre-launch) | Plan 3 | airtight launcher (macOS + Linux; Windows scaffolded) |
| (pre-launch) | Plan B (explore) | global `/explore` + capture-time PII indexing |
| (pre-launch) | Plan 6 | one-command `init` + agent detection + `doctor` + truststore install |
| (pre-launch) | Plan 7 | release automation via goreleaser |
| `v0.1.0` | (launch) | public-launch polish (badges, screenshots, SECURITY.md) |
| `v0.2.0` | Plan 5 Stream A | multi-vendor HTTP parsers (OpenAI Chat + Responses + SSE) |

Active and deferred:

| Target | Plan | Status |
|---|---|---|
| `v0.3.0` | Plan 5 Stream B | codex WebSocket capture — implementation queued |
| `v0.4.0` | Plan 4 | Windows airtight runtime — deferred until Windows iteration loop + demand exist |

See `TODOS.md` at the repo root for the publicly-tracked active +
deferred summaries.

Specs and plans for each live (gitignored) under `docs/superpowers/`. The
spec is the design doc; the plan is the bite-sized implementation list.
**Do not push or commit anything in `docs/superpowers/`** — that path is
local working material and is in `.gitignore`.

For non-trivial work, the user runs in a git worktree:
`.worktrees/<short-slug>/` off main, with a fresh `<type>/<slug>` branch
(e.g. `feat/plan5-sse-decode`, `fix/dashboard-badge-polish`). The user
runs multiple Claude agents in parallel — worktrees prevent file drift
between them.

## Adding things — patterns to follow

**A new policy rule:**
1. Implement in `internal/policy/builtin.go` as a `Rule` (matches the
   existing constructor pattern: `New<Foo>Rule(...)` if it needs deps,
   `<Foo>Rule{}` if pure).
2. Register it in `internal/runtime/runtime.go` inside `LoadCommon`'s
   `policy.NewEngine(...)` call.
3. Add a test in `internal/policy/builtin_test.go` against fixtures.
4. Update README.md's `## Built-in policy rules` table.

**A new CLI flag on `run`:**
1. Add the flag in `cmd/agent-gate/run_cmd.go`.
2. If it affects launcher behavior, add a field to `launcher.Options`
   (`internal/launcher/launcher.go`) and thread it through
   `runSupervised` in `supervisor.go`.
3. If it affects the proxy, add a field to `proxy.Options`
   (`internal/proxy/proxy.go`) and pass it from supervisor's `proxy.Run`
   calls (note: there are TWO `proxy.Run` calls — one for the host
   listener, one for the netns listener; both must be updated together).
4. Update README.md's `### Flags` block.
5. Test on all three GOOS via cross-compile.

**A new sandbox capability:**
- macOS: edit `sandboxProfileTemplate` in `internal/launcher/sandbox_darwin.go`.
  SBPL `remote ip` only accepts `localhost` or `*`, NOT bare IPs — this is a
  real footgun; verify in `sandbox_darwin_test.go`.
- Linux: edit `internal/launcher/netnshelper_linux.go` for the helper-side
  setup; `sandbox_linux.go` for supervisor-side FD-passing. Probe mode
  (port=0) must keep working — it's how feasibility detection survives
  Ubuntu 24's `apparmor_restrict_unprivileged_userns=1`.
- Windows: scaffolded only. Plan 4 will land the runtime path.

## Test coverage map

| Layer | What's covered | Gaps |
|---|---|---|
| `internal/parser` | Anthropic Messages + generic; SSE re-assembly | New API shapes need new fixtures |
| `internal/policy` | Each builtin rule fires/doesn't on synthetic + recorded events | Custom user rules (config-extensible) — schema reserved, not wired |
| `internal/redactor` | Golden tests against secrets fixtures | — |
| `internal/store` | JSONL/SQLite round-trip; reindex; concurrent append | Windows file-lock test skips |
| `internal/proxy` | TLS round-trip via `httptest.NewTLSServer`; HostGuard 403; passthrough no-MITM | — |
| `internal/launcher` | macOS sandbox-isolation tests, Linux netns tests (skip on darwin/CI), supervisor lifecycle | Windows airtight (Plan 4) |
| `internal/dashboard` | Handlers + SSE delivery via `httptest` | Frontend (Playwright) — not wired |
| `internal/e2e` | Builds the binary; runs `agent-gate run` end-to-end with real subprocess | Manual `agent-gate run -- claude` dogfood is release-validation, not a test |

When adding a feature, prefer fixture-driven tests (`testdata/`) for parsers
and policy. The whole point of the parser-as-pure-function design is that one
captured Anthropic exchange can be replayed forever.

## What we explicitly don't do

- **Block at the network layer for non-allowlisted hosts unless `enforce`
  is on.** The default posture is "audit, don't drop." Allowlist is an
  annotation, not a firewall, by default. (`--enforce-allowlist` opts into
  hard blocking.)
- **Decrypt cert-pinned upstreams.** When the agent's MCP client pins
  `mcp-proxy.anthropic.com`, agent-gate's MITM fails. We tunnel TCP raw
  via passthrough — body audit isn't possible, only connection metadata.
- **Detect filesystem exfiltration.** agent-gate audits network. If the
  agent reads `.env` and writes it somewhere on disk, we don't see it.
  Document this in the threat model; never paper over it.
- **Run as a service / agent / daemon.** Single-shot supervisor + dashboard
  per `agent-gate run`. The lockfile enforces "one instance at a time."
- **Ship to a remote backend.** Everything stays local. No telemetry, no
  upload, no cross-machine collation.

## Voice for any docs you write

User-forward, not implementation-forward. "You can now Block hosts directly
from the dashboard" beats "Added handleBlock endpoint to dashboard server."
Lead with what the user can do; the mechanism is supporting detail.

The README is the front door for human users. CLAUDE.md (this file) is the
front door for AI assistants. Don't duplicate; cross-link with prose, not
shared boilerplate.

## Skill routing

When the user's request matches an available skill, invoke it via the Skill tool. The
skill has multi-step workflows, checklists, and quality gates that produce better
results than an ad-hoc answer. When in doubt, invoke the skill. A false positive is
cheaper than a false negative.

Key routing rules:
- Product ideas, "is this worth building", brainstorming → invoke /office-hours
- Strategy, scope, "think bigger", "what should we build" → invoke /plan-ceo-review
- Architecture, "does this design make sense" → invoke /plan-eng-review
- Design system, brand, "how should this look" → invoke /design-consultation
- Design review of a plan → invoke /plan-design-review
- Developer experience of a plan → invoke /plan-devex-review
- "Review everything", full review pipeline → invoke /autoplan
- Bugs, errors, "why is this broken", "wtf", "this doesn't work" → invoke /investigate
- Test the site, find bugs, "does this work" → invoke /qa (or /qa-only for report only)
- Code review, check the diff, "look at my changes" → invoke /review
- Visual polish, design audit, "this looks off" → invoke /design-review
- Developer experience audit, try onboarding → invoke /devex-review
- Ship, deploy, create a PR, "send it" → invoke /ship
- Merge + deploy + verify → invoke /land-and-deploy
- Configure deployment → invoke /setup-deploy
- Post-deploy monitoring → invoke /canary
- Update docs after shipping → invoke /document-release
- Weekly retro, "how'd we do" → invoke /retro
- Second opinion, codex review → invoke /codex
- Safety mode, careful mode, lock it down → invoke /careful or /guard
- Restrict edits to a directory → invoke /freeze or /unfreeze
- Upgrade gstack → invoke /gstack-upgrade
- Save progress, "save my work" → invoke /context-save
- Resume, restore, "where was I" → invoke /context-restore
- Security audit, OWASP, "is this secure" → invoke /cso
- Make a PDF, document, publication → invoke /make-pdf
- Launch real browser for QA → invoke /open-gstack-browser
- Import cookies for authenticated testing → invoke /setup-browser-cookies
- Performance regression, page speed, benchmarks → invoke /benchmark
- Review what gstack has learned → invoke /learn
- Tune question sensitivity → invoke /plan-tune
- Code quality dashboard → invoke /health

Plus the project's superpowers skills: brainstorming → writing-plans →
subagent-driven-development → finishing-a-development-branch is the standard
arc for non-trivial work. Plans land as PRs; tags follow the `vX.Y.Z-<slug>`
shape (e.g., `v0.3.0-airtight-launcher`).
