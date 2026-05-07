package initwizard

import "errors"

// ErrPromptBack is returned by a Prompter method when the user asks to go
// back to the previous wizard step. The runner catches it and re-invokes
// the prior stage instead of advancing.
//
// Welcome has no prior stage; if a back signal somehow reaches it the
// runner just keeps showing welcome (no-op).
var ErrPromptBack = errors.New("initwizard: user requested previous step")

// stage names the runner's wizard state machine. Named type prevents
// accidental cross-type assignment (`stage := 3` won't compile) and
// signals to readers that this isn't just a plain int.
type stage int

const (
	stageWelcome stage = iota
	stageThreeList
	stageHostsLoop
	stageCustomHosts
	stagePolicySummary
	stageDone
)
