package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"agent-gate/internal/types"
)

// OpenAIChat decodes OpenAI Chat Completions API exchanges. The same shape is
// served by many "OpenAI-compatible" gateways, so we match on path + body
// shape rather than hostname.
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
		parseOpenAIChatSSEResponse(&ev)
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

// parseOpenAIChatSSEResponse re-assembles a streamed Chat Completions response
// into the same parsed-event shape as the non-streaming branch.
//
// Each line is "data: <json>" where <json> is a chunk with object
// "chat.completion.chunk". The chunk's choices[*].delta carries incremental
// fields:
//   - role: "assistant" (first chunk only)
//   - content: text token (concatenated across chunks; not stored on
//     ParsedEvent today, matching the Anthropic streamed parser)
//   - tool_calls: streamed entries with a stable index (used as bucket key,
//     NOT the per-delta order)
//
// A final chunk with empty delta and finish_reason closes the stream. The
// "[DONE]" sentinel is the literal string after the last data line.
//
// Usage may appear in a trailing chunk only when stream_options.include_usage
// is set on the request; we surface it when present and leave zeros otherwise.
func parseOpenAIChatSSEResponse(ev *types.ParsedEvent) {
	scanner := bufio.NewScanner(bytes.NewReader(ev.RespBody))
	scanner.Buffer(make([]byte, 1<<16), 1<<24)

	type toolBucket struct {
		ID      string
		Name    string
		ArgsBuf strings.Builder
	}
	var (
		modelFromStream string
		buckets         []*toolBucket
		usageInput      int
		usageOutput     int
	)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Model != "" && modelFromStream == "" {
			modelFromStream = chunk.Model
		}
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usageInput = chunk.Usage.PromptTokens
			usageOutput = chunk.Usage.CompletionTokens
		}

		for _, choice := range chunk.Choices {
			for _, tc := range choice.Delta.ToolCalls {
				for tc.Index >= len(buckets) {
					buckets = append(buckets, &toolBucket{})
				}
				b := buckets[tc.Index]
				if tc.ID != "" {
					b.ID = tc.ID
				}
				if tc.Function.Name != "" {
					b.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					b.ArgsBuf.WriteString(tc.Function.Arguments)
				}
			}
		}
	}

	if modelFromStream != "" {
		ev.Model = modelFromStream
	}
	for _, b := range buckets {
		if b.ID == "" && b.Name == "" {
			// Empty bucket — only happens if a delta referenced an out-of-range
			// index without ever providing identity. Skip rather than emit a
			// placeholder ToolUse the dashboard would render as blank.
			continue
		}
		ev.Tools = append(ev.Tools, types.ToolUse{
			ID:    b.ID,
			Name:  b.Name,
			Input: decodeToolCallArguments(json.RawMessage(quoteJSONString(b.ArgsBuf.String()))),
		})
	}
	ev.Usage.InputTokens = usageInput
	ev.Usage.OutputTokens = usageOutput
}

// quoteJSONString wraps a raw assembled-arguments string in JSON quotes so
// decodeToolCallArguments (which expects the OpenAI "JSON-encoded string"
// shape) can unwrap it. Inputs like {"city":"SF"} → "{\"city\":\"SF\"}".
func quoteJSONString(s string) []byte {
	if s == "" {
		return nil
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	return encoded
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
