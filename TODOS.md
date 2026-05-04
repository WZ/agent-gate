# TODOs

Tracked plans for the next two release cuts. Each lands as its own PR
behind a `vX.Y.Z-<slug>` tag (same cadence as Plans 1–3).

## Recently shipped

- [x] Global Explore page: per-event PII counts indexed at capture time,
  filtered by kind/time/host, full-body free-text search.

## Plan 4 — Windows airtight runtime

**Target:** `v0.4.0-windows-airtight`
**Why:** Plan 3 ships Windows scaffolded only. `agent-gate run` on Windows
falls back to permissive with a clear "pending Plan 4" message. This plan
fills in the runtime path so Windows reaches feature parity with macOS and
Linux.

What's in:

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

## Plan 5 — Multi-vendor parser support

**Target:** `v0.5.0-multi-vendor`
**Why:** Today's smart parser only decodes Anthropic Messages.
agent-gate works for any agent (OpenAI clients, OpenClaw, Aider, custom
MCP-only flows) at the proxy + jail + audit layer, but the dashboard
shows non-Anthropic events as "generic HTTP" instead of "GPT-4o, 1200
input tokens, 3 tool calls."

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
