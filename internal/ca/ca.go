package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	rootValidity = 10 * 365 * 24 * time.Hour
	leafValidity = 365 * 24 * time.Hour
	keyBits      = 2048
)

// CA is a loaded root CA capable of signing leaf certs.
type CA struct {
	Dir  string
	Cert *x509.Certificate
	Key  *rsa.PrivateKey

	mu     sync.Mutex
	leaves map[string]*Leaf
}

// Leaf is a signed leaf certificate for a specific host.
type Leaf struct {
	Cert *x509.Certificate
	Key  *rsa.PrivateKey
}

// Ensure loads an existing CA from dir or generates a new one. Enforces 0o600 on key.pem.
func Ensure(dir string) (*CA, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if _, err := os.Stat(certPath); err == nil {
		// Validate key file is a regular file with 0600 permissions before loading.
		st, err := os.Lstat(keyPath)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("ca key %s must be a regular file (got mode %v)", keyPath, st.Mode())
		}
		// Windows does not honor unix file mode bits — Go's runtime persists 0o666
		// for any file written via os.WriteFile regardless of the requested perm.
		// Skip the assertion there; the test harness already does the same.
		if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
			return nil, fmt.Errorf("ca key %s must be mode 0600, got %v", keyPath, st.Mode().Perm())
		}
		return load(dir, certPath, keyPath)
	}

	return generate(dir, certPath, keyPath)
}

func generate(dir, certPath, keyPath string) (*CA, error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "agent-gate root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(rootValidity),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return nil, err
	}
	if err := writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), 0o600); err != nil {
		return nil, err
	}
	return &CA{Dir: dir, Cert: cert, Key: key, leaves: map[string]*Leaf{}}, nil
}

func load(dir, certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	cBlock, _ := pem.Decode(certPEM)
	if cBlock == nil {
		return nil, fmt.Errorf("decode cert pem: %s", certPath)
	}
	cert, err := x509.ParseCertificate(cBlock.Bytes)
	if err != nil {
		return nil, err
	}
	kBlock, _ := pem.Decode(keyPEM)
	if kBlock == nil {
		return nil, fmt.Errorf("decode key pem: %s", keyPath)
	}
	key, err := x509.ParsePKCS1PrivateKey(kBlock.Bytes)
	if err != nil {
		return nil, err
	}
	return &CA{Dir: dir, Cert: cert, Key: key, leaves: map[string]*Leaf{}}, nil
}

// SignLeaf signs (or returns a cached) leaf cert for host. Memoized in-memory.
func (c *CA) SignLeaf(host string) (*Leaf, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if l, ok := c.leaves[host]; ok {
		return l, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.Cert, &key.PublicKey, c.Key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	leaf := &Leaf{Cert: cert, Key: key}
	c.leaves[host] = leaf
	return leaf, nil
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}
