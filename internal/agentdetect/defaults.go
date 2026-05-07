package agentdetect

// Defaults maps agent names to their default upstream hosts. Used as a
// fallback when env-derived hosts aren't present.
//
// codex defaults seed both chatgpt.com (OAuth path, the codex 0.128.0
// default) and api.openai.com (API-key path, when ~/.codex/auth.json is
// absent). The OAuth path uses /backend-api/codex/responses with HTTP
// fallback when its WebSocket transport gets pinned through the local CA.
// The API-key path uses /v1/responses on api.openai.com directly.
var Defaults = map[string][]string{
	"claude":   {"api.anthropic.com"},
	"codex":    {"chatgpt.com", "api.openai.com"},
	"aider":    {"api.anthropic.com", "api.openai.com"},
	"opencode": {"api.anthropic.com"},
}
