package launcher

import (
	"crypto/sha256"
	"encoding/hex"

	"agent-gate/internal/ca"
)

// checkCATrusted returns ("", true) if our CA cert is in the system trust
// store, or (warningMessage, false) if not. Per design Q5 it never blocks
// startup — caller writes the warning to stderr and continues.
func checkCATrusted(c *ca.CA) (string, bool) {
	if c == nil || c.Cert == nil {
		return "", true
	}
	sum := sha256.Sum256(c.Cert.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	return checkCATrustedOS(fingerprint)
}
