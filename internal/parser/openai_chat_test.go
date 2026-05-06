package parser

import (
	"net/http"
	"testing"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatMatch(t *testing.T) {
	p := OpenAIChat{}

	flow := loadFlow(t, "../../testdata/flows/openai/chat_completions_nonstreaming.json")
	assert.True(t, p.Match(&flow), "should match captured fazai/litellm chat completion")

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
		URL:     "https://api.openai.com/v1/embeddings",
		ReqBody: []byte(`{"input":"hi","model":"text-embedding-3-small"}`),
	}), "embeddings endpoint is a different shape")
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

func TestParseOpenAIChatStreamingMarksFlag(t *testing.T) {
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
	assert.True(t, ev.IsStreamed, "SSE response must be flagged as streamed")
	// Streaming body decoding ships in a follow-up; for now request-side fields suffice.
	assert.Equal(t, "gpt-4o", ev.Model)
}
