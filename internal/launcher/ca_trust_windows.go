//go:build windows

package launcher

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func checkCATrustedOS(fingerprint string) (string, bool) {
	if matchInStore(windows.CERT_SYSTEM_STORE_LOCAL_MACHINE, fingerprint) {
		return "", true
	}
	if matchInStore(windows.CERT_SYSTEM_STORE_CURRENT_USER, fingerprint) {
		return "", true
	}
	return "WARNING: agent-gate CA not in system trust store; agent will see TLS handshake failures.\n  Run `agent-gate cert install` once with admin.", false
}

func matchInStore(storeFlags uint32, fingerprint string) bool {
	storeName, err := windows.UTF16PtrFromString("Root")
	if err != nil {
		return false
	}
	store, err := windows.CertOpenStore(
		windows.CERT_STORE_PROV_SYSTEM,
		0,
		0,
		storeFlags,
		uintptr(unsafe.Pointer(storeName)))
	if err != nil {
		return false
	}
	defer windows.CertCloseStore(store, 0)
	var prev *windows.CertContext
	for {
		ctx, err := windows.CertEnumCertificatesInStore(store, prev)
		if err != nil || ctx == nil {
			return false
		}
		raw := unsafe.Slice((*byte)(unsafe.Pointer(ctx.EncodedCert)), ctx.Length)
		sum := sha256.Sum256(raw)
		if strings.EqualFold(hex.EncodeToString(sum[:]), fingerprint) {
			windows.CertFreeCertificateContext(ctx)
			return true
		}
		prev = ctx
	}
}
