//go:build !darwin && !linux && !windows

package launcher

func checkCATrustedOS(fingerprint string) (string, bool) {
	// Unknown platform: skip the check.
	_ = fingerprint
	return "", true
}
