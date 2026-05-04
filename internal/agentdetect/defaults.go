package agentdetect

// Defaults maps agent names to their default upstream hosts. Used as a
// fallback when env-derived hosts aren't present.
var Defaults = map[string][]string{
	"claude":   {"api.anthropic.com"},
	"codex":    {"api.openai.com"},
	"aider":    {"api.anthropic.com", "api.openai.com"},
	"opencode": {"api.anthropic.com"},
}
