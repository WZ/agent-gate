package parser

import (
	"testing"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatGPTBackendMatch(t *testing.T) {
	p := ChatGPTBackend{}
	for _, path := range []string{
		"../../testdata/flows/codex/codex_models_list.json",
		"../../testdata/flows/codex/codex_wham_apps_mcp.json",
		"../../testdata/flows/codex/codex_analytics_event.json",
	} {
		flow := loadFlow(t, path)
		assert.True(t, p.Match(&flow), path)
	}

	assert.False(t, p.Match(&types.RawFlow{URL: "https://api.anthropic.com/v1/messages"}))
	assert.False(t, p.Match(&types.RawFlow{URL: "https://api.openai.com/v1/chat/completions"}))
	assert.False(t, p.Match(&types.RawFlow{URL: "https://chatgpt.com/public-api/codex/models"}))
}

func TestParseChatGPTBackendModelsList(t *testing.T) {
	flow := loadFlow(t, "../../testdata/flows/codex/codex_models_list.json")

	ev := Parse(flow)

	assert.Equal(t, "chatgpt_backend", ev.Kind)
	assert.Equal(t, "models_list", ev.Endpoint)
	assert.Empty(t, ev.Model)
	assert.Equal(t, 7, ev.ItemCount)
	assert.Empty(t, ev.Tools)
}

func TestParseChatGPTBackendWhamAppsTools(t *testing.T) {
	flow := loadFlow(t, "../../testdata/flows/codex/codex_wham_apps_mcp.json")

	ev := Parse(flow)

	assert.Equal(t, "chatgpt_backend", ev.Kind)
	assert.Equal(t, "wham_apps", ev.Endpoint)
	assert.Equal(t, 92, ev.ItemCount)
	require.Len(t, ev.Tools, 3)
	assert.Equal(t, "github_add_comment_to_issue", ev.Tools[0].Name)
	assert.Equal(t, "github_add_issue_assignees", ev.Tools[1].Name)
	assert.Equal(t, "github_add_issue_labels", ev.Tools[2].Name)
}

func TestParseChatGPTBackendAnalyticsEvents(t *testing.T) {
	flow := loadFlow(t, "../../testdata/flows/codex/codex_analytics_event.json")

	ev := Parse(flow)

	assert.Equal(t, "chatgpt_backend", ev.Kind)
	assert.Equal(t, "analytics_events", ev.Endpoint)
	assert.Equal(t, 1, ev.ItemCount)
	assert.Empty(t, ev.Tools)
}

func TestParseChatGPTBackendConnectorsList(t *testing.T) {
	flow := types.RawFlow{
		ID:         "connectors",
		Method:     "GET",
		URL:        "https://chatgpt.com/backend-api/connectors/directory/list?external_logos=true",
		RespStatus: 200,
		RespBody:   []byte(`{"items":[{"id":"github"},{"id":"gmail"}]}`),
	}

	ev := Parse(flow)

	assert.Equal(t, "chatgpt_backend", ev.Kind)
	assert.Equal(t, "connectors_list", ev.Endpoint)
	assert.Equal(t, 2, ev.ItemCount)
}
