package networking

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// Local HTTPS prefers the standard port for clean URLs. A normal desktop
	// process often cannot bind it, so the router transparently falls back.
	localHTTPSPort         = 443
	localHTTPSFallbackPort = 38474
)

// LocalCA is Draft's machine-local certificate authority. Its private key is
// kept in Draft's per-user configuration directory; only its public root is
// installed in the OS trust store.
type LocalCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	mu   sync.Mutex
	leaf map[string]*tlsCertificate
}

// tlsCertificate avoids exposing crypto/tls in the CA API's callers while
// keeping certificate creation and cache ownership together.
type tlsCertificate struct{ certPEM, keyPEM []byte }

func localHTTPSDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "Draft", "local-https")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func localCARootPath() (string, error) {
	dir, err := localHTTPSDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "root-ca.pem"), nil
}

func loadOrCreateLocalCA() (*LocalCA, string, error) {
	dir, err := localHTTPSDir()
	if err != nil {
		return nil, "", err
	}
	certPath, keyPath := filepath.Join(dir, "root-ca.pem"), filepath.Join(dir, "root-ca-key.pem")
	certBytes, certErr := os.ReadFile(certPath)
	keyBytes, keyErr := os.ReadFile(keyPath)
	if certErr == nil && keyErr == nil {
		cert, key, err := parseLocalCA(certBytes, keyBytes)
		if err == nil {
			return &LocalCA{cert: cert, key: key, leaf: make(map[string]*tlsCertificate)}, certPath, nil
		}
	}
	if (certErr == nil) != (keyErr == nil) {
		return nil, "", fmt.Errorf("Draft local HTTPS CA files are incomplete; remove %s to recreate them", dir)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generate local HTTPS CA key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Draft Local HTTPS CA", Organization: []string{"Draft"}}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, "", fmt.Errorf("create local HTTPS CA: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, "", err
	}
	return &LocalCA{cert: cert, key: key, leaf: make(map[string]*tlsCertificate)}, certPath, nil
}

func parseLocalCA(certBytes, keyBytes []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certBlock, _ := pem.Decode(certBytes)
	keyBlock, _ := pem.Decode(keyBytes)
	if certBlock == nil || keyBlock == nil {
		return nil, nil, fmt.Errorf("invalid PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	if !cert.IsCA {
		return nil, nil, fmt.Errorf("certificate is not a CA")
	}
	return cert, key, nil
}

func (ca *LocalCA) rootSHA1() string { return fmt.Sprintf("%X", sha1.Sum(ca.cert.Raw)) }

func (ca *LocalCA) certificatePEM(host string) ([]byte, []byte, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return nil, nil, fmt.Errorf("empty TLS server name")
	}
	ca.mu.Lock()
	defer ca.mu.Unlock()
	if cached := ca.leaf[host]; cached != nil {
		return cached.certPEM, cached.keyPEM, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host, Organization: []string{"Draft Local HTTPS"}}, DNSNames: []string{host}, NotBefore: now.Add(-time.Hour), NotAfter: minTime(ca.cert.NotAfter, now.AddDate(0, 3, 0)), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	certPEM, keyPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	ca.leaf[host] = &tlsCertificate{certPEM: certPEM, keyPEM: keyPEM}
	return certPEM, keyPEM, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
