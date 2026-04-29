package parser

import (
	"encoding/json"
	"os"
	"testing"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFlow(t *testing.T, path string) types.RawFlow {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var f types.RawFlow
	require.NoError(t, json.Unmarshal(data, &f))
	return f
}

func TestParseAnthropicMessagesNonStreaming(t *testing.T) {
	flow := loadFlow(t, "../../testdata/flows/anthropic_messages_nonstreaming.json")
	ev := Parse(flow)

	assert.Equal(t, "anthropic_messages", ev.Kind)
	assert.Equal(t, "claude-opus-4-7", ev.Model)
	assert.Equal(t, 10, ev.Usage.InputTokens)
	assert.Equal(t, 5, ev.Usage.OutputTokens)
	assert.Equal(t, "sess-abc", ev.SessionID)
}

func TestParseGenericFallbackForUnknownHost(t *testing.T) {
	flow := types.RawFlow{
		ID:     "01H",
		Method: "GET",
		URL:    "https://example.com/foo",
	}
	ev := Parse(flow)
	assert.Equal(t, "generic", ev.Kind)
	assert.Empty(t, ev.Model)
}

func TestParseHandlesMalformedAnthropicBody(t *testing.T) {
	flow := types.RawFlow{
		ID:       "01H",
		Method:   "POST",
		URL:      "https://api.anthropic.com/v1/messages",
		ReqBody:  []byte("{not json"),
		RespBody: []byte("also not json"),
	}
	ev := Parse(flow)
	// We still tag it as anthropic_messages by host; usage stays zero-valued; no panic.
	assert.Equal(t, "anthropic_messages", ev.Kind)
	assert.Equal(t, 0, ev.Usage.InputTokens)
}
