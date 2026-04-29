// internal/types/types_test.go
package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawFlowJSONRoundTrip(t *testing.T) {
	original := RawFlow{
		ID:          "01HXYZ",
		StartedAt:   time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC),
		EndedAt:     time.Date(2026, 4, 28, 10, 0, 1, 0, time.UTC),
		Method:      "POST",
		URL:         "https://api.anthropic.com/v1/messages",
		ReqHeaders:  map[string][]string{"Content-Type": {"application/json"}},
		ReqBody:     []byte(`{"model":"claude-opus-4-7"}`),
		RespStatus:  200,
		RespHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RespBody:    []byte(`{"id":"msg_1"}`),
		IsStreamed:  false,
		CaptureMode: "permissive",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded RawFlow
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.URL, decoded.URL)
	assert.Equal(t, original.ReqBody, decoded.ReqBody)
}

func TestParsedEventEmbedsRawFlow(t *testing.T) {
	pe := ParsedEvent{
		RawFlow: RawFlow{ID: "01HXYZ", URL: "https://api.anthropic.com/v1/messages"},
		Kind:    "anthropic_messages",
		Model:   "claude-opus-4-7",
	}
	assert.Equal(t, "01HXYZ", pe.ID)
	assert.Equal(t, "anthropic_messages", pe.Kind)
}
