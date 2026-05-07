# codex (OpenAI CLI) — agent-gate capture notes

Captured: 2026-05-04
agent-gate version: 0.0.1-dev (worktree HEAD: 16b56f5)
codex version: codex-cli 0.128.0 (Node-based, `/usr/bin/env node` script)
codex auth: OAuth (`~/.codex/auth.json`, no `OPENAI_API_KEY` set)
Capture method: permissive proxy + `HTTPS_PROXY` + `NODE_EXTRA_CA_CERTS`

## Trigger

```bash
cd <agent-gate-worktree>   # codex's config trusts the agent-gate dir
HTTPS_PROXY=http://127.0.0.1:8888 \
HTTP_PROXY=http://127.0.0.1:8888 \
NODE_EXTRA_CA_CERTS=$(agent-gate cert path) \
codex exec --skip-git-repo-check "say hi in exactly one word" </dev/null
```

Result: codex started, made 9 outbound HTTPS requests for setup, then
hit `ERROR: Reconnecting...` because agent-gate's MITM doesn't proxy
WebSocket upgrades cleanly. The model invocation itself was over
WebSockets (see "Critical finding") and was not captured.

## Hostnames observed

| Host | Purpose |
|---|---|
| `chatgpt.com` | All OpenAI-side traffic (NOT `api.openai.com`) |
| `github.com` | Plugin discovery (`git clone openai/plugins.git`) |

**codex (this version, this auth path) does not contact `api.openai.com`
at all.** This contradicts the prior assumption that codex uses the
public OpenAI Chat Completions API.

## Endpoints captured

All `chatgpt.com` calls go to `/backend-api/...` — the ChatGPT private
backend, OAuth-authenticated. None of them are
`/v1/chat/completions` or `/v1/responses`.

| Path | Method | Status | Resp size | Purpose |
|---|---|---|---|---|
| `/backend-api/plugins/featured?platform=codex` | GET | 200 | <1KB | featured plugins list |
| `/backend-api/codex/models?client_version=0.128.0` | GET | 200 | 222KB | model catalog (see codex_models_list.json) |
| `/backend-api/wham/apps` | POST ×3 | 200/204/200 | 0–161KB | MCP-style `tools` listing (JSON-RPC) |
| `/backend-api/codex/analytics-events/events` | POST | 200 | <1KB | telemetry batch (see codex_analytics_event.json) |
| `/backend-api/connectors/directory/list?external_logos=true` | GET | 200 | 1.7MB | connector / integration catalog (not committed; too large) |

Plus two `github.com` git-protocol calls (`/info/refs`,
`/git-upload-pack`) for git clone/fetch traffic, not API.

## Body shape verdict

**Not OpenAI Chat Completions. Not OpenAI Responses. A custom ChatGPT
internal protocol, mostly JSON, with one MCP-over-JSON-RPC pocket
(`/backend-api/wham/apps`).**

Key shape signals:
- Every `chatgpt.com` request carries `Authorization: Bearer <oauth>`,
  `Chatgpt-Account-Id: <uuid>`, `Originator: codex_exec`,
  `User-Agent: codex_exec/0.128.0 (...)`.
- `/backend-api/codex/models` returns a richly-structured `models`
  array with codex-specific keys: `prefer_websockets`,
  `support_verbosity`, `apply_patch_tool_type`, `web_search_tool_type`,
  `truncation_policy`, `reasoning_summary_format`,
  `shell_type: "shell_command"`, `supported_in_api`,
  `available_in_plans`. None of these are public OpenAI API fields.
- `/backend-api/wham/apps` POSTs return JSON-RPC 2.0
  (`{"jsonrpc": "2.0", "result": {"tools": [...]}}`) — a list of 92
  MCP tools. Protocol family is MCP/JSON-RPC, not OpenAI shape.

## Critical finding: WebSocket transport

Every model in the `/backend-api/codex/models` response has
`prefer_websockets: true`:

```
gpt-5.5, gpt-5.4, gpt-5.4-mini, gpt-5.3-codex, gpt-5.3-codex-spark,
gpt-5.2, codex-auto-review  →  prefer_websockets: true (all 7)
```

**The actual model invocation is over a WebSocket, not HTTP
request/response.** When codex tried to upgrade to WS, agent-gate
logged:

```
WARN: Cannot read request from mitm'd client chatgpt.com:443
      read: connection reset by peer
WARN: Unable to use Websocket connection
```

agent-gate does not currently MITM WebSocket frames. So under the
current proxy implementation, codex's actual prompts/responses are
**not visible** even though the connection is intercepted.

`supported_in_api` is `true` for most models — meaning those same
models would be reachable via `api.openai.com/v1/...` if codex were
running with an `OPENAI_API_KEY` instead of OAuth. That path was NOT
exercised in this capture.

## TLS behavior

- codex respects `NODE_EXTRA_CA_CERTS`. agent-gate's CA was trusted
  cleanly. (Node 25.6.1's `undici` honored `HTTPS_PROXY` here; no
  `NODE_OPTIONS=--use-env-proxy` workaround needed.)
- No cert pinning observed against agent-gate's CA on `chatgpt.com`.
- WebSocket upgrade (over the same TLS session) was where agent-gate's
  proxy fell short — that's a proxy gap, not codex pinning.

## Plan 5 implication

The original eureka — "shape-based parser matching, one OpenAI Chat
Completions parser covers most agentic tools" — **does not hold for
codex in its default OAuth/ChatGPT-backend mode.** Three concrete
implications:

1. **codex needs its own parser family.** The endpoints are
   `chatgpt.com/backend-api/codex/...` and `chatgpt.com/backend-api/
   wham/...`. The shape is bespoke (per-model `support_verbosity`,
   `apply_patch_tool_type`, `truncation_policy`, etc.). A new
   `chatgpt_backend` parser is the right unit.
2. **The actual model invocation is invisible without WebSocket
   support in agent-gate's proxy.** Plan 5 should either (a) add
   WebSocket frame capture (significant proxy work), or (b)
   explicitly document codex (OAuth mode) as "connection metadata
   only" and lean on the rich HTTP-side telemetry (model catalog +
   analytics events + connector list) which still produces useful
   audit output.
3. **A second capture pass with `OPENAI_API_KEY` set** would reveal
   whether codex falls back to `api.openai.com/v1/chat/completions`
   under the API-key path. If yes, that path IS classic OpenAI Chat
   shape — and a single `openai_chat` parser covers
   codex-with-API-key plus presumably most non-codex agents
   (opencode, aider, cursor's agent backend, etc.). The eureka may
   still hold for the non-OAuth case.

**Recommended Plan 5 scope (revised):**
- Ship `openai_chat` parser (validates eureka for the API-key path —
  needs one more capture session with `OPENAI_API_KEY`).
- Ship a `chatgpt_backend` parser for codex's HTTP setup endpoints
  (model catalog, analytics, connectors). Useful even without the
  WebSocket-side body.
- Document WebSocket capture as a Plan 5.1 / future-work boundary.
  Don't ship it in this scope.

## Files in this directory

Each fixture is a single agent-gate `StoredEvent` (matching the JSONL
schema in `internal/store/`). Tests can replay via the parser
pipeline directly; same shape as `testdata/flows/anthropic_*.json`.

- `NOTES.md` — this file.
- `codex_models_list.json` — the `/backend-api/codex/models` GET
  response. Most representative codex-specific shape; reveals the
  WebSocket transport flag for every model.
- `codex_wham_apps_mcp.json` — a `/backend-api/wham/apps` POST
  response. JSON-RPC 2.0 with 92 MCP tools. Shows the
  MCP-over-HTTP pocket inside codex's protocol.
- `codex_analytics_event.json` — a small `/backend-api/codex/
  analytics-events/events` POST. Tiny fixture for the analytics-side
  parser path.

`resp_body` is base64-encoded as agent-gate stores it. Decode with
`base64 -d` for inspection.

## Redaction applied

Headers scrubbed across all three fixtures: `Authorization`, `Cookie`,
`Set-Cookie`, `Chatgpt-Account-Id`, `X-Request-Id`, `X-Oai-Request-Id`,
`OpenAI-Organization`, `OAI-Device-Id`, `CF-Ray`. Response bodies
scanned for `sk-…`, JWT prefixes, `Bearer` strings, email addresses,
and bare UUIDs — none present in the kept bodies. User-prompt content
is absent from these captures (they're all setup calls; the actual
prompt was sent over the unobserved WebSocket).

The `id` field on each event is replaced with the placeholder
`01CODEX_FIXTURE_REDACTED` (the original ULID could be cross-
referenced against the user's local store).

## Follow-up capture: codex with OPENAI_API_KEY (resolves the eureka)

Captured: 2026-05-04 (same day, second run)
Trigger: identical to the OAuth run, but with
`OPENAI_API_KEY=sk-fake-test-not-real-...` exported.

Goal: determine whether codex 0.128.0 falls back to
`api.openai.com/v1/...` when `OPENAI_API_KEY` is in env. If yes,
the public OpenAI Chat parser would cover codex too, and the
"shape-based matching" eureka holds across auth paths.

**Result: it doesn't.** codex completely ignored the env var and
used the existing OAuth tokens in `~/.codex/auth.json`. Same exact
endpoints as the OAuth-only run:

- `chatgpt.com/backend-api/plugins/featured`
- `chatgpt.com/backend-api/wham/apps` (×3, MCP tool list)
- `chatgpt.com/backend-api/codex/analytics-events/events`
- `chatgpt.com/backend-api/connectors/directory/list`
- `github.com/openai/plugins.git/*` (git-protocol clone for plugins)

Zero traffic to `api.openai.com`. The codex CLI prefers OAuth when
auth.json exists, regardless of `OPENAI_API_KEY` in env.

**Plan 5 implication (now locked):** codex is permanently in
`chatgpt_backend` land for users who have logged in via `codex login`.
Plan 5 ships 3 HTTP parsers (not 4): `openai_chat`, `openai_responses`,
`chatgpt_backend`. Plus `chatgpt_realtime` over WS.

**Caveat not tested:** a fresh codex install with `OPENAI_API_KEY`
set BEFORE running `codex login` (i.e., no auth.json on disk). May
behave differently. Not exercised here because we didn't want to
log the user out of their existing codex session. If a user reports
codex hitting `api.openai.com`, that path becomes the second
fixture-capture target. Until then: assume `chatgpt_backend` is
codex's only path.

No new fixture files committed for this run — captured endpoints
are identical to the OAuth-only run; the existing fixtures cover
them. The point of this run was a routing question, and the
routing is now answered.

## Open follow-ups

- Decide WebSocket capture in/out of Plan 5 scope. → DECIDED in
  Plan 5 design 2026-05-04: WS capture IS in scope.
- Capture opencode next — its multi-vendor design is the strongest
  test of "shape-based matching."
- Fresh-install codex with API-key only (no auth.json) — only if a
  user reports it hitting api.openai.com.

## 2026-05-07 follow-up: WS hijack support landed; codex pins; HTTP fallback wins

Captured: 2026-05-07 (Stream B, after T9+T10 shipped)
agent-gate version: feat/plan5-ws-capture (HEAD: edd40c9)
codex version: codex-cli 0.128.0 (codex-tui)
Capture method: airtight launcher + `--hijack-host chatgpt.com` +
`NODE_EXTRA_CA_CERTS=$(agent-gate cert path)`

### Trigger

```fish
NODE_EXTRA_CA_CERTS=(agent-gate cert path) \
NODE_OPTIONS=--use-env-proxy \
agent-gate run --hijack-host chatgpt.com -- codex exec "say hi in one word"
```

### What happened

1. **HTTP setup endpoints** (`/backend-api/wham/apps`,
   `/wham/usage`, `/connectors/directory/list`,
   `/plugins/featured`, `/codex/analytics-events/events`,
   plus `github.com/openai/plugins.git`) all decoded cleanly through
   the standard MITM path.
2. **WebSocket upgrade** to `wss://chatgpt.com/backend-api/codex/responses`
   (note: the path is `/responses`, not `/realtime` as Plan 5's design
   doc speculated). Returned **`101 Switching Protocols` from
   cloudflare** — TLS handshake and HTTP-level upgrade both
   succeeded.
3. **Then codex closed the conn immediately** with no WS frames in
   either direction. Logs in the codex terminal:
   `ERROR codex_api::endpoint::responses_websocket: failed to connect
   to websocket: Attack attempt detected`. This message is
   client-side: codex's Rust client inspects the upstream cert chain,
   sees agent-gate's local CA leaf instead of cloudflare's, and
   refuses to talk further.
4. **codex retried 5 times**, each one a fresh upgrade with the same
   outcome (101 from server, immediate close from client).
5. **After WS retries exhausted, codex fell back to plain
   `POST /backend-api/codex/responses`** — a regular HTTPS request
   with `Accept: text/event-stream`. **This path captured cleanly.**
   Full request (zstd-compressed JSON; system prompt; tool catalog;
   user message) and response (146 KB SSE stream ending in
   `response.completed` with the usage block) landed in the store.

### Critical findings

- **codex 0.128.0 client-pins its TLS for the WebSocket transport.**
  The "Attack attempt detected" terminal message is from
  `codex_api::endpoint::responses_websocket` (Rust crate), not from
  the server. Server returns 101 happily; codex rejects post-handshake.
  No amount of MITM / proxy work captures WS bodies for codex without
  defeating the pin (which is out of scope for an audit gate).
- **codex does NOT pin TLS for the HTTP fallback path.** When the WS
  fails enough times, codex retries the same model invocation as a
  plain HTTPS POST to the same URL. Full body capture works for that
  path — and that's where the actual model conversation lives.
- **Wire format on the HTTP fallback IS the OpenAI Responses API.**
  Top-level keys: `model`, `instructions`, `input` (list of items),
  `tools`, `tool_choice`, `parallel_tool_calls`, `reasoning`, `store`,
  `stream`, `include`, `prompt_cache_key`, `client_metadata`.
  Response is SSE with `response.created` →
  `response.in_progress` → `response.output_item.done`* →
  `response.completed`. Same shape as `/v1/responses` on
  `api.openai.com`, just on `chatgpt.com/backend-api/codex/responses`.
- **Request body is `Content-Encoding: zstd`** — codex compresses
  upstream. Parser must `zstd`-decompress the request body before
  running the OpenAI Responses-API decoder on it.

### Plan 5 Stream B re-scope (locked)

The original "ship a `chatgpt_realtime` parser fixture-driven from
captured WS frames" task is **dropped** — there are no captured
frames and the design's "no parser without ground truth" rule applies.
Replaced by:

1. Extend `openai_responses` (Stream A) to handle
   `chatgpt.com /backend-api/codex/responses` and to decode
   `Content-Encoding: zstd` request bodies. Existing SSE response
   decoder (`response.completed`) applies unchanged.
2. Add a `ws_pinned_upstream` info-level policy rule that fires on
   any 101 upgrade flow with no child WS message events — explains
   to the user why no body shows up under codex's WS attempts.
3. Keep the WS hijack handler (T9 / T10 / HTTP fall-through) as
   infrastructure. It's not load-bearing for codex visibility, but
   it sets us up to capture WS bodies for any future agent that
   doesn't pin (or in cases where the pin can be sidestepped).
4. Document the codex pinning + HTTP fallback flow in README and
   doctor's known-limitation surface.

### Files added in this run

- `codex_responses_post_streaming.json` — synthetic Responses-API
  fixture with a `Content-Encoding: zstd` request and a small
  SSE response. Real captured shape; values are synthesized to
  avoid quoting OpenAI's product prompt or tool catalog.
- `codex_ws_upgrade_pinned.json` — header-only upgrade fixture.
  Real wire shape with all PII scrubbed (auth, account, session,
  workspace path). Empty body since the pin closes the conn before
  any frames flow. Used by the `ws_pinned_upstream` rule test.

### Redaction applied (this run)

Headers scrubbed: `Authorization`, `Cookie`, `Set-Cookie`,
`Chatgpt-Account-Id`, `Session_id`, `X-Client-Request-Id`,
`X-Codex-Window-Id`, `X-Codex-Turn-Metadata`, `Sec-Websocket-Key`,
`Sec-Websocket-Accept`, `Cf-Ray`, `Date`, `X-Oai-Request-Id`,
`X-Models-Etag`, `Nel`, `Report-To`. The X-Codex-Turn-Metadata
JSON (which embeds workspace path + git remote URL +
commit hash) was rewritten to a synthetic `/workspace/test-project`
+ `https://example.com/test/test-project.git` placeholder.
