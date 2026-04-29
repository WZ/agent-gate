package types

import (
	"net/http"
	"time"
)

// RawFlow is one HTTP exchange captured by the proxy. It is the input to the parser.
type RawFlow struct {
	ID           string      `json:"id"` // ULID
	StartedAt    time.Time   `json:"started_at"`
	EndedAt      time.Time   `json:"ended_at"`
	Method       string      `json:"method"`
	URL          string      `json:"url"`
	ReqHeaders   http.Header `json:"req_headers"`
	ReqBody      []byte      `json:"req_body"` // base64 in JSON
	RespStatus   int         `json:"resp_status"`
	RespHeaders  http.Header `json:"resp_headers"`
	RespBody     []byte      `json:"resp_body"`
	IsStreamed   bool        `json:"is_streamed"`
	CaptureMode  string      `json:"capture_mode"` // "airtight" | "permissive"
	Err          string      `json:"err,omitempty"`
	ClientConnID string      `json:"client_conn_id,omitempty"`
}

// Usage tracks Anthropic-style token accounting.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheRead    int `json:"cache_read_input_tokens"`
}

// ToolUse mirrors a tool_use block in an Anthropic Messages response.
type ToolUse struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResult mirrors a tool_result block in a subsequent Messages request.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"` // serialized; structured content stringified
	IsError   bool   `json:"is_error"`
}

// ParsedEvent embeds RawFlow and adds decoded fields. Anthropic-aware where we can; generic otherwise.
type ParsedEvent struct {
	RawFlow
	Kind        string       `json:"kind"` // "anthropic_messages" | "mcp_http" | "generic"
	SessionID   string       `json:"session_id"`
	Model       string       `json:"model"`
	Usage       Usage        `json:"usage"`
	Tools       []ToolUse    `json:"tools"`
	ToolResults []ToolResult `json:"tool_results"`
}

// Flag is one rule-evaluation result attached to a stored event.
type Flag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // "high" | "medium" | "low" | "info"
	Detail   string `json:"detail"`
}

// StoredEvent is what we persist: ParsedEvent + Flags.
type StoredEvent struct {
	ParsedEvent
	Flags []Flag `json:"flags"`
}
