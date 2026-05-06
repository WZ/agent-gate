package parser

import "agent-gate/internal/types"

// Parser decodes one concrete request/response shape.
type Parser interface {
	Match(*types.RawFlow) bool
	Parse(*types.RawFlow) (*types.ParsedEvent, error)
}

var registry = []Parser{
	AnthropicMessages{},
	ChatGPTBackend{},
	OpenAIChat{},
}

// ParseFlow dispatches flow through registered shape-specific parsers, falling
// back to a generic event when none match or all matching parsers fail.
func ParseFlow(flow types.RawFlow) types.ParsedEvent {
	return parseWithRegistry(flow, registry)
}

func parseWithRegistry(flow types.RawFlow, parsers []Parser) types.ParsedEvent {
	for _, p := range parsers {
		if !p.Match(&flow) {
			continue
		}
		ev, err := p.Parse(&flow)
		if err == nil && ev != nil {
			return *ev
		}
	}
	return GenericFallback(flow)
}
