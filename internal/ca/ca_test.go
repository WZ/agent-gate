package ca

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureCreatesNewCAOnFirstRun(t *testing.T) {
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
	dir := t.TempDir()
	first, err := Ensure(dir)
	require.NoError(t, err)
	second, err := Ensure(dir)
	require.NoError(t, err)
	assert.Equal(t, first.Cert.SerialNumber, second.Cert.SerialNumber)
}

func TestEnsureRejectsBadKeyPerms(t *testing.T) {
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
