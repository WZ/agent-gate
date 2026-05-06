package parser

import (
	"errors"
	"testing"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestRegistryDispatchUsesFirstSuccessfulParser(t *testing.T) {
	flow := types.RawFlow{ID: "01REG", URL: "https://example.com/path"}

	ev := parseWithRegistry(flow, []Parser{
		testRegistryParser{match: true, kind: "first"},
		testRegistryParser{match: true, kind: "second"},
	})

	assert.Equal(t, "first", ev.Kind)
}

func TestRegistryDispatchFallsBackWhenNoParserMatches(t *testing.T) {
	flow := types.RawFlow{
		ID:           "01REG",
		URL:          "https://example.com/path",
		ClientConnID: "conn-1",
	}

	ev := parseWithRegistry(flow, []Parser{
		testRegistryParser{match: false, kind: "ignored"},
	})

	assert.Equal(t, "generic", ev.Kind)
	assert.Equal(t, "conn-1", ev.SessionID)
}

func TestRegistryDispatchContinuesAfterParseError(t *testing.T) {
	flow := types.RawFlow{ID: "01REG", URL: "https://example.com/path"}

	ev := parseWithRegistry(flow, []Parser{
		testRegistryParser{match: true, err: errors.New("bad parse")},
		testRegistryParser{match: true, kind: "second"},
	})

	assert.Equal(t, "second", ev.Kind)
}

func TestRegistryDispatchFallsBackWhenMatchedParsersFail(t *testing.T) {
	flow := types.RawFlow{ID: "01REG", URL: "https://example.com/path"}

	ev := parseWithRegistry(flow, []Parser{
		testRegistryParser{match: true, err: errors.New("bad parse")},
	})

	assert.Equal(t, "generic", ev.Kind)
}

type testRegistryParser struct {
	match bool
	kind  string
	err   error
}

func (p testRegistryParser) Match(*types.RawFlow) bool {
	return p.match
}

func (p testRegistryParser) Parse(flow *types.RawFlow) (*types.ParsedEvent, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &types.ParsedEvent{
		RawFlow: *flow,
		Kind:    p.kind,
	}, nil
}
