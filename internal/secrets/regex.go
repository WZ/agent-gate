package secrets

import "regexp"

// Pattern is one named secret regex.
type Pattern struct {
	Code   string         // stable identifier, e.g. "anthropic_key"
	Regexp *regexp.Regexp // compiled regex
}

// Match is a single regex hit on a body.
type Match struct {
	PatternCode string
	Start, End  int // byte offsets within the input
}

// Patterns is the canonical set used by both the policy engine and the redactor.
// Order does not matter; FindAll deduplicates by offset.
var Patterns = []Pattern{
	{Code: "anthropic_key", Regexp: regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{40,}`)},
	{Code: "openai_key", Regexp: regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)},
	{Code: "slack_token", Regexp: regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{Code: "github_token", Regexp: regexp.MustCompile(`gh[psoru]_[A-Za-z0-9]{36}`)},
	{Code: "gitlab_token", Regexp: regexp.MustCompile(`glsa_[A-Za-z0-9]{20,}`)},
	{Code: "aws_access_key", Regexp: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{Code: "bearer_token", Regexp: regexp.MustCompile(`Bearer [A-Za-z0-9._\-]{20,}`)},
}

// FindAll returns every (deduplicated, ordered) match of any Pattern in body.
func FindAll(body []byte) []Match {
	var matches []Match
	for _, p := range Patterns {
		for _, idx := range p.Regexp.FindAllIndex(body, -1) {
			matches = append(matches, Match{PatternCode: p.Code, Start: idx[0], End: idx[1]})
		}
	}
	return matches
}
