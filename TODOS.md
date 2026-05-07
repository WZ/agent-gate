# TODOs

Tracked plans for the next release cuts. Each lands as its own PR
behind a `vX.Y.Z` tag and ships via goreleaser as a GitHub Release
with cross-platform binaries.

The repo is public as of v0.1.0
(https://github.com/WZ/agent-gate). Roadmap order below reflects
current sequencing. With Plan 5 Stream B shipped as v0.3.0, the only
named open plan is Plan 4 (deferred); the natural fixture-driven
follow-ups are opencode and aider captures.

## Plan 4 — Windows airtight runtime (DEFERRED)

**Target:** `v0.4.0`
**Status:** Deferred until: (a) a Windows iteration loop is available
locally, (b) demand or capacity exists. Stream B (the previous gating
condition) shipped as v0.3.0. Not abandoned — the v0.1.0 WFP
scaffolding stays in `init` as the foundation.
**Why deferred:** Plan 3 ships Windows scaffolded only. `agent-gate run`
on Windows falls back to permissive with a clear "pending Plan 4"
message. The user has no Windows iteration loop, target agents
(claude/codex/opencode/aider/cursor) are mostly Mac/Linux, and
multi-vendor + WS capture have wider user-surface impact for the same
effort.

What's in (when picked up):

- Real `spawnAirtight` for Windows: Job Object + WFP per-exe filters +
  the `CREATE_SUSPENDED → AssignProcessToJobObject → ResumeThread` dance.
- `IoCompletionPort` listener for `JOB_OBJECT_MSG_NEW_PROCESS` — adds
  filters for descendants on the fly (Path B from the Plan 3 spike,
  since WFP has no Job-Object-handle condition).
- Per-user sublayer DACL grant
  (`FwpmSubLayerSetSecurityInfoByKey0`) so `agent-gate run` doesn't
  need elevation after `agent-gate init`.
- Real Windows isolation tests; `internal/launcher/sandbox_windows_test.go`
  stops being a skip-only file.
- README + CLAUDE.md updates removing "pending Plan 4" caveats.

Estimated effort: 3-5 days. Mostly Win32 plumbing without good runtime
feedback (no Windows machine in our usual loop, slow CI iteration).

## Completed

- **v0.3.0** — Codex visibility on `chatgpt.com` (Plan 5 Stream B).
  - `HijackConnect` handler (`internal/proxy/hijack.go`) — TLS
    termination via the local CA's leaf-signer, RFC 6455 frame pumps
    with per-message re-assembly + 16 MB cap + control-frame
    persistence + parse-error degradation. Non-WS HTTP forwards
    through the same handler so `--hijack-host chatgpt.com` doesn't
    break codex's setup endpoints.
  - `--hijack-host HOST` repeatable CLI flag plumbed through
    `launcher.Options.HijackHosts` and threaded through both
    `proxy.Run` callsites (host listener + Linux netns helper).
  - `openai_responses` parser extended to decode `Content-Encoding:
    zstd` request bodies and to fall back to the `Session_id` request
    header for SessionID. Same parser now matches both
    `api.openai.com/v1/responses` and
    `chatgpt.com/backend-api/codex/responses`.
  - `ws_pinned_upstream` info-level policy rule on every WS upgrade
    (status 101 + Upgrade: websocket). Explains an empty 101 in the
    dashboard so reviewers don't assume a parser bug.
  - agentdetect codex defaults seed both `chatgpt.com` (OAuth path)
    and `api.openai.com` (API-key path).
  - `doctor CheckCodexWebSocketPinning` advisory result fires when
    codex is on PATH so users know the limitation before they go
    looking for missing bodies.
  - Two new fixtures: `codex_responses_post_streaming.json` (synthetic
    zstd Responses-API call) and `codex_ws_upgrade_pinned.json`
    (header-only, real wire shape, scrubbed). 2026-05-07 follow-up
    section in `testdata/flows/codex/NOTES.md` documents the pinning
    + HTTP fallback finding.
  - Headline finding from a real capture: codex 0.128.0 client-pins
    TLS on its WebSocket transport (`Attack attempt detected` is
    client-side, not server-side), so WS body capture is permanently
    blocked. But codex falls back to plain `POST
    /backend-api/codex/responses` after WS retries exhaust, and that
    path captures cleanly. The hijack code ships as infrastructure
    for future non-pinning agents; the v0.3.0 ship hangs on the HTTP
    fallback decode.
  - Dropped from the original spec because no captured frames exist:
    `chatgpt_realtime` WS frame parser, dashboard child-events
    endpoint, `oversize_websocket` / `ws_parse_error` policy rules.
- **v0.2.0** — Multi-vendor parser support (Plan 5 Stream A). Decode
  every OpenAI-compatible exchange — vanilla `api.openai.com`, Azure,
  LiteLLM gateways, vLLM, DeepSeek, Mistral, Groq, Together, Ollama —
  via path + body-shape match (no per-vendor hostname code).
  - `openai_chat` parser for `/chat/completions`. Surfaces model,
    message count, usage tokens, tool calls (with
    `function.arguments` double-decoded), tool results,
    multimodal content.
  - `openai_responses` parser for `/responses`. Same coverage but
    for the newer API surface, including `previous_response_id`-aware
    SessionID and the `function_call` / `function_call_output`
    input/output shape.
  - SSE decoders for both. Chat assembles tool calls across delta
    chunks keyed on `(choice.index, tool_call.index)` so multi-choice
    `n>1` streams don't collide; Responses pulls the full final
    response from the `response.completed` event.
  - Real captured fixtures from an OpenAI-compatible gateway, scrubbed.
  - Codex review-driven hardening on the SSE side: per-choice bucket
    keys + negative-index guards.
- **v0.1.0** — Public launch.
  - Repo flipped public 2026-05-06 with Apache 2.0 license, README
    hero banner, SECURITY.md, repo description + topics. Sanitized
    git history (no internal design docs, no captured-data leaks).
  - Dashboard polish: friendlier flag badges ("Host not allowlisted"
    instead of `host_not_allowlisted`), severity-correct badge colors
    on session-detail (was hardcoded grey), inline PII chips on
    Explore so events stay one row.
- **Plan 7** — Release automation via goreleaser. Cross-platform
  binaries build and publish to GitHub Releases on every `v*` tag push
  (darwin/linux/windows × arm64/amd64). README links to the latest
  release shield.
- **Plan B (explore-page)** — Global `/explore` page + capture-time PII
  indexing. Filter chips (PII kind / time / host), free-text body+url
  +host search with snippet highlighting, pagination, per-row PII chip
  strip. New `agent-gate reindex` CLI command + first-launch
  auto-reindex on the dashboard subcommand. Plan A (PII coverage
  expansion) shipped pre-launch and rolled into v0.1.0.
- **Plan 6** — One-command onboarding via `agent-gate init`. Mints CA
  + detects agents (claude/codex/aider/opencode via PATH and env vars
  with strict IDN homograph rejection) + interactive allowlist seed
  + cross-platform truststore install + commented config.toml. New
  `agent-gate doctor` validates installs with safe/aggressive repair.
  Auto-seed bug fix end-to-end: `Allowlist.Remove()` +
  `denylist.Remove()` + `passthrough.Remove()` + `POST /api/untrust`
  + dashboard Untrust button. Help topic pages for the three-list
  mental model.
- **Plan 3** — Airtight launcher: macOS + Linux runtime. Windows
  scaffolded only.
- **Plan 2** — Policy rules + dismissals + dashboard.
- **Plan 1** — Proxy + parser + store + CLI scaffold.

## How to pick up a plan

The standard arc for non-trivial work in this repo:

1. Brainstorm with the user (CEO / eng / design / DX reviews if useful).
2. Write a design spec (under `docs/superpowers/specs/`, gitignored).
3. Write an implementation plan (under `docs/superpowers/plans/`,
   gitignored), task-by-task with code samples.
4. Execute via subagent-driven development on a branch off main, in a
   `.worktrees/<short-slug>/` worktree.
5. Open a PR; CI (test on ubuntu/macos/windows + vet-race-fmt) must be
   green; rebase onto main if main moved.
6. Tag `vX.Y.Z` after merge — goreleaser publishes binaries to
   GitHub Releases automatically on tag push.

See `CLAUDE.md` for the development conventions and `README.md` for
the current user-facing surface.
