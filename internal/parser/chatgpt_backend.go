package parser

import (
	"encoding/json"
	"net/url"
	"strings"

	"agent-gate/internal/types"
)

type ChatGPTBackend struct{}

func (ChatGPTBackend) Match(flow *types.RawFlow) bool {
	u, err := url.Parse(flow.URL)
	if err != nil || u == nil {
		return false
	}
	if u.Hostname() != "chatgpt.com" {
		return false
	}
	return chatGPTBackendEndpoint(u.Path) != ""
}

func (ChatGPTBackend) Parse(flow *types.RawFlow) (*types.ParsedEvent, error) {
	u, _ := url.Parse(flow.URL)
	endpoint := ""
	if u != nil {
		endpoint = chatGPTBackendEndpoint(u.Path)
	}
	ev := types.ParsedEvent{
		RawFlow:  *flow,
		Kind:     "chatgpt_backend",
		Endpoint: endpoint,
	}

	switch endpoint {
	case "models_list":
		ev.ItemCount = countArrayField(flow.RespBody, "models")
	case "wham_apps":
		ev.ItemCount, ev.Tools = parseWhamAppsTools(flow.RespBody)
	case "analytics_events":
		ev.ItemCount = analyticsEventCount(flow)
	case "connectors_list":
		ev.ItemCount = firstArrayCount(flow.RespBody, "items", "connectors")
	case "plugins_featured":
		ev.ItemCount = firstArrayCount(flow.RespBody, "items", "plugins", "featured")
	}

	return &ev, nil
}

func chatGPTBackendEndpoint(path string) string {
	switch {
	case strings.HasPrefix(path, "/backend-api/codex/models"):
		return "models_list"
	case strings.HasPrefix(path, "/backend-api/codex/analytics-events"):
		return "analytics_events"
	case strings.HasPrefix(path, "/backend-api/wham/apps"):
		return "wham_apps"
	case strings.HasPrefix(path, "/backend-api/connectors/directory/list"):
		return "connectors_list"
	case strings.HasPrefix(path, "/backend-api/plugins/"):
		return "plugins_featured"
	default:
		return ""
	}
}

func countArrayField(body []byte, field string) int {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(obj[field], &arr); err != nil {
		return 0
	}
	return len(arr)
}

func firstArrayCount(body []byte, fields ...string) int {
	for _, field := range fields {
		if n := countArrayField(body, field); n > 0 {
			return n
		}
	}
	return 0
}

func parseWhamAppsTools(body []byte) (int, []types.ToolUse) {
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, nil
	}
	tools := make([]types.ToolUse, 0, min(len(resp.Result.Tools), 3))
	for _, tool := range resp.Result.Tools {
		if len(tools) == 3 {
			break
		}
		if tool.Name == "" {
			continue
		}
		tools = append(tools, types.ToolUse{Name: tool.Name})
	}
	return len(resp.Result.Tools), tools
}

func analyticsEventCount(flow *types.RawFlow) int {
	var resp struct {
		TotalEvents int `json:"total_events"`
	}
	if err := json.Unmarshal(flow.RespBody, &resp); err == nil && resp.TotalEvents > 0 {
		return resp.TotalEvents
	}
	return countArrayField(flow.ReqBody, "events")
}
