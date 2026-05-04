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

## Open follow-ups

- Capture codex with `OPENAI_API_KEY` set (forces the public-API
  path). One more session, ~5 tokens. Confirms or kills the eureka
  for the non-OAuth codex case.
- Decide WebSocket capture in/out of Plan 5 scope.
- Capture opencode next — its multi-vendor design is the strongest
  test of "shape-based matching."
