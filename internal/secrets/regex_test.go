package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPatternsMatchKnownSecrets(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // expected pattern code that matches
	}{
		{"anthropic key", "sk-ant-" + repeat("a", 60), "anthropic_key"},
		{"openai key", "sk-" + repeat("A", 40), "openai_key"},
		{"slack bot", "xoxb-" + repeat("1", 20) + "-abc", "slack_token"},
		{"github personal", "ghp_" + repeat("A", 36), "github_token"},
		{"github oauth", "gho_" + repeat("A", 36), "github_token"},
		{"gitlab", "glsa_" + repeat("a", 30), "gitlab_token"},
		{"aws access key", "AKIA" + repeat("A", 16), "aws_access_key"},
		{"bearer", "Bearer " + repeat("a", 40), "bearer_token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matched := false
			for _, p := range Patterns {
				if p.Regexp.MatchString(c.text) {
					matched = true
					if c.want != "" {
						assert.Equal(t, c.want, p.Code, "matched wrong pattern")
					}
					break
				}
			}
			assert.True(t, matched, "no pattern matched %q", c.text)
		})
	}
}

func TestPatternsDoNotMatchInnocuousText(t *testing.T) {
	innocuous := []string{
		"hello world",
		"sk-short",  // too short for openai_key
		"ghp_short", // too short for github_token
		"AKIA",      // too short for aws_access_key
		"Bearer x",  // too short for bearer_token
	}
	for _, s := range innocuous {
		for _, p := range Patterns {
			assert.False(t, p.Regexp.MatchString(s),
				"pattern %s falsely matched %q", p.Code, s)
		}
	}
}

func TestFindAllReturnsLocations(t *testing.T) {
	text := "key=ghp_" + repeat("A", 36) + " and another sk-" + repeat("B", 40)
	locs := FindAll([]byte(text))
	assert.GreaterOrEqual(t, len(locs), 2)
	for _, l := range locs {
		assert.Less(t, l.Start, l.End)
	}
}

// helper to keep test cases compact
func repeat(s string, n int) string {
	out := make([]byte, n*len(s))
	for i := 0; i < n; i++ {
		copy(out[i*len(s):], s)
	}
	return string(out)
}
