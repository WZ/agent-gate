package parser

import (
	"encoding/json"
	"net/url"
	"strings"

	"agent-gate/internal/types"
)

// OpenAIChat decodes OpenAI Chat Completions API exchanges. The same shape is
// served by every "OpenAI-compatible" gateway (Azure OpenAI, Fortinet FazAI,
// LiteLLM, vLLM, DeepSeek, Mistral, Groq, Together, Ollama) so we match on
// path + body shape rather than hostname.
type OpenAIChat struct{}

// Match uses a dual anchor: path suffix "/chat/completions" plus a non-empty
// "messages" array in the request body. Either one alone would over-claim —
// a path-only match pulls in proxies that wrap unrelated bodies under the
// same URL, and a body-only match would steal Anthropic Messages flows
// (their request body also has a top-level messages array). Together they
// pin us to genuine OpenAI Chat Completions exchanges across vanilla OpenAI,
// Azure deployments, and every "OpenAI-compatible" gateway.
func (OpenAIChat) Match(flow *types.RawFlow) bool {
	u, err := url.Parse(flow.URL)
	if err != nil || u == nil {
		return false
	}
	if !strings.HasSuffix(u.Path, "/chat/completions") {
		return false
	}
	var probe struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(flow.ReqBody, &probe); err != nil {
		return false
	}
	return len(probe.Messages) > 0
}

func (OpenAIChat) Parse(flow *types.RawFlow) (*types.ParsedEvent, error) {
	ev := types.ParsedEvent{
		RawFlow:  *flow,
		Kind:     "openai_chat",
		Endpoint: "chat_completions",
	}
	parseOpenAIChatRequest(&ev)
	if isSSE(ev.RespHeaders) {
		ev.IsStreamed = true
		// Streaming is fixture-driven once we have an SSE capture; for now we
		// still return the parsed-request half so the dashboard shows model +
		// message count rather than "generic" for streaming completions.
	} else {
		parseOpenAIChatJSONResponse(&ev)
	}
	if ev.SessionID == "" {
		ev.SessionID = ev.RawFlow.ClientConnID
	}
	return &ev, nil
}

// openaiChatRequest is the subset of the request body we care about. The full
// schema has dozens of fields (temperature, top_p, presence_penalty, etc.);
// we only decode what surfaces in the dashboard.
type openaiChatRequest struct {
	Model    string `json:"model"`
	User     string `json:"user"`
	Messages []struct {
		Role       string `json:"role"`
		Content    any    `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		Name       string `json:"name"`
	} `json:"messages"`
	Tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

type openaiChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Object  string `json:"object"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func parseOpenAIChatRequest(ev *types.ParsedEvent) {
	var req openaiChatRequest
	if err := json.Unmarshal(ev.ReqBody, &req); err != nil {
		return
	}
	ev.Model = req.Model
	ev.ItemCount = len(req.Messages)

	// "user" is OpenAI's stable end-user identifier (analytics + abuse signal).
	// Treat it as the session id when present so the dashboard can group flows.
	if req.User != "" {
		ev.SessionID = req.User
	}

	// Surface each prior tool result the agent is replaying back into the
	// model — same convention as the Anthropic parser (different field shape).
	for _, m := range req.Messages {
		if m.Role != "tool" {
			continue
		}
		ev.ToolResults = append(ev.ToolResults, types.ToolResult{
			ToolUseID: m.ToolCallID,
			Content:   stringifyContent(m.Content),
		})
	}
}

func parseOpenAIChatJSONResponse(ev *types.ParsedEvent) {
	var resp openaiChatResponse
	if err := json.Unmarshal(ev.RespBody, &resp); err != nil {
		return
	}
	if resp.Model != "" {
		ev.Model = resp.Model
	}
	ev.Usage.InputTokens = resp.Usage.PromptTokens
	ev.Usage.OutputTokens = resp.Usage.CompletionTokens

	for _, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			ev.Tools = append(ev.Tools, types.ToolUse{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: decodeToolCallArguments(tc.Function.Arguments),
			})
		}
	}
}

// decodeToolCallArguments unwraps OpenAI's tool_call.function.arguments field,
// which is a JSON-encoded *string* containing the actual argument object —
// not a nested object. Returns an empty map if the inner payload is absent or
// unparseable rather than failing the whole parse.
func decodeToolCallArguments(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	var inner string
	if err := json.Unmarshal(raw, &inner); err != nil {
		// Some compatibility layers (older models, third-party gateways) inline
		// the object directly. Try that shape before giving up.
		_ = json.Unmarshal(raw, &out)
		return out
	}
	if inner == "" {
		return out
	}
	_ = json.Unmarshal([]byte(inner), &out)
	return out
}

// stringifyContent flattens the `content` field which can be a plain string OR
// a list of {type:"text", text:"..."} chunks (vision / multimodal shape).
func stringifyContent(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, p := range c {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := m["text"].(string); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
