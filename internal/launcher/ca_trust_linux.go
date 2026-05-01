//go:build linux

package launcher

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
)

// checkCATrustedOS scans well-known cert dirs for a PEM/DER cert whose SHA-256
// fingerprint matches `fingerprint`.
func checkCATrustedOS(fingerprint string) (string, bool) {
	candidates := []string{
		"/etc/ssl/certs/ca-certificates.crt", // Debian/Ubuntu bundle
		"/etc/pki/tls/certs/ca-bundle.crt",   // Fedora/RHEL bundle
		"/etc/ssl/certs",                     // Debian per-cert dir
		"/etc/pki/tls/certs",                 // RHEL per-cert dir
	}
	for _, c := range candidates {
		info, err := os.Stat(c)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if matchInBundle(c, fingerprint) {
				return "", true
			}
			continue
		}
		matched := false
		_ = filepath.WalkDir(c, func(p string, e os.DirEntry, _ error) error {
			if matched || e == nil || e.IsDir() {
				return nil
			}
			if matchInBundle(p, fingerprint) {
				matched = true
			}
			return nil
		})
		if matched {
			return "", true
		}
	}
	return "WARNING: agent-gate CA not in system trust store; agent will see TLS handshake failures.\n  Run `agent-gate cert install` once.", false
}

func matchInBundle(path, fingerprint string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for {
		block, rest := pem.Decode(b)
		if block == nil {
			break
		}
		b = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(cert.Raw)
		if strings.EqualFold(hex.EncodeToString(sum[:]), fingerprint) {
			return true
		}
	}
	return false
}
