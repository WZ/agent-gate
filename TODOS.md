# TODOs

Tracked plans for the next release cuts. Each lands as its own PR
behind a `vX.Y.Z-<slug>` tag.

The order below reflects current sequencing. Plan 5 is the active next
target; Plan 4 is deferred until ground-truth conditions exist (see
"Why deferred" below).

## Plan 5 — Multi-vendor parser support

**Target:** `v0.5.0-multi-vendor`
**Status:** Investigation active — fixture capture in progress.
**Why now:** Today's smart parser only decodes Anthropic Messages.
agent-gate works for any agent (OpenAI clients, OpenClaw, Aider, custom
MCP-only flows) at the proxy + jail + audit layer, but the dashboard
shows non-Anthropic events as "generic HTTP" instead of "GPT-4o, 1200
input tokens, 3 tool calls." Plan 6 just shipped agent detection for
codex / aider / opencode — without Plan 5, those agents' flows decode
as opaque HTTP and the dashboard adds little value over `tcpdump`.

What's in:

- New `ParsedEvent.Kind` values for OpenAI Chat Completions, OpenAI
  Responses, Anthropic Bedrock, Vertex `generateContent`.
- Parser registry pattern: each vendor is one file
  (`internal/parser/<vendor>.go`) with a `Match` predicate + a `Parse`
  function. Adding a sixth vendor is a single-file PR.
- Shared "AI call summary" partial in the dashboard that renders model +
  token counts + tool-call summary for any Kind that produces them. No
  per-vendor template explosion.
- Test fixtures captured from real exchanges per vendor; redacted;
  committed under `testdata/flows/<vendor>/`.
- Allowlist seed extended to the major vendor hostnames so first-run
  experience doesn't spam `host_not_allowlisted` flags.
- README "Multi-vendor support" section.

Estimated effort: 2-3 days. Most time goes to capturing clean fixtures
(each vendor has its own auth + request shape quirks); the parser code
itself mirrors the existing Anthropic branch.

## Plan 4 — Windows airtight runtime (DEFERRED)

**Target:** `v0.4.0-windows-airtight`
**Status:** Deferred until: (a) a Windows iteration loop is available
locally, (b) Plans 5 + 7 have shipped (capability gaps suppress adoption
more than Windows-parity gaps for this user base), (c) demand or
capacity exists. Not abandoned — the v0.3.0 WFP scaffolding stays in
`init` as the foundation.
**Why deferred:** Plan 3 ships Windows scaffolded only. `agent-gate run`
on Windows falls back to permissive with a clear "pending Plan 4"
message. The user has no Windows iteration loop, target agents
(claude/codex/opencode/aider/cursor) are mostly Mac/Linux, and Plan 5's
multi-vendor support has wider user-surface impact for the same effort.

What's in (when picked up):

- Real `spawnAirtight` for Windows: Job Object + WFP per-exe filters + the
  CREATE_SUSPENDED → AssignProcessToJobObject → ResumeThread dance.
- `IoCompletionPort` listener for `JOB_OBJECT_MSG_NEW_PROCESS` — adds
  filters for descendants on the fly (Path B from the Plan 3 spike,
  since WFP has no Job-Object-handle condition).
- Per-user sublayer DACL grant (`FwpmSubLayerSetSecurityInfoByKey0`) so
  `agent-gate run` doesn't need elevation after `agent-gate init`.
- Real Windows isolation tests; `internal/launcher/sandbox_windows_test.go`
  stops being a skip-only file.
- README + CLAUDE.md updates removing "pending Plan 4" caveats.

Estimated effort: 3-5 days. Mostly Win32 plumbing without good runtime
feedback (no Windows machine in our usual loop, slow CI iteration).

## Completed

- **Plan B (explore-page)** — global `/explore` page + capture-time PII indexing.
  Filter chips (PII kind / time / host), free-text body+url+host search with
  snippet highlighting, pagination, per-row PII chip strip. New
  `agent-gate reindex` CLI command + first-launch auto-reindex on the
  dashboard subcommand. Plan A (PII coverage expansion) shipped as
  `v0.3.0-pii-coverage`.
- **Plan 6** — `v0.6.0-init-umbrella` (PR #6, merged 2026-05-04). One-command
  onboarding: `agent-gate init` mints CA + detects agents (claude/codex/aider/
  opencode via PATH and env vars with strict IDN homograph rejection) +
  interactive allowlist seed + cross-platform truststore install + commented
  config.toml. New `agent-gate doctor` validates installs with safe/aggressive
  repair. Auto-seed bug fix end-to-end: `Allowlist.Remove()` +
  `denylist.Remove()` + `passthrough.Remove()` + `POST /api/untrust` +
  dashboard Untrust button. Help topic pages for the three-list mental model.
  `--help` command grouping. Pre-existing Windows CA-mode bug fixed (CA load
  path now skips 0o600 assertion since Windows ignores file modes).
- **Plan 3** — `v0.3.0-airtight-launcher` — macOS + Linux runtime path.
  Windows scaffolded only.
- **Plan 2** — `v0.2.0-policy-dashboard` — policy rules + dismissals + dashboard.
- **Plan 1** — `v0.1.0-mvp-backbone` — proxy + parser + store + CLI scaffold.

## How to pick up a plan

The standard arc for non-trivial work in this repo:

1. Brainstorm with the user (CEO / eng / design / DX reviews if useful).
2. Write a design spec (under `docs/superpowers/specs/`, gitignored).
3. Write an implementation plan (under `docs/superpowers/plans/`,
   gitignored), task-by-task with code samples.
4. Execute via subagent-driven development on a branch off main, in a
   `.worktrees/plan<N>-<slug>/` worktree.
5. Open a PR; CI must be green; rebase onto main if main moved.
6. Tag `vX.Y.Z-<slug>` after merge.

See `CLAUDE.md` for the development conventions and `README.md` for
the current user-facing surface.
