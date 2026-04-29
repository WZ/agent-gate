package redactor

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactReplacesSecretsInline(t *testing.T) {
	in := `{"key":"sk-ant-abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH"}`
	out := Redact(in)
	assert.NotContains(t, out, "sk-ant-abcdefghij")
	assert.Contains(t, out, "«REDACTED:anthropic_key:")
}

func TestRedactPreservesNonSecretText(t *testing.T) {
	in := `Hello there, friend.`
	out := Redact(in)
	assert.Equal(t, in, out)
}

func TestRedactHeadersMasksAuthCookieAPIKey(t *testing.T) {
	h := http.Header{
		"Authorization": []string{"Bearer abc"},
		"Cookie":        []string{"session=xyz"},
		"X-Api-Key":     []string{"top-secret"},
		"Content-Type":  []string{"application/json"},
	}
	masked := RedactHeaders(h)
	assert.Equal(t, []string{"«REDACTED»"}, masked["Authorization"])
	assert.Equal(t, []string{"«REDACTED»"}, masked["Cookie"])
	assert.Equal(t, []string{"«REDACTED»"}, masked["X-Api-Key"])
	assert.Equal(t, []string{"application/json"}, masked["Content-Type"])
}

func TestRedactPreservesLineCount(t *testing.T) {
	in := strings.Repeat("sk-ant-abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH\n", 3)
	out := Redact(in)
	assert.Equal(t, strings.Count(in, "\n"), strings.Count(out, "\n"))
}
