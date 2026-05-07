package parser

import (
	"bytes"
	"io"
	"strings"

	"agent-gate/internal/types"

	"github.com/klauspost/compress/zstd"
)

// decodeRequestBody returns the raw bytes a parser should JSON-decode for
// flow.ReqBody, transparently handling Content-Encoding wrappers.
//
// codex (chatgpt.com) sends `Content-Encoding: zstd` on its
// /backend-api/codex/responses POSTs — the wire format is otherwise the
// OpenAI Responses API. Other gateways (api.openai.com, Azure, vLLM, …)
// don't wrap the request body, so the helper short-circuits to the original
// bytes when the header is empty or "identity".
//
// Decoding errors return nil; a parser's Match probe interprets that as
// "this isn't my flow," which is the right downstream behavior.
func decodeRequestBody(flow *types.RawFlow) []byte {
	if flow == nil || len(flow.ReqBody) == 0 {
		return flow.ReqBody
	}
	enc := strings.ToLower(strings.TrimSpace(flow.ReqHeaders.Get("Content-Encoding")))
	switch enc {
	case "", "identity":
		return flow.ReqBody
	case "zstd":
		dec, err := zstd.NewReader(bytes.NewReader(flow.ReqBody))
		if err != nil {
			return nil
		}
		defer dec.Close()
		out, err := io.ReadAll(dec)
		if err != nil {
			return nil
		}
		return out
	default:
		// Unknown encoding — let the caller fall through to its no-match path
		// rather than silently treating the bytes as JSON.
		return nil
	}
}
