package parser

import (
	"agent-gate/internal/bodydecode"
	"agent-gate/internal/types"
)

// decodeRequestBody is a parser-local alias to bodydecode.Request. Kept for
// readability at parser call sites; the real implementation lives in
// internal/bodydecode so policy rules, the PII indexer, and the dashboard
// renderer share the same Content-Encoding handling.
func decodeRequestBody(flow *types.RawFlow) []byte {
	return bodydecode.Request(flow)
}
