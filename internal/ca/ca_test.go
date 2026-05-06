package ca

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureCreatesNewCAOnFirstRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not honor unix file mode bits; this assertion is unix-only")
	}
	dir := t.TempDir()
	ca, err := Ensure(dir)
	require.NoError(t, err)
	assert.True(t, ca.Cert.IsCA)
	// Files exist with correct perms.
	st, err := os.Stat(filepath.Join(dir, "key.pem"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestEnsureReusesExistingCA(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Ensure() rejects keys not at 0600; Windows can't write 0600, so this path is unix-only")
	}
	dir := t.TempDir()
	first, err := Ensure(dir)
	require.NoError(t, err)
	second, err := Ensure(dir)
	require.NoError(t, err)
	assert.Equal(t, first.Cert.SerialNumber, second.Cert.SerialNumber)
}

func TestEnsureRejectsBadKeyPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Ensure() skips the 0600 assertion on Windows (file modes are ignored)")
	}
	dir := t.TempDir()
	_, err := Ensure(dir)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(dir, "key.pem"), 0o644))
	_, err = Ensure(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0600")
}

func TestSignLeafProducesValidCert(t *testing.T) {
	dir := t.TempDir()
	ca, err := Ensure(dir)
	require.NoError(t, err)

	leaf, err := ca.SignLeaf("api.anthropic.com")
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	_, err = leaf.Cert.Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: time.Now().Add(time.Minute),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	require.NoError(t, err)
	// Make sure we can use it as a tls.Certificate.
	tlsCert := tls.Certificate{Certificate: [][]byte{leaf.Cert.Raw}, PrivateKey: leaf.Key}
	assert.NotNil(t, tlsCert.PrivateKey)
}

func TestLeafSignerFuncReturnsTLSConfigForHost(t *testing.T) {
	dir := t.TempDir()
	ca, err := Ensure(dir)
	require.NoError(t, err)

	cfg, err := ca.LeafSignerFunc("chatgpt.com")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	require.Len(t, cfg.Certificates, 1)
	require.NotEmpty(t, cfg.Certificates[0].Certificate)
	assert.NotNil(t, cfg.Certificates[0].PrivateKey)

	leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "chatgpt.com", leaf.Subject.CommonName)
	assert.Contains(t, leaf.DNSNames, "chatgpt.com")

	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: time.Now().Add(time.Minute),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	require.NoError(t, err)
}

func TestEnsureRejectsKeySymlink(t *testing.T) {
	dir := t.TempDir()
	_, err := Ensure(dir)
	require.NoError(t, err)
	keyPath := filepath.Join(dir, "key.pem")
	target := filepath.Join(dir, "evil-target.pem")
	// Move the real key out, replace it with a symlink to a permissive copy.
	require.NoError(t, os.Rename(keyPath, target))
	require.NoError(t, os.Chmod(target, 0o644))
	require.NoError(t, os.Symlink(target, keyPath))
	_, err = Ensure(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "regular file")
}
