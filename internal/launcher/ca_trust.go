package launcher

import "agent-gate/internal/ca"

// checkCATrusted returns ("", true) if our CA cert is in the system trust
// store, or (warningMessage, false) otherwise. Real per-platform impls land
// in Task 17; this stub assumes trusted (no warning) so Phase 1 can run.
func checkCATrusted(c *ca.CA) (string, bool) {
	return "", true
}
