package parser

import "agent-gate/internal/types"

func GenericFallback(flow types.RawFlow) types.ParsedEvent {
	ev := types.ParsedEvent{
		RawFlow: flow,
		Kind:    "generic",
	}
	parseGeneric(&ev)
	return ev
}

func parseGeneric(ev *types.ParsedEvent) {
	// No structured decoding — generic flows just retain RawFlow fields.
	// SessionID falls back to ClientConnID; better than nothing.
	if ev.SessionID == "" {
		ev.SessionID = ev.RawFlow.ClientConnID
	}
}
