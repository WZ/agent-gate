package parser

import (
	"encoding/json"
	"net/url"
	"strings"

	"agent-gate/internal/types"
)

// OpenAIResponses decodes OpenAI's newer /v1/responses API exchanges. Like
// OpenAIChat, the same shape is exposed by every "OpenAI-compatible" gateway
// that has rolled Responses support under paths such as /openai/v1/responses,
// so we match on path + body
// shape rather than hostname.
//
// The Responses API differs from Chat Completions in three ways relevant
// here:
//   - Request uses `input` (string or list of items) instead of `messages`
//   - Response uses `output` (list of typed items: message, reasoning,
//     function_call, etc.) instead of `choices[].message`
//   - Usage uses `input_tokens`/`output_tokens` instead of
//     `prompt_tokens`/`completion_tokens`
type OpenAIResponses struct{}

// Match uses a dual anchor: path suffix "/responses" plus a present `input`
// field in the request body. The path alone is too greedy (any unrelated
// service whose path ends in "/responses" would trip it) and the field alone
// is too narrow to anchor on. Together they pin to genuine Responses API
// calls across vanilla OpenAI, Azure, and OpenAI-compatible gateways.
func (OpenAIResponses) Match(flow *types.RawFlow) bool {
	u, err := url.Parse(flow.URL)
	if err != nil || u == nil {
		return false
	}
	if !strings.HasSuffix(u.Path, "/responses") {
		return false
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(flow.ReqBody, &probe); err != nil {
		return false
	}
	_, ok := probe["input"]
	return ok
}

func (OpenAIResponses) Parse(flow *types.RawFlow) (*types.ParsedEvent, error) {
	ev := types.ParsedEvent{
		RawFlow:  *flow,
		Kind:     "openai_responses",
		Endpoint: "responses",
	}
	parseOpenAIResponsesRequest(&ev)
	if isSSE(ev.RespHeaders) {
		ev.IsStreamed = true
	} else {
		parseOpenAIResponsesJSONResponse(&ev)
	}
	if ev.SessionID == "" {
		ev.SessionID = ev.RawFlow.ClientConnID
	}
	return &ev, nil
}

type openaiResponsesRequest struct {
	Model              string          `json:"model"`
	User               string          `json:"user"`
	PreviousResponseID string          `json:"previous_response_id"`
	Input              json.RawMessage `json:"input"`
	Tools              []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tools"`
}

type openaiResponsesResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Model  string `json:"model"`
	Status string `json:"status"`
	Output []struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		CallID    string          `json:"call_id"`
		Arguments json.RawMessage `json:"arguments"`
		Content   []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func parseOpenAIResponsesRequest(ev *types.ParsedEvent) {
	var req openaiResponsesRequest
	if err := json.Unmarshal(ev.ReqBody, &req); err != nil {
		return
	}
	ev.Model = req.Model

	// `user` is stable across a conversation and keeps dashboard session groups
	// together. `previous_response_id` changes on each follow-up, so only use it
	// when there is no stable end-user identifier.
	switch {
	case req.User != "":
		ev.SessionID = req.User
	case req.PreviousResponseID != "":
		ev.SessionID = req.PreviousResponseID
	}

	ev.ItemCount = countResponsesInput(req.Input)
	ev.ToolResults = extractResponsesToolResults(req.Input)
}

func parseOpenAIResponsesJSONResponse(ev *types.ParsedEvent) {
	var resp openaiResponsesResponse
	if err := json.Unmarshal(ev.RespBody, &resp); err != nil {
		return
	}
	if resp.Model != "" {
		ev.Model = resp.Model
	}
	ev.Usage.InputTokens = resp.Usage.InputTokens
	ev.Usage.OutputTokens = resp.Usage.OutputTokens

	for _, item := range resp.Output {
		if item.Type != "function_call" {
			continue
		}
		ev.Tools = append(ev.Tools, types.ToolUse{
			ID:    item.CallID,
			Name:  item.Name,
			Input: decodeToolCallArguments(item.Arguments),
		})
	}
}

// countResponsesInput returns 1 for a plain string input and the number of
// items for a list input. Returns 0 only when the field is absent or
// unparseable.
func countResponsesInput(input json.RawMessage) int {
	if len(input) == 0 {
		return 0
	}
	// String form: "say hi in one word"
	var s string
	if err := json.Unmarshal(input, &s); err == nil {
		return 1
	}
	// List form: [{role, content, ...}, ...]
	var list []json.RawMessage
	if err := json.Unmarshal(input, &list); err == nil {
		return len(list)
	}
	return 0
}

// extractResponsesToolResults pulls every function_call_output item out of
// the request input list — those are the agent replaying prior tool outputs
// back into the model.
func extractResponsesToolResults(input json.RawMessage) []types.ToolResult {
	if len(input) == 0 {
		return nil
	}
	var list []struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Output any    `json:"output"`
	}
	if err := json.Unmarshal(input, &list); err != nil {
		return nil
	}
	var out []types.ToolResult
	for _, item := range list {
		if item.Type != "function_call_output" {
			continue
		}
		out = append(out, types.ToolResult{
			ToolUseID: item.CallID,
			Content:   stringifyContent(item.Output),
		})
	}
	return out
}
