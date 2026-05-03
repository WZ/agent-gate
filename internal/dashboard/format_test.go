package dashboard

import (
	"html/template"
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

// ---- highlightBody / highlightJSON / highlightEventStream ----

func TestHighlightJSONColorsKeysAndValues(t *testing.T) {
	// html.EscapeString turns " into &#34; — that's correct (string content
	// must not be able to break out of the surrounding span).
	got := string(highlightJSON(`{"name":"claude","tokens":42,"ok":true,"extra":null}`))
	assert.Contains(t, got, `<span class="json-key">&#34;name&#34;</span>`)
	assert.Contains(t, got, `<span class="json-string">&#34;claude&#34;</span>`)
	assert.Contains(t, got, `<span class="json-key">&#34;tokens&#34;</span>`)
	assert.Contains(t, got, `<span class="json-number">42</span>`)
	assert.Contains(t, got, `<span class="json-bool">true</span>`)
	assert.Contains(t, got, `<span class="json-null">null</span>`)
	assert.Contains(t, got, `<span class="json-punct">{</span>`)
	assert.Contains(t, got, `<span class="json-punct">,</span>`)
	assert.Contains(t, got, `<span class="json-punct">:</span>`)
}

func TestHighlightJSONHandlesNegativeAndExponentNumbers(t *testing.T) {
	got := string(highlightJSON(`[-1.5, 2e10, -3.4E-7]`))
	assert.Contains(t, got, `<span class="json-number">-1.5</span>`)
	assert.Contains(t, got, `<span class="json-number">2e10</span>`)
	assert.Contains(t, got, `<span class="json-number">-3.4E-7</span>`)
}

func TestHighlightJSONEscapesHTMLInStrings(t *testing.T) {
	// A string containing < > & must be HTML-escaped so it can never break
	// out of the surrounding span or inject markup.
	got := string(highlightJSON(`{"x":"<script>alert(1)</script>"}`))
	assert.NotContains(t, got, `<script>`)
	assert.Contains(t, got, `&lt;script&gt;`)
}

func TestHighlightJSONHandlesEscapedQuoteInsideString(t *testing.T) {
	// `"a\"b"` is a 5-character string value containing a literal quote.
	got := string(highlightJSON(`{"k":"a\"b"}`))
	assert.Contains(t, got, `<span class="json-string">&#34;a\&#34;b&#34;</span>`)
	assert.Contains(t, got, `<span class="json-key">&#34;k&#34;</span>`)
}

func TestHighlightJSONPreservesIndentation(t *testing.T) {
	pretty := prettyJSON(`{"a":1,"b":[2,3]}`)
	got := string(highlightJSON(pretty))
	// the leading indentation between tokens is preserved verbatim
	assert.Contains(t, got, "</span>\n  <span")
	assert.Contains(t, got, "  <span class=\"json-key\">&#34;a&#34;</span>")
}

func TestHighlightEventStreamColorsFieldsAndJSON(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","ok":true}`,
	}, "\n")
	got := string(highlightEventStream(body))
	assert.Contains(t, got, `<span class="sse-field">event:</span> message_start`)
	assert.Contains(t, got, `<span class="sse-field">data:</span> <span class="json-punct">{</span>`)
	assert.Contains(t, got, `<span class="json-key">&#34;type&#34;</span>`)
	assert.Contains(t, got, `<span class="json-bool">true</span>`)
}

func TestHighlightBodyDispatchesByContentType(t *testing.T) {
	json := `{"a":1}`

	jh := http.Header{"Content-Type": []string{"application/json"}}
	assert.Contains(t, string(highlightBody(json, jh)), `class="json-key"`)

	sh := http.Header{"Content-Type": []string{"text/event-stream"}}
	assert.Contains(t, string(highlightBody("data: "+json, sh)), `class="sse-field"`)

	// Plain text is HTML-escaped only, no token spans.
	plain := http.Header{"Content-Type": []string{"text/plain"}}
	assert.Equal(t, "&lt;ok&gt;", string(highlightBody("<ok>", plain)))

	assert.Equal(t, template.HTML(""), highlightBody("", jh))
}
