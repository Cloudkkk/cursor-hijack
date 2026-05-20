// Package ca handles CA certificate generation and management.
package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CA manages the root CA and generates certificates for MITM.
type CA struct {
	certDir          string
	caCert           *x509.Certificate
	caKey            *ecdsa.PrivateKey
	certCache        sync.Map // map[string]*tls.Certificate
	caValidityYears  int
	certValidityDays int
}

// Options for creating a new CA.
type Options struct {
	CertDir          string
	CAValidityYears  int
	CertValidityDays int
}

// DefaultOptions returns default CA options.
func DefaultOptions() Options {
	return Options{
		CertDir:          "~/.cursor-tap",
		CAValidityYears:  100,  // 100 years - essentially permanent
		CertValidityDays: 3650, // 10 years for server certs
	}
}

// New creates or loads a CA from the specified directory.
func New(opts Options) (*CA, error) {
	certDir := expandPath(opts.CertDir)

	caValidityYears := opts.CAValidityYears
	if caValidityYears <= 0 {
		caValidityYears = 100
	}
	certValidityDays := opts.CertValidityDays
	if certValidityDays <= 0 {
		certValidityDays = 3650
	}

	ca := &CA{
		certDir:          certDir,
		caValidityYears:  caValidityYears,
		certValidityDays: certValidityDays,
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Join(certDir, "ca"), 0755); err != nil {
		return nil, fmt.Errorf("create ca dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(certDir, "certs"), 0755); err != nil {
		return nil, fmt.Errorf("create certs dir: %w", err)
	}

	// Try to load existing CA
	caPath := filepath.Join(certDir, "ca", "ca.crt")
	keyPath := filepath.Join(certDir, "ca", "ca.key")

	if fileExists(caPath) && fileExists(keyPath) {
		if err := ca.load(caPath, keyPath); err != nil {
			return nil, fmt.Errorf("load ca: %w", err)
		}
		return ca, nil
	}

	// Generate new CA
	if err := ca.generate(); err != nil {
		return nil, fmt.Errorf("generate ca: %w", err)
	}

	return ca, nil
}

// CertPath returns the path to the CA certificate.
func (ca *CA) CertPath() string {
	return filepath.Join(ca.certDir, "ca", "ca.crt")
}

// KeyPath returns the path to the CA private key.
func (ca *CA) KeyPath() string {
	return filepath.Join(ca.certDir, "ca", "ca.key")
}

// CertsDir returns the path to the certificates directory.
func (ca *CA) CertsDir() string {
	return filepath.Join(ca.certDir, "certs")
}

// generate creates a new CA certificate and private key.
func (ca *CA) generate() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return fmt.Errorf("marshal public key: %w", err)
	}
	subjectKeyId := sha256Sum(pubKeyBytes)[:20]

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "cursor-tap Root CA",
			Organization: []string{"cursor-tap Proxy CA"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().AddDate(ca.caValidityYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		SubjectKeyId:          subjectKeyId,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}

	if err := ca.saveCert(cert, ca.CertPath()); err != nil {
		return fmt.Errorf("save cert: %w", err)
	}
	if err := ca.saveKey(key, ca.KeyPath()); err != nil {
		return fmt.Errorf("save key: %w", err)
	}

	ca.caCert = cert
	ca.caKey = key

	return nil
}

// load loads an existing CA from disk.
func (ca *CA) load(certPath, keyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read cert: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("decode cert pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse cert: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return fmt.Errorf("decode key pem")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse key: %w", err)
	}

	ca.caCert = cert
	ca.caKey = key

	return nil
}

// saveCert saves a certificate to a PEM file.
func (ca *CA) saveCert(cert *x509.Certificate, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return pem.Encode(f, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
}

// saveKey saves a private key to a PEM file.
func (ca *CA) saveKey(key *ecdsa.PrivateKey, path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}

	return pem.Encode(f, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	})
}

// GetOrCreateCert returns a certificate for the given host, creating it if necessary.
func (ca *CA) GetOrCreateCert(host string) (*tls.Certificate, error) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	if cert, ok := ca.certCache.Load(host); ok {
		return cert.(*tls.Certificate), nil
	}

	certPath := filepath.Join(ca.certDir, "certs", host+".crt")
	keyPath := filepath.Join(ca.certDir, "certs", host+".key")

	if fileExists(certPath) && fileExists(keyPath) {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err == nil {
			ca.certCache.Store(host, &cert)
			return &cert, nil
		}
	}

	cert, err := ca.generateCert(host)
	if err != nil {
		return nil, err
	}

	if err := ca.saveCertKeyPair(cert, certPath, keyPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save cert for %s: %v\n", host, err)
	}

	ca.certCache.Store(host, cert)

	return cert, nil
}

// generateCert generates a new certificate for the given host.
func (ca *CA) generateCert(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	subjectKeyId := sha256Sum(pubKeyBytes)[:20]

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   host,
			Organization: []string{"cursor-tap Proxy"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, ca.certValidityDays),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		SubjectKeyId:          subjectKeyId,
		AuthorityKeyId:        ca.caCert.SubjectKeyId,
	}

	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.caCert, &key.PublicKey, ca.caKey)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	leafCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	cert := &tls.Certificate{
		Certificate: [][]byte{certDER, ca.caCert.Raw},
		PrivateKey:  key,
		Leaf:        leafCert,
	}

	return cert, nil
}

// saveCertKeyPair saves a TLS certificate and key to disk.
func (ca *CA) saveCertKeyPair(cert *tls.Certificate, certPath, keyPath string) error {
	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer certFile.Close()

	for _, certDER := range cert.Certificate {
		if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
			return err
		}
	}

	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyFile.Close()

	key := cert.PrivateKey.(*ecdsa.PrivateKey)
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}

	return pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

// CertCount returns the number of cached certificates.
func (ca *CA) CertCount() int {
	count := 0
	entries, _ := os.ReadDir(filepath.Join(ca.certDir, "certs"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".crt" {
			count++
		}
	}
	return count
}

// CleanCerts removes all cached server certificates.
func (ca *CA) CleanCerts() error {
	certsDir := filepath.Join(ca.certDir, "certs")
	entries, err := os.ReadDir(certsDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if err := os.Remove(filepath.Join(certsDir, e.Name())); err != nil {
			return err
		}
	}

	ca.certCache = sync.Map{}

	return nil
}

// Regenerate creates a new CA certificate and clears all cached certificates.
func (ca *CA) Regenerate() error {
	if err := ca.CleanCerts(); err != nil {
		return fmt.Errorf("clean certs: %w", err)
	}

	if err := ca.generate(); err != nil {
		return fmt.Errorf("generate ca: %w", err)
	}

	return nil
}

// Helper functions

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
