//go:build darwin

package launcher

import (
	"os/exec"
	"strings"
)

func checkCATrustedOS(fingerprint string) (string, bool) {
	if matchInKeychain("/Library/Keychains/System.keychain", fingerprint) {
		return "", true
	}
	if matchInKeychain("", fingerprint) {
		return "", true
	}
	return "WARNING: agent-gate CA not in system trust store; agent will see TLS handshake failures.\n  Run `agent-gate cert install` once.", false
}

// matchInKeychain runs `security find-certificate -Z -a -c "agent-gate root"`
// against the given keychain (empty string = default user search list) and
// looks for the SHA-1 fingerprint line that contains our SHA-256 hash.
//
// `security find-certificate -Z` prints SHA-1 hashes by default and `-Z` adds
// the SHA-256 hash as a second line. Substring-match against the SHA-256.
func matchInKeychain(keychainPath, fingerprint string) bool {
	args := []string{"find-certificate", "-Z", "-a", "-c", "agent-gate root"}
	if keychainPath != "" {
		args = append(args, keychainPath)
	}
	out, err := exec.Command("security", args...).Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(fingerprint))
}
