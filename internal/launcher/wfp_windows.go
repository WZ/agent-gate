//go:build windows

package launcher

import (
	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

// agentGateProviderID is the stable WFP provider GUID for agent-gate.
// 9b6dd7d3-1c9c-4b1a-a4ef-5b0c0a3a7c61
var agentGateProviderID = wf.ProviderID(windows.GUID{
	Data1: 0x9b6dd7d3,
	Data2: 0x1c9c,
	Data3: 0x4b1a,
	Data4: [8]byte{0xa4, 0xef, 0x5b, 0x0c, 0x0a, 0x3a, 0x7c, 0x61},
})

// agentGateSubLayerID is the stable WFP sublayer GUID under our provider.
// 9b6dd7d4-1c9c-4b1a-a4ef-5b0c0a3a7c61
var agentGateSubLayerID = wf.SublayerID(windows.GUID{
	Data1: 0x9b6dd7d4,
	Data2: 0x1c9c,
	Data3: 0x4b1a,
	Data4: [8]byte{0xa4, 0xef, 0x5b, 0x0c, 0x0a, 0x3a, 0x7c, 0x61},
})
