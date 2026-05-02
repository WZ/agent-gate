package dashboard

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatBodyJSONIndents(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"application/json"}}
	got := formatBody(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`, headers)
	assert.Contains(t, got, "\n  \"model\": \"claude\"")
	assert.Contains(t, got, "\n      \"role\": \"user\"")
}

func TestFormatBodyJSONHandlesPlusJSONSuffix(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"application/vnd.api+json; charset=utf-8"}}
	got := formatBody(`{"a":1}`, headers)
	assert.Contains(t, got, "\n  \"a\": 1")
}

func TestFormatBodyJSONFallsBackOnInvalidJSON(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"application/json"}}
	body := `{"truncated": tru` // truncated capture
	got := formatBody(body, headers)
	assert.Equal(t, body, got)
}

func TestFormatBodyJSONPreservesRedactorMarkers(t *testing.T) {
	// Redactor wraps secrets in «REDACTED:CODE:prefix•••». The marker lives
	// inside the JSON string, so the document stays valid and indents cleanly.
	headers := http.Header{"Content-Type": []string{"application/json"}}
	body := `{"key":"«REDACTED:openai_key:sk-abc•••»"}`
	got := formatBody(body, headers)
	assert.Contains(t, got, "\n  \"key\": \"«REDACTED:openai_key:sk-abc•••»\"")
}

func TestFormatBodyEventStreamPrettyPrintsDataLines(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	body := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_01"}}`,
		"",
		"event: content_block_delta",
		`data: {"delta":{"text":"hello"}}`,
	}, "\n")
	got := formatBody(body, headers)
	assert.Contains(t, got, "data: {\n  \"type\": \"message_start\"")
	assert.Contains(t, got, "data: {\n  \"delta\": {\n    \"text\": \"hello\"")
	assert.Contains(t, got, "event: message_start")
}

func TestFormatBodyEventStreamLeavesNonJSONDataAlone(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	body := "data: ping\n"
	got := formatBody(body, headers)
	assert.Equal(t, "data: ping", got)
}

func TestFormatBodyPassThroughForOtherContentTypes(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"text/html"}}
	body := `<html><body><h1>hi</h1></body></html>`
	assert.Equal(t, body, formatBody(body, headers))
}

func TestFormatBodyEmptyReturnsEmpty(t *testing.T) {
	headers := http.Header{"Content-Type": []string{"application/json"}}
	assert.Equal(t, "", formatBody("", headers))
}

func TestFormatBodyMissingContentTypePassesThrough(t *testing.T) {
	body := `{"a":1}` // looks like JSON but no content-type — leave it
	assert.Equal(t, body, formatBody(body, http.Header{}))
}
