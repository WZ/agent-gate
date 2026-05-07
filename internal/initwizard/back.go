package initwizard

import "errors"

// ErrPromptBack is returned by a Prompter method when the user asks to go
// back to the previous wizard step. The runner catches it and re-invokes
// the prior stage instead of advancing.
//
// Welcome has no prior stage; if a back signal somehow reaches it the
// runner just keeps showing welcome (no-op).
var ErrPromptBack = errors.New("initwizard: user requested previous step")
