package parser

import (
	"net/http"
	"strings"
	"testing"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatMatch(t *testing.T) {
	p := OpenAIChat{}

	flow := loadFlow(t, "../../testdata/flows/openai/chat_completions_nonstreaming.json")
	assert.True(t, p.Match(&flow), "should match captured compatible-gateway chat completion")

	assert.True(t, p.Match(&types.RawFlow{
		URL:     "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
	}), "vanilla OpenAI host should match")

	assert.True(t, p.Match(&types.RawFlow{
		URL:     "https://api.deepseek.com/v1/chat/completions",
		ReqBody: []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}`),
	}), "OpenAI-compatible third party (DeepSeek) should match")

	assert.False(t, p.Match(&types.RawFlow{
		URL:     "https://api.anthropic.com/v1/messages",
		ReqBody: []byte(`{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`),
	}), "Anthropic Messages must not match — different parser owns it")

	assert.False(t, p.Match(&types.RawFlow{
		URL:     "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{"model":"gpt-4o"}`),
	}), "missing messages field is not chat-completion-shaped")

	assert.False(t, p.Match(&types.RawFlow{
		URL:     "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{"model":"gpt-4o","messages":[]}`),
	}), "empty messages array is not a real completion request")

	assert.False(t, p.Match(&types.RawFlow{
		URL:     "https://api.openai.com/v1/embeddings",
		ReqBody: []byte(`{"input":"hi","model":"text-embedding-3-small"}`),
	}), "embeddings endpoint is a different shape")
}

func TestOpenAIChatStringifyContentMultimodal(t *testing.T) {
	// content can be a list of {type,text,image_url,...} chunks for vision /
	// multimodal calls. We flatten just the text parts; non-text chunks are
	// represented elsewhere in the flow (raw bytes, response).
	flow := types.RawFlow{
		Method: "POST",
		URL:    "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{
			"model": "gpt-4o",
			"messages": [
				{"role":"user","content":[{"type":"text","text":"hi"}]},
				{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},
				{"role":"tool","tool_call_id":"c1","content":[{"type":"text","text":"first"},{"type":"image_url","image_url":{"url":"data:..."}},{"type":"text","text":"second"}]}
			]
		}`),
		RespStatus:  200,
		RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
		RespBody:    []byte(`{"object":"chat.completion","model":"gpt-4o","choices":[],"usage":{}}`),
	}

	ev := Parse(flow)

	require.Len(t, ev.ToolResults, 1)
	assert.Equal(t, "first\nsecond", ev.ToolResults[0].Content,
		"text chunks should be joined with newlines, image chunks dropped")
}

func TestParseOpenAIChatToolCallArgumentsInlinedObject(t *testing.T) {
	// Some compatibility layers and older models inline the arguments object
	// directly instead of double-encoding it as a string. The fallback branch
	// in decodeToolCallArguments should still pick out the values.
	flow := types.RawFlow{
		Method: "POST",
		URL:    "https://compat-gateway.example.com/v1/chat/completions",
		ReqBody: []byte(`{
			"model": "some-model",
			"messages": [{"role":"user","content":"weather?"}]
		}`),
		RespStatus:  200,
		RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
		RespBody: []byte(`{
			"object": "chat.completion",
			"model": "some-model",
			"choices": [{
				"index": 0,
				"finish_reason": "tool_calls",
				"message": {
					"role": "assistant",
					"tool_calls": [{
						"id": "call_2",
						"type": "function",
						"function": {"name": "get_weather", "arguments": {"city": "NYC"}}
					}]
				}
			}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1}
		}`),
	}

	ev := Parse(flow)

	require.Len(t, ev.Tools, 1)
	assert.Equal(t, "get_weather", ev.Tools[0].Name)
	assert.Equal(t, "NYC", ev.Tools[0].Input["city"],
		"inlined-object arguments must still decode")
}

func TestParseOpenAIChatNonstreaming(t *testing.T) {
	flow := loadFlow(t, "../../testdata/flows/openai/chat_completions_nonstreaming.json")

	ev := Parse(flow)

	assert.Equal(t, "openai_chat", ev.Kind)
	assert.Equal(t, "chat_completions", ev.Endpoint)
	assert.Equal(t, "gpt-oss-120b", ev.Model)
	assert.Equal(t, 1, ev.ItemCount, "one user message in the request")
	assert.False(t, ev.IsStreamed)
	assert.Equal(t, 72, ev.Usage.InputTokens)
	assert.Equal(t, 56, ev.Usage.OutputTokens)
	assert.Empty(t, ev.Tools, "no tool_calls in this completion")
	assert.Empty(t, ev.ToolResults, "no prior tool results in the request")
}

func TestParseOpenAIChatExtractsToolCalls(t *testing.T) {
	flow := types.RawFlow{
		ID:     "tc",
		Method: "POST",
		URL:    "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{
			"model": "gpt-4o",
			"messages": [{"role":"user","content":"weather in SF?"}]
		}`),
		RespStatus:  200,
		RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
		RespBody: []byte(`{
			"id": "chatcmpl-x",
			"object": "chat.completion",
			"model": "gpt-4o-2024-08-06",
			"choices": [{
				"index": 0,
				"finish_reason": "tool_calls",
				"message": {
					"role": "assistant",
					"content": null,
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {"name": "get_weather", "arguments": "{\"city\":\"SF\"}"}
					}]
				}
			}],
			"usage": {"prompt_tokens": 14, "completion_tokens": 9}
		}`),
	}

	ev := Parse(flow)

	assert.Equal(t, "openai_chat", ev.Kind)
	assert.Equal(t, "gpt-4o-2024-08-06", ev.Model, "response model wins over request model")
	require.Len(t, ev.Tools, 1)
	assert.Equal(t, "call_1", ev.Tools[0].ID)
	assert.Equal(t, "get_weather", ev.Tools[0].Name)
	assert.Equal(t, "SF", ev.Tools[0].Input["city"])
}

func TestParseOpenAIChatExtractsToolResults(t *testing.T) {
	flow := types.RawFlow{
		ID:     "tr",
		Method: "POST",
		URL:    "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{
			"model": "gpt-4o",
			"messages": [
				{"role":"user","content":"weather in SF?"},
				{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
				{"role":"tool","tool_call_id":"call_1","content":"sunny, 68°F"}
			]
		}`),
		RespStatus:  200,
		RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
		RespBody:    []byte(`{"object":"chat.completion","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`),
	}

	ev := Parse(flow)

	require.Len(t, ev.ToolResults, 1)
	assert.Equal(t, "call_1", ev.ToolResults[0].ToolUseID)
	assert.Equal(t, "sunny, 68°F", ev.ToolResults[0].Content)
}

func TestParseOpenAIChatStreamingFixture(t *testing.T) {
	// Real captured stream from a Fortinet LiteLLM gateway. The first chunk's
	// model wins over the request's model, no tool_calls fired, and this
	// particular gateway didn't include a usage trailer (no
	// stream_options.include_usage on the request).
	flow := loadFlow(t, "../../testdata/flows/openai/chat_completions_streaming.json")

	ev := Parse(flow)

	assert.Equal(t, "openai_chat", ev.Kind)
	assert.True(t, ev.IsStreamed)
	assert.Equal(t, "gpt-oss-120b", ev.Model)
	assert.Empty(t, ev.Tools)
	assert.Zero(t, ev.Usage.InputTokens, "no usage trailer when stream_options.include_usage is unset")
	assert.Zero(t, ev.Usage.OutputTokens)
}

func TestParseOpenAIChatStreamingMarksFlag(t *testing.T) {
	// Minimal SSE — request-side metadata still decodes even when the response
	// stream is just a single content delta with no usage / tool calls.
	flow := types.RawFlow{
		ID:      "sse",
		Method:  "POST",
		URL:     "https://api.openai.com/v1/chat/completions",
		ReqBody: []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		RespHeaders: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		RespBody: []byte(`data: {"choices":[{"delta":{"content":"H"}}]}` + "\n\n"),
	}

	ev := Parse(flow)

	assert.Equal(t, "openai_chat", ev.Kind)
	assert.True(t, ev.IsStreamed)
	assert.Equal(t, "gpt-4o", ev.Model)
}

func TestParseOpenAIChatStreamingAssemblesToolCalls(t *testing.T) {
	// Tool-call streaming: the call's identity (id + function.name) arrives in
	// the first delta, then arguments stream in token-by-token across many
	// deltas. The same `index` field on each delta is the bucket key. A final
	// optional usage chunk lands when stream_options.include_usage is set.
	body := strings.Join([]string{
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city"}}]}}]}`,
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"SF\"}"}}]}}]}`,
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chunk","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":7}}`,
		`data: [DONE]`,
		"",
	}, "\n\n")

	flow := types.RawFlow{
		ID:          "sse-tc",
		Method:      "POST",
		URL:         "https://api.openai.com/v1/chat/completions",
		ReqBody:     []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"weather in SF?"}],"stream":true}`),
		RespHeaders: http.Header{"Content-Type": []string{"text/event-stream"}},
		RespBody:    []byte(body),
	}

	ev := Parse(flow)

	assert.Equal(t, "openai_chat", ev.Kind)
	assert.True(t, ev.IsStreamed)
	assert.Equal(t, "gpt-4o", ev.Model)
	require.Len(t, ev.Tools, 1)
	assert.Equal(t, "call_x", ev.Tools[0].ID)
	assert.Equal(t, "get_weather", ev.Tools[0].Name)
	assert.Equal(t, "SF", ev.Tools[0].Input["city"], "arguments concatenated across deltas and parsed")
	assert.Equal(t, 12, ev.Usage.InputTokens)
	assert.Equal(t, 7, ev.Usage.OutputTokens)
}

func TestParseOpenAIChatStreamingToolCallsAreScopedPerChoice(t *testing.T) {
	// With n > 1, OpenAI scopes delta.tool_calls[*].index to each
	// choice.index. Both choices can stream tool index 0 without referring to
	// the same tool call.
	body := strings.Join([]string{
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":1,"delta":{"tool_calls":[{"index":0,"id":"call_choice_1","type":"function","function":{"name":"lookup_city","arguments":""}}]}},{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_choice_0","type":"function","function":{"name":"lookup_weather","arguments":""}}]}}]}`,
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":1,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"NYC\"}"}}]}},{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"SF\"}"}}]}}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")

	flow := types.RawFlow{
		ID:          "sse-tc-n2",
		Method:      "POST",
		URL:         "https://api.openai.com/v1/chat/completions",
		ReqBody:     []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"weather"}],"stream":true,"n":2}`),
		RespHeaders: http.Header{"Content-Type": []string{"text/event-stream"}},
		RespBody:    []byte(body),
	}

	ev := Parse(flow)

	require.Len(t, ev.Tools, 2)
	assert.Equal(t, "call_choice_0", ev.Tools[0].ID, "choice 0 should emit before choice 1")
	assert.Equal(t, "lookup_weather", ev.Tools[0].Name)
	assert.Equal(t, "SF", ev.Tools[0].Input["city"])
	assert.Equal(t, "call_choice_1", ev.Tools[1].ID)
	assert.Equal(t, "lookup_city", ev.Tools[1].Name)
	assert.Equal(t, "NYC", ev.Tools[1].Input["city"])
}

func TestParseOpenAIChatStreamingSkipsNegativeToolCallIndexes(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":-1,"id":"call_bad_tool","type":"function","function":{"name":"bad_tool","arguments":"{\"city\":\"LA\"}"}}]}},{"index":-1,"delta":{"tool_calls":[{"index":0,"id":"call_bad_choice","type":"function","function":{"name":"bad_choice","arguments":"{\"city\":\"SEA\"}"}}]}}]}`,
		`data: {"id":"chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_good","type":"function","function":{"name":"good_tool","arguments":"{\"city\":\"SF\"}"}}]}}]}`,
		`data: [DONE]`,
		"",
	}, "\n\n")

	flow := types.RawFlow{
		ID:          "sse-negative-tc",
		Method:      "POST",
		URL:         "https://api.openai.com/v1/chat/completions",
		ReqBody:     []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"weather"}],"stream":true}`),
		RespHeaders: http.Header{"Content-Type": []string{"text/event-stream"}},
		RespBody:    []byte(body),
	}

	ev := Parse(flow)

	require.Len(t, ev.Tools, 1)
	assert.Equal(t, "call_good", ev.Tools[0].ID)
	assert.Equal(t, "good_tool", ev.Tools[0].Name)
	assert.Equal(t, "SF", ev.Tools[0].Input["city"])
}
