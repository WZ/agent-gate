package pii

import "regexp"

var (
	// dateLikeRegex accepts ISO-8601 (YYYY-MM-DD) and slash-separated date
	// shapes (M/D/YY through MM/DD/YYYY). Strict enough to reject "n/a"
	// and free-text strings.
	dateLikeRegex = regexp.MustCompile(`^\d{4}-\d{1,2}-\d{1,2}$|^\d{1,2}/\d{1,2}/\d{2,4}$`)

	// ssnLikeRegex matches the two SSN shapes accepted with key context:
	// dashed canonical (xxx-xx-xxxx) or bare 9 digits.
	ssnLikeRegex = regexp.MustCompile(`^\d{3}-\d{2}-\d{4}$|^\d{9}$`)
)

// walkerToken is one structural element the walker found. start/end are
// byte offsets in the original body and exclude the surrounding quotes
// for string and key tokens.
type walkerToken struct {
	kind  tokenKind
	start int
	end   int
}

type tokenKind int

const (
	tokKey tokenKind = iota
	tokString
	tokNumber
)

// walkTokens scans body and returns every key, string value, and number
// value it can identify, in order. The walker is intentionally permissive
// and stops emitting at the first byte it cannot classify — partial
// results are useful for truncated bodies.
func walkTokens(body []byte) []walkerToken {
	w := &walker{body: body, i: 0, n: len(body)}
	w.walkValue()
	return w.tokens
}

type walker struct {
	body   []byte
	i, n   int
	tokens []walkerToken
}

// walkValue parses one JSON value at the current position. Returns true
// on successful parse, false if the walker had to bail (e.g., malformed
// input). On bail, already-emitted tokens are preserved.
func (w *walker) walkValue() bool {
	w.skipWhitespace()
	if w.i >= w.n {
		return false
	}
	c := w.body[w.i]
	switch {
	case c == '{':
		return w.walkObject()
	case c == '[':
		return w.walkArray()
	case c == '"':
		return w.walkString(false)
	case c == '-' || (c >= '0' && c <= '9'):
		return w.walkNumber()
	case c == 't' || c == 'f' || c == 'n':
		return w.walkLiteral()
	}
	// Unknown byte — bail.
	return false
}

func (w *walker) walkObject() bool {
	if w.i >= w.n || w.body[w.i] != '{' {
		return false
	}
	w.i++
	w.skipWhitespace()
	if w.i < w.n && w.body[w.i] == '}' {
		w.i++
		return true
	}
	for w.i < w.n {
		w.skipWhitespace()
		if w.i >= w.n || w.body[w.i] != '"' {
			return false
		}
		if !w.walkString(true) {
			return false
		}
		w.skipWhitespace()
		if w.i >= w.n || w.body[w.i] != ':' {
			return false
		}
		w.i++
		if !w.walkValue() {
			return false
		}
		w.skipWhitespace()
		if w.i >= w.n {
			return false
		}
		switch w.body[w.i] {
		case ',':
			w.i++
		case '}':
			w.i++
			return true
		default:
			return false
		}
	}
	return false
}

func (w *walker) walkArray() bool {
	if w.i >= w.n || w.body[w.i] != '[' {
		return false
	}
	w.i++
	w.skipWhitespace()
	if w.i < w.n && w.body[w.i] == ']' {
		w.i++
		return true
	}
	for w.i < w.n {
		if !w.walkValue() {
			return false
		}
		w.skipWhitespace()
		if w.i >= w.n {
			return false
		}
		switch w.body[w.i] {
		case ',':
			w.i++
		case ']':
			w.i++
			return true
		default:
			return false
		}
	}
	return false
}

// walkString consumes a JSON string literal at w.body[w.i] and emits a
// token. asKey controls whether the emitted token kind is tokKey or
// tokString. Token offsets cover the bytes BETWEEN the surrounding
// quotes (not including them); embedded \" escapes are part of the
// emitted range as raw bytes.
func (w *walker) walkString(asKey bool) bool {
	if w.i >= w.n || w.body[w.i] != '"' {
		return false
	}
	startInner := w.i + 1
	w.i++
	for w.i < w.n {
		c := w.body[w.i]
		if c == '\\' && w.i+1 < w.n {
			w.i += 2
			continue
		}
		if c == '"' {
			endInner := w.i
			w.i++
			kind := tokString
			if asKey {
				kind = tokKey
			}
			w.tokens = append(w.tokens, walkerToken{kind: kind, start: startInner, end: endInner})
			return true
		}
		w.i++
	}
	return false // unterminated string
}

func (w *walker) walkNumber() bool {
	start := w.i
	if w.body[w.i] == '-' {
		w.i++
	}
	for w.i < w.n {
		c := w.body[w.i]
		if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
			w.i++
			continue
		}
		break
	}
	if w.i == start {
		return false
	}
	w.tokens = append(w.tokens, walkerToken{kind: tokNumber, start: start, end: w.i})
	return true
}

func (w *walker) walkLiteral() bool {
	for _, lit := range []string{"true", "false", "null"} {
		if w.i+len(lit) <= w.n && string(w.body[w.i:w.i+len(lit)]) == lit {
			w.i += len(lit)
			return true
		}
	}
	return false
}

func (w *walker) skipWhitespace() {
	for w.i < w.n {
		c := w.body[w.i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			w.i++
			continue
		}
		return
	}
}

// findInJSON walks the body as JSON. It emits key-context matches plus
// free-text regex hits inside string values (via walkerMatches +
// regexMatchesInRange).
//
// If the walker bails before reaching EOF (truncated body, redactor
// markers in awkward spots, plain non-JSON bytes that snuck through
// content-type sniffing) the unparsed remainder is regex-swept so PII
// in malformed bodies isn't silently dropped — audit-completeness wins
// over the "keys not flagged" decision when JSON structure is broken.
func findInJSON(body []byte) []Match {
	w := &walker{body: body, i: 0, n: len(body)}
	w.walkValue()
	matches := walkerMatches(w.tokens, body)
	if w.i < w.n {
		matches = append(matches, regexMatchesInRange(body, w.i, w.n)...)
	}
	return matches
}

// walkerMatches turns a sequence of walker tokens into Match entries.
// Each string value is scanned twice: first for a key-context match
// (the pending key paired with this value), then for any free-text
// regex hits inside the value bytes. JSON keys are never scanned for
// regex, so an email-shaped key is not flagged.
func walkerMatches(tokens []walkerToken, body []byte) []Match {
	var out []Match
	var pending *sensitiveKey
	for _, tok := range tokens {
		switch tok.kind {
		case tokKey:
			if v, ok := sensitiveKeyLookup(string(body[tok.start:tok.end])); ok {
				pending = &v
			} else {
				pending = nil
			}
		case tokString:
			value := body[tok.start:tok.end]

			// Key-context match (any kind that opted in via shapeMatches).
			if pending != nil && shapeMatches(pending.Code, value) {
				out = append(out, Match{
					Code:   pending.Code,
					Tier:   pending.Tier,
					Source: SourceKey,
					Start:  tok.start,
					End:    tok.end,
				})
			}
			pending = nil

			// Free-text regex hits inside this string value.
			out = append(out, regexMatchesInRange(body, tok.start, tok.end)...)

		case tokNumber:
			// Numbers never trigger key-context detection in v1; documented
			// limitation. Reset pending so a subsequent key isn't paired with
			// the wrong value.
			pending = nil
		}
	}
	return out
}

// regexMatchesInRange runs the free-text Patterns set against body[start:end]
// and returns matches with offsets translated back into the original body
// coordinate space. credit_card hits go through Luhn just like FindAll.
func regexMatchesInRange(body []byte, start, end int) []Match {
	slice := body[start:end]
	var out []Match
	for _, p := range Patterns {
		for _, idx := range p.Regexp.FindAllIndex(slice, -1) {
			absStart := start + idx[0]
			absEnd := start + idx[1]
			if p.Code == "credit_card" {
				digits := stripNonDigits(string(body[absStart:absEnd]))
				if !Luhn(digits) {
					continue
				}
				out = append(out, Match{Code: p.Code, Tier: p.Tier, Source: SourceLuhn,
					Start: absStart, End: absEnd})
				continue
			}
			out = append(out, Match{Code: p.Code, Tier: p.Tier, Source: SourceRegex,
				Start: absStart, End: absEnd})
		}
	}
	return out
}

// shapeMatches is the per-kind value-shape check for key-context matches.
// In this initial commit we only accept name and address (any non-empty
// value); other kinds are added in subsequent tasks.
func shapeMatches(code string, value []byte) bool {
	if len(value) == 0 {
		return false
	}
	switch code {
	case "name", "address":
		return true
	case "dob":
		return dateLikeRegex.Match(value)
	case "phone":
		return countDigits(value) >= 7
	case "ssn":
		return ssnLikeRegex.Match(value)
	case "credit_card":
		return Luhn(stripNonDigits(string(value)))
	}
	return false
}

// countDigits returns the number of ASCII digit bytes in value.
func countDigits(value []byte) int {
	n := 0
	for _, c := range value {
		if c >= '0' && c <= '9' {
			n++
		}
	}
	return n
}
