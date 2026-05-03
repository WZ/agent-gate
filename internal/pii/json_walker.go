package pii

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
