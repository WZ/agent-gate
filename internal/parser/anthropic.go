package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	"agent-gate/internal/types"
)

type AnthropicMessages struct{}

func (AnthropicMessages) Match(flow *types.RawFlow) bool {
	return hostOf(flow.URL) == "api.anthropic.com"
}

func (AnthropicMessages) Parse(flow *types.RawFlow) (*types.ParsedEvent, error) {
	ev := types.ParsedEvent{
		RawFlow: *flow,
		Kind:    "anthropic_messages",
	}
	parseAnthropic(&ev)
	return &ev, nil
}

// anthropicRequest is the subset of the Messages request body we care about.
type anthropicRequest struct {
	Model    string `json:"model"`
	Metadata struct {
		UserID    string `json:"user_id"`
		SessionID string `json:"session_id"`
	} `json:"metadata"`
}

// anthropicResponse is the subset of the Messages response body we care about.
type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text"`
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func parseAnthropic(ev *types.ParsedEvent) {
	parseAnthropicRequest(ev)
	if isSSE(ev.RespHeaders) {
		parseAnthropicSSEResponse(ev)
		ev.IsStreamed = true
	} else {
		parseAnthropicJSONResponse(ev)
	}
	if ev.SessionID == "" {
		ev.SessionID = ev.RawFlow.ClientConnID
	}
}

func parseAnthropicRequest(ev *types.ParsedEvent) {
	var req anthropicRequest
	_ = json.Unmarshal(ev.ReqBody, &req)
	ev.Model = req.Model

	// Session id priority: x-claude-session-id > body metadata.session_id > body metadata.user_id.
	if v := ev.ReqHeaders.Get("X-Claude-Session-Id"); v != "" {
		ev.SessionID = v
	} else if req.Metadata.SessionID != "" {
		ev.SessionID = req.Metadata.SessionID
	} else if req.Metadata.UserID != "" {
		ev.SessionID = req.Metadata.UserID
	}

	// Extract any tool_result blocks from the request (model is being given prior tool outputs).
	ev.ToolResults = extractToolResultsFromRequest(ev.ReqBody)
}

func parseAnthropicJSONResponse(ev *types.ParsedEvent) {
	var resp anthropicResponse
	if err := json.Unmarshal(ev.RespBody, &resp); err != nil {
		return
	}
	if resp.Model != "" {
		ev.Model = resp.Model
	}
	ev.Usage = types.Usage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		CacheRead:    resp.Usage.CacheReadInputTokens,
	}
	for _, b := range resp.Content {
		if b.Type == "tool_use" {
			ev.Tools = append(ev.Tools, types.ToolUse{
				ID:    b.ID,
				Name:  b.Name,
				Input: b.Input,
			})
		}
	}
}

// isSSE reports whether the response is an event-stream.
func isSSE(h map[string][]string) bool {
	for _, v := range h["Content-Type"] {
		if strings.Contains(strings.ToLower(v), "text/event-stream") {
			return true
		}
	}
	return false
}

// extractToolResultsFromRequest scans a Messages request body's `messages[*].content[*]` for tool_result blocks.
func extractToolResultsFromRequest(body []byte) []types.ToolResult {
	var req struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil
	}
	var results []types.ToolResult
	for _, m := range req.Messages {
		// content can be a string (no tool_results) or array of blocks.
		if len(m.Content) == 0 || m.Content[0] != '[' {
			continue
		}
		var blocks []struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_result" {
				continue
			}
			var contentStr string
			if err := json.Unmarshal(b.Content, &contentStr); err != nil {
				// content is structured (array of blocks) — stringify.
				contentStr = string(b.Content)
			}
			results = append(results, types.ToolResult{
				ToolUseID: b.ToolUseID,
				Content:   contentStr,
				IsError:   b.IsError,
			})
		}
	}
	return results
}

// parseAnthropicSSEResponse re-assembles a streamed Messages response into one logical event.
func parseAnthropicSSEResponse(ev *types.ParsedEvent) {
	scanner := bufio.NewScanner(bytes.NewReader(ev.RespBody))
	scanner.Buffer(make([]byte, 1<<16), 1<<24)

	var (
		assembled        strings.Builder
		accumulatedTools []types.ToolUse
		inputTokens      int
		outputTokens     int
		cacheReadTokens  int
		modelFromMessage string
		currentToolBlock *types.ToolUse
		currentToolJSON  strings.Builder
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
		var evt struct {
			Type         string          `json:"type"`
			Message      json.RawMessage `json:"message"`
			Delta        json.RawMessage `json:"delta"`
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
			Usage        struct {
				InputTokens          int `json:"input_tokens"`
				OutputTokens         int `json:"output_tokens"`
				CacheReadInputTokens int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "message_start":
			var m struct {
				Model string `json:"model"`
				Usage struct {
					InputTokens          int `json:"input_tokens"`
					CacheReadInputTokens int `json:"cache_read_input_tokens"`
				} `json:"usage"`
			}
			_ = json.Unmarshal(evt.Message, &m)
			modelFromMessage = m.Model
			inputTokens = m.Usage.InputTokens
			cacheReadTokens = m.Usage.CacheReadInputTokens
		case "content_block_start":
			var b struct {
				Type  string         `json:"type"`
				ID    string         `json:"id"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			}
			_ = json.Unmarshal(evt.ContentBlock, &b)
			if b.Type == "tool_use" {
				currentToolBlock = &types.ToolUse{ID: b.ID, Name: b.Name, Input: b.Input}
				currentToolJSON.Reset()
			}
		case "content_block_delta":
			var d struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			}
			_ = json.Unmarshal(evt.Delta, &d)
			if d.Type == "text_delta" {
				assembled.WriteString(d.Text)
			} else if d.Type == "input_json_delta" && currentToolBlock != nil {
				currentToolJSON.WriteString(d.PartialJSON)
			}
		case "content_block_stop":
			if currentToolBlock != nil {
				if s := currentToolJSON.String(); s != "" {
					var input map[string]any
					if err := json.Unmarshal([]byte(s), &input); err == nil {
						currentToolBlock.Input = input
					}
				}
				accumulatedTools = append(accumulatedTools, *currentToolBlock)
				currentToolBlock = nil
				currentToolJSON.Reset()
			}
		case "message_delta":
			// Anthropic emits usage as a top-level sibling of delta in message_delta events
			// (see SSE docs: https://docs.anthropic.com/en/api/messages-streaming).
			outputTokens = evt.Usage.OutputTokens
		}
	}

	if modelFromMessage != "" {
		ev.Model = modelFromMessage
	}
	ev.Tools = accumulatedTools
	ev.Usage = types.Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CacheRead:    cacheReadTokens,
	}
}
