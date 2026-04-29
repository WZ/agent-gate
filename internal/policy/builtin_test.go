package policy

import (
	"path/filepath"
	"strings"
	"testing"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkengine(t *testing.T, rules ...Rule) (*Engine, *allowlist.Allowlist) {
	t.Helper()
	dir := t.TempDir()
	al, err := allowlist.Load(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	di, err := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	return NewEngine(al, di, rules...), al
}

func TestHostNotAllowlistedFiresOnUnknownHost(t *testing.T) {
	dir := t.TempDir()
	al, _ := allowlist.Load(filepath.Join(dir, "a.txt"))
	di, _ := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, al.Add("api.anthropic.com"))
	e := NewEngine(al, di, NewHostNotAllowlistedRule(al))

	ev := &types.ParsedEvent{RawFlow: types.RawFlow{ID: "e1", URL: "https://evil.example.com/foo"}}
	flags := e.Evaluate(ev)
	require.Len(t, flags, 1)
	assert.Equal(t, "host_not_allowlisted", flags[0].Code)
	assert.Equal(t, "high", flags[0].Severity)
	assert.Contains(t, flags[0].Detail, "evil.example.com")
}

func TestHostNotAllowlistedSilentForKnownHost(t *testing.T) {
	dir := t.TempDir()
	al, _ := allowlist.Load(filepath.Join(dir, "a.txt"))
	di, _ := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, al.Add("api.anthropic.com"))
	e := NewEngine(al, di, NewHostNotAllowlistedRule(al))

	ev := &types.ParsedEvent{RawFlow: types.RawFlow{ID: "e2", URL: "https://api.anthropic.com/v1/messages"}}
	flags := e.Evaluate(ev)
	assert.Empty(t, flags)
}

func TestPermissiveCaptureFiresWhenModeIsPermissive(t *testing.T) {
	e, _ := mkengine(t, PermissiveCaptureRule{})
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "e", CaptureMode: "permissive"}})
	require.Len(t, flags, 1)
	assert.Equal(t, "permissive_capture", flags[0].Code)
	assert.Equal(t, "info", flags[0].Severity)
}

func TestPermissiveCaptureSilentInAirtight(t *testing.T) {
	e, _ := mkengine(t, PermissiveCaptureRule{})
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "e", CaptureMode: "airtight"}})
	assert.Empty(t, flags)
}

func TestSecretInRequestFiresOnAnthropicKey(t *testing.T) {
	e, _ := mkengine(t, SecretInRequestRule{})
	body := []byte(`{"prompt":"my key is sk-ant-` + strings.Repeat("a", 60) + `"}`)
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "e", ReqBody: body}})
	require.Len(t, flags, 1)
	assert.Equal(t, "secret_in_request", flags[0].Code)
	assert.Equal(t, "high", flags[0].Severity)
	assert.Contains(t, flags[0].Detail, "anthropic_key")
}

func TestSecretInRequestSilentOnInnocuousBody(t *testing.T) {
	e, _ := mkengine(t, SecretInRequestRule{})
	flags := e.Evaluate(&types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "e", ReqBody: []byte(`{"prompt":"hello world"}`)},
	})
	assert.Empty(t, flags)
}

func TestEnvInToolResultFiresOnDotEnvShape(t *testing.T) {
	e, _ := mkengine(t, EnvInToolResultRule{})
	ev := &types.ParsedEvent{ToolResults: []types.ToolResult{{
		ToolUseID: "tool-1",
		Content: `DATABASE_URL=postgres://x:y@host/db
API_KEY=abc123
SECRET_TOKEN=topsecret
DEBUG=1`,
	}}}
	flags := e.Evaluate(ev)
	require.Len(t, flags, 1)
	assert.Equal(t, "env_in_tool_result", flags[0].Code)
	assert.Equal(t, "high", flags[0].Severity)
}

func TestEnvInToolResultSilentBelowThreshold(t *testing.T) {
	e, _ := mkengine(t, EnvInToolResultRule{})
	ev := &types.ParsedEvent{ToolResults: []types.ToolResult{{
		ToolUseID: "tool-1",
		Content:   `KEY=value`,
	}}}
	assert.Empty(t, e.Evaluate(ev))
}

func TestOversizedRequestFiresAbove5MB(t *testing.T) {
	e, _ := mkengine(t, OversizedRequestRule{Limit: 5 << 20})
	body := make([]byte, (5<<20)+1)
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "e", ReqBody: body}})
	require.Len(t, flags, 1)
	assert.Equal(t, "oversized_request", flags[0].Code)
	assert.Equal(t, "medium", flags[0].Severity)
}

func TestOversizedResponseFiresAbove5MB(t *testing.T) {
	e, _ := mkengine(t, OversizedResponseRule{Limit: 5 << 20})
	body := make([]byte, (5<<20)+1)
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "e", RespBody: body}})
	require.Len(t, flags, 1)
	assert.Equal(t, "oversized_response", flags[0].Code)
	assert.Equal(t, "low", flags[0].Severity)
}

func TestUnknownMCPEndpointFires(t *testing.T) {
	e, _ := mkengine(t, NewUnknownMCPEndpointRule(map[string]struct{}{"mcp.known.com": {}}))
	flags := e.Evaluate(&types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "e",
			URL:         "https://strange.example.com/sse",
			RespHeaders: map[string][]string{"Content-Type": {"text/event-stream"}},
		},
	})
	require.Len(t, flags, 1)
	assert.Equal(t, "unknown_mcp_endpoint", flags[0].Code)
	assert.Equal(t, "medium", flags[0].Severity)
}

func TestUnknownMCPEndpointSilentForNonSSE(t *testing.T) {
	e, _ := mkengine(t, NewUnknownMCPEndpointRule(nil))
	flags := e.Evaluate(&types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "e",
			URL:         "https://strange.example.com/api",
			RespHeaders: map[string][]string{"Content-Type": {"application/json"}},
		},
	})
	assert.Empty(t, flags)
}

func TestParseErrorFiresWhenRawFlowErrIsSet(t *testing.T) {
	e, _ := mkengine(t, ParseErrorRule{})
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{Err: "decode failed: ..."}})
	require.Len(t, flags, 1)
	assert.Equal(t, "parse_error", flags[0].Code)
	assert.Equal(t, "info", flags[0].Severity)
}
