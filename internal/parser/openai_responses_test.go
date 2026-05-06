package parser

import (
	"net/http"
	"testing"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesMatch(t *testing.T) {
	p := OpenAIResponses{}

	flow := loadFlow(t, "../../testdata/flows/openai/responses_nonstreaming.json")
	assert.True(t, p.Match(&flow), "should match captured fazai/litellm responses call")

	assert.True(t, p.Match(&types.RawFlow{
		URL:     "https://api.openai.com/v1/responses",
		ReqBody: []byte(`{"model":"gpt-4o","input":"hi"}`),
	}), "vanilla OpenAI host should match string-input form")

	assert.True(t, p.Match(&types.RawFlow{
		URL:     "https://example.azure.com/openai/v1/responses",
		ReqBody: []byte(`{"model":"gpt-4o","input":[{"role":"user","content":"hi"}]}`),
	}), "Azure deployment URL with list input should match")

	assert.False(t, p.Match(&types.RawFlow{
		URL:     "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
	}), "Chat Completions request must not match — different parser owns it")

	assert.False(t, p.Match(&types.RawFlow{
		URL:     "https://api.openai.com/v1/responses",
		ReqBody: []byte(`{"model":"gpt-4o"}`),
	}), "missing input field is not a real Responses request")

	// Documented best-effort: any path ending in "/responses" with an `input`
	// field claims the parser. We accept this slightly broader catchment so
	// future OpenAI-compatible vendors that mount the API under a vendor
	// prefix ("/openai/v1/responses", "/proxy/v1/responses") all decode out
	// of the box. If false-positive collisions show up in the wild, tighten
	// here to require "/v1/responses" or a vendor allowlist.
	assert.True(t, p.Match(&types.RawFlow{
		URL:     "https://gateway.example.com/proxy/v1/responses",
		ReqBody: []byte(`{"model":"x","input":"hi"}`),
	}), "vendor prefix paths should still match — Match is intentionally permissive")
}

func TestParseOpenAIResponsesNonstreaming(t *testing.T) {
	flow := loadFlow(t, "../../testdata/flows/openai/responses_nonstreaming.json")

	ev := Parse(flow)

	assert.Equal(t, "openai_responses", ev.Kind)
	assert.Equal(t, "responses", ev.Endpoint)
	assert.Equal(t, "gpt-oss-120b", ev.Model)
	assert.Equal(t, 1, ev.ItemCount, "string input counts as one item")
	assert.False(t, ev.IsStreamed)
	assert.Equal(t, 72, ev.Usage.InputTokens)
	assert.Equal(t, 71, ev.Usage.OutputTokens)
	assert.Empty(t, ev.Tools, "no function_call output items in this completion")
	assert.Empty(t, ev.ToolResults)
}

func TestParseOpenAIResponsesExtractsFunctionCall(t *testing.T) {
	flow := types.RawFlow{
		Method: "POST",
		URL:    "https://api.openai.com/v1/responses",
		ReqBody: []byte(`{
			"model": "gpt-4o",
			"input": "what's the weather in SF?"
		}`),
		RespStatus:  200,
		RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
		RespBody: []byte(`{
			"id": "resp_abc",
			"object": "response",
			"model": "gpt-4o-2024-08-06",
			"status": "completed",
			"output": [
				{
					"type": "function_call",
					"id": "fc_1",
					"call_id": "call_1",
					"name": "get_weather",
					"arguments": "{\"city\":\"SF\"}"
				}
			],
			"usage": {"input_tokens": 14, "output_tokens": 9}
		}`),
	}

	ev := Parse(flow)

	assert.Equal(t, "openai_responses", ev.Kind)
	assert.Equal(t, "gpt-4o-2024-08-06", ev.Model, "response model wins over request model")
	require.Len(t, ev.Tools, 1)
	assert.Equal(t, "call_1", ev.Tools[0].ID, "exposes call_id, not the function_call item id")
	assert.Equal(t, "get_weather", ev.Tools[0].Name)
	assert.Equal(t, "SF", ev.Tools[0].Input["city"])
}

func TestParseOpenAIResponsesExtractsToolResults(t *testing.T) {
	flow := types.RawFlow{
		Method: "POST",
		URL:    "https://api.openai.com/v1/responses",
		ReqBody: []byte(`{
			"model": "gpt-4o",
			"input": [
				{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{}"},
				{"type": "function_call_output", "call_id": "call_1", "output": "sunny, 68°F"},
				{"role": "user", "content": "thanks"}
			]
		}`),
		RespStatus:  200,
		RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
		RespBody:    []byte(`{"id":"resp_x","object":"response","model":"gpt-4o","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`),
	}

	ev := Parse(flow)

	assert.Equal(t, 3, ev.ItemCount, "three items in the input list")
	require.Len(t, ev.ToolResults, 1)
	assert.Equal(t, "call_1", ev.ToolResults[0].ToolUseID)
	assert.Equal(t, "sunny, 68°F", ev.ToolResults[0].Content)
}

func TestParseOpenAIResponsesPrefersPreviousResponseID(t *testing.T) {
	// previous_response_id wins over `user` because it's the API-native
	// conversation-chaining marker — ties follow-on calls to their parent.
	flow := types.RawFlow{
		Method: "POST",
		URL:    "https://api.openai.com/v1/responses",
		ReqBody: []byte(`{
			"model": "gpt-4o",
			"input": "follow-up",
			"user": "u-42",
			"previous_response_id": "resp_parent"
		}`),
		RespStatus:  200,
		RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
		RespBody:    []byte(`{"id":"resp_child","object":"response","model":"gpt-4o","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`),
	}

	ev := Parse(flow)

	assert.Equal(t, "resp_parent", ev.SessionID)
}

func TestParseOpenAIResponsesStreamingMarksFlag(t *testing.T) {
	flow := types.RawFlow{
		Method:  "POST",
		URL:     "https://api.openai.com/v1/responses",
		ReqBody: []byte(`{"model":"gpt-4o","input":"hi","stream":true}`),
		RespHeaders: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		RespBody: []byte(`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"resp_x"}}` + "\n\n"),
	}

	ev := Parse(flow)

	assert.Equal(t, "openai_responses", ev.Kind)
	assert.True(t, ev.IsStreamed)
	assert.Equal(t, "gpt-4o", ev.Model)
}
