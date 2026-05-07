// Package bodydecode handles Content-Encoding wrappers on captured request
// bodies so every consumer (parser, policy rules, PII indexing, dashboard
// rendering) sees the same decoded bytes.
//
// codex on chatgpt.com sends `Content-Encoding: zstd` over an OpenAI
// Responses API JSON request. Without decoding here, secret-detection
// rules and PII indexing scan the compressed bytes and silently miss
// anything inside; the dashboard renders unreadable binary. Centralizing
// the decode here keeps that contract invariant across layers.
package bodydecode

import (
	"bytes"
	"io"
	"strings"

	"agent-gate/internal/types"

	"github.com/klauspost/compress/zstd"
)

// Keep decoded bodies under the same default in-memory cap as raw captures.
const maxDecodedBodyBytes = 8 << 20

// Request returns the request bytes safe to scan or render, transparently
// undoing any Content-Encoding wrapper.
//
//   - empty/identity → original bytes
//   - zstd → decompressed
//   - any other encoding → original bytes (unknown wrappers degrade
//     gracefully; we don't claim to decode what we don't understand)
//
// On a malformed zstd stream the function returns the original encoded
// bytes rather than nil — downstream consumers that can already cope with
// arbitrary bytes (PII regex, secret regex) keep working; ones that expect
// JSON will fail their own parse and fall through to no-match.
func Request(flow *types.RawFlow) []byte {
	if flow == nil || len(flow.ReqBody) == 0 {
		return nil
	}
	if flow.ReqHeaders == nil {
		return flow.ReqBody
	}
	enc := strings.ToLower(strings.TrimSpace(flow.ReqHeaders.Get("Content-Encoding")))
	switch enc {
	case "", "identity":
		return flow.ReqBody
	case "zstd":
		dec, err := zstd.NewReader(bytes.NewReader(flow.ReqBody))
		if err != nil {
			return flow.ReqBody
		}
		defer dec.Close()
		out, err := io.ReadAll(io.LimitReader(dec, maxDecodedBodyBytes+1))
		if err != nil {
			return flow.ReqBody
		}
		if len(out) > maxDecodedBodyBytes {
			return out[:maxDecodedBodyBytes]
		}
		return out
	default:
		return flow.ReqBody
	}
}
