package parser

import "agent-gate/internal/types"

func parseGeneric(ev *types.ParsedEvent) {
	// No structured decoding — generic flows just retain RawFlow fields.
	// SessionID falls back to ClientConnID; better than nothing.
	if ev.SessionID == "" {
		ev.SessionID = ev.RawFlow.ClientConnID
	}
}
