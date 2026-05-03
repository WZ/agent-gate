package dashboard

import (
	"html/template"
	"net/http"
	"strings"
	"testing"

	"agent-gate/internal/pii"

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

func TestFormatBodyLeavesEventStreamUntouched(t *testing.T) {
	// SSE pretty-printing happens during highlight (per-event collapse owns
	// its own JSON layout). formatBody passes SSE through unchanged.
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	body := "event: message_start\ndata: {\"type\":\"start\"}"
	assert.Equal(t, body, formatBody(body, headers))
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
	got := string(highlightJSON(`{"name":"claude","tokens":42,"ok":true,"extra":null}`, nil))
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
	got := string(highlightJSON(`[-1.5, 2e10, -3.4E-7]`, nil))
	assert.Contains(t, got, `<span class="json-number">-1.5</span>`)
	assert.Contains(t, got, `<span class="json-number">2e10</span>`)
	assert.Contains(t, got, `<span class="json-number">-3.4E-7</span>`)
}

func TestHighlightJSONEscapesHTMLInStrings(t *testing.T) {
	// A string containing < > & must be HTML-escaped so it can never break
	// out of the surrounding span or inject markup.
	got := string(highlightJSON(`{"x":"<script>alert(1)</script>"}`, nil))
	assert.NotContains(t, got, `<script>`)
	assert.Contains(t, got, `&lt;script&gt;`)
}

func TestHighlightJSONHandlesEscapedQuoteInsideString(t *testing.T) {
	// `"a\"b"` is a 5-character string value containing a literal quote.
	got := string(highlightJSON(`{"k":"a\"b"}`, nil))
	assert.Contains(t, got, `<span class="json-string">&#34;a\&#34;b&#34;</span>`)
	assert.Contains(t, got, `<span class="json-key">&#34;k&#34;</span>`)
}

func TestHighlightJSONPreservesIndentation(t *testing.T) {
	pretty := prettyJSON(`{"a":1,"b":[2,3]}`)
	got := string(highlightJSON(pretty, nil))
	// the leading indentation between tokens is preserved verbatim
	assert.Contains(t, got, "</span>\n  <span")
	assert.Contains(t, got, "  <span class=\"json-key\">&#34;a&#34;</span>")
}

func TestHighlightEventStreamColorsFieldsAndJSON(t *testing.T) {
	// highlightEventStream owns SSE indent + highlight. Each data: payload
	// is pretty-printed inside its block, so the rendered JSON is multi-line.
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","ok":true}`,
	}, "\n")
	got := string(highlightEventStream(body))
	assert.Contains(t, got, `<span class="sse-field">event:</span> message_start`)
	assert.Contains(t, got, `<span class="sse-field">data:</span> <span class="json-punct">{</span>`)
	assert.Contains(t, got, `<span class="json-key">&#34;type&#34;</span>`)
	assert.Contains(t, got, `<span class="json-bool">true</span>`)
	// indented (multi-line)
	assert.Contains(t, got, "<span class=\"json-punct\">{</span>\n  ")
}

func TestHighlightEventStreamWrapsEachEventInDetails(t *testing.T) {
	// Three events separated by blank-line boundaries → three <details>.
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start"}`,
		``,
		`event: content_block_delta`,
		`data: {"delta":{"text":"hi"}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
	}, "\n")
	got := string(highlightEventStream(body))
	assert.Equal(t, 3, strings.Count(got, `<details class="sse-block" open>`),
		"should emit one details element per event boundary")
	assert.Equal(t, 3, strings.Count(got, `</details>`))
	// Event names land inside the summary element, not the body.
	assert.Contains(t, got, `<summary><span class="sse-field">event:</span> message_start</summary>`)
	assert.Contains(t, got, `<summary><span class="sse-field">event:</span> content_block_delta</summary>`)
	assert.Contains(t, got, `<summary><span class="sse-field">event:</span> message_stop</summary>`)
}

func TestHighlightEventStreamHandlesAnonymousEvent(t *testing.T) {
	// SSE blocks without an explicit `event:` field default to "message".
	body := `data: {"x":1}`
	got := string(highlightEventStream(body))
	assert.Contains(t, got, `<details class="sse-block" open>`)
	assert.Contains(t, got, `<span class="sse-event-anonymous">message</span>`)
	assert.Contains(t, got, `<span class="json-key">&#34;x&#34;</span>`)
}

func TestHighlightEventStreamPreservesIDField(t *testing.T) {
	body := strings.Join([]string{
		`event: ping`,
		`id: 42`,
		`data: {"ok":true}`,
	}, "\n")
	got := string(highlightEventStream(body))
	assert.Contains(t, got, `<span class="sse-field">id:</span> 42`)
}

// ---- PII highlighting (inside json-string tokens) ----

func TestHighlightJSONFlagsEmailInsideString(t *testing.T) {
	body := `{"contact":"alice@example.com"}`
	matches := pii.Find([]byte(body), pii.KindJSON)
	got := string(highlightJSON(body, matches))
	// The email span lives INSIDE the json-string span — both classes present.
	assert.Contains(t, got, `<span class="json-string">`)
	assert.Contains(t, got, `<span class="pii pii-identifying pii-email" title="email">alice@example.com</span>`)
}

func TestHighlightJSONFlagsMultiplePIIKindsInSameString(t *testing.T) {
	body := `{"x":"contact alice@example.com from 192.168.1.1"}`
	matches := pii.Find([]byte(body), pii.KindJSON)
	got := string(highlightJSON(body, matches))
	assert.Contains(t, got, `<span class="pii pii-identifying pii-email" title="email">alice@example.com</span>`)
	assert.Contains(t, got, `<span class="pii pii-identifying pii-ipv4" title="ipv4">192.168.1.1</span>`)
}

func TestHighlightJSONNoPIIInKeys(t *testing.T) {
	// Keys are emitted with json-key class, never with pii spans.
	body := `{"alice@example.com":"value"}`
	matches := pii.Find([]byte(body), pii.KindJSON)
	got := string(highlightJSON(body, matches))
	assert.Contains(t, got, `<span class="json-key">&#34;alice@example.com&#34;</span>`)
	assert.NotContains(t, got, `class="pii pii-identifying pii-email"`)
}

func TestHighlightJSONSensitiveTierUsesDestructiveClass(t *testing.T) {
	body := `{"ssn":"123-45-6789"}`
	matches := pii.Find([]byte(body), pii.KindJSON)
	got := string(highlightJSON(body, matches))
	assert.Contains(t, got, `<span class="pii pii-sensitive pii-ssn" title="ssn">123-45-6789</span>`)
}

func TestSummarizePIIFromMatches(t *testing.T) {
	matches := []pii.Match{
		{Code: "email", Tier: pii.TierIdentifying, Source: pii.SourceRegex, Start: 0, End: 10},
		{Code: "email", Tier: pii.TierIdentifying, Source: pii.SourceRegex, Start: 11, End: 21},
		{Code: "ssn", Tier: pii.TierSensitive, Source: pii.SourceRegex, Start: 22, End: 33},
	}
	got := SummarizePII(matches)
	want := []PIICount{
		{Code: "ssn", Label: "SSN", Tier: "sensitive", Count: 1},
		{Code: "email", Label: "Email", Tier: "identifying", Count: 2},
	}
	assert.Equal(t, want, got)
}

func TestSummarizePIIReturnsNilForEmptyMatches(t *testing.T) {
	assert.Nil(t, SummarizePII(nil))
	assert.Nil(t, SummarizePII([]pii.Match{}))
}

func TestHasSensitivePII(t *testing.T) {
	assert.True(t, HasSensitivePII([]PIICount{
		{Code: "ssn", Tier: "sensitive", Count: 1},
		{Code: "email", Tier: "identifying", Count: 2},
	}))
	assert.False(t, HasSensitivePII([]PIICount{
		{Code: "email", Tier: "identifying", Count: 2},
	}))
	assert.False(t, HasSensitivePII(nil))
}

func TestHighlightBodyDispatchesByContentType(t *testing.T) {
	json := `{"a":1}`

	jh := http.Header{"Content-Type": []string{"application/json"}}
	assert.Contains(t, string(highlightBody(json, jh, nil)), `class="json-key"`)

	sh := http.Header{"Content-Type": []string{"text/event-stream"}}
	assert.Contains(t, string(highlightBody("data: "+json, sh, nil)), `class="sse-field"`)

	// Plain text is HTML-escaped only, no token spans.
	plain := http.Header{"Content-Type": []string{"text/plain"}}
	assert.Equal(t, "&lt;ok&gt;", string(highlightBody("<ok>", plain, nil)))

	assert.Equal(t, template.HTML(""), highlightBody("", jh, nil))
}
