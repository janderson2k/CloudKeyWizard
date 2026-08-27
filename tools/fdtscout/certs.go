package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"sync/atomic"
	"time"
)

// CertManager holds the currently-active TLS certificate behind an atomic pointer, so
// GenerateSelfSigned/InstallUploaded can swap the live certificate without restarting the HTTPS
// listener -- tls.Config.GetCertificate is called fresh on every handshake, so a swap takes
// effect on the very next connection.
type CertManager struct {
	current atomic.Pointer[tls.Certificate]
}

func NewCertManager() *CertManager {
	return &CertManager{}
}

func (m *CertManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := m.current.Load()
	if cert == nil {
		return nil, errors.New("no TLS certificate loaded")
	}
	return cert, nil
}

// LoadOrGenerate loads cert.pem/key.pem from disk if present, otherwise generates and persists a
// fresh self-signed certificate -- so the very first boot after install already serves HTTPS
// without any manual step.
func (m *CertManager) LoadOrGenerate() error {
	if _, err := os.Stat(CertFile); err == nil {
		if _, err := os.Stat(KeyFile); err == nil {
			return m.loadFromDisk()
		}
	}
	return m.GenerateSelfSigned()
}

func (m *CertManager) loadFromDisk() error {
	cert, err := tls.LoadX509KeyPair(CertFile, KeyFile)
	if err != nil {
		return fmt.Errorf("loading existing cert/key: %w", err)
	}
	m.current.Store(&cert)
	return nil
}

// GenerateSelfSigned creates a fresh self-signed cert covering every local IP this box currently
// has plus "localhost" -- good enough for a browser to connect to over HTTPS with a manually
// accepted security warning, which is the expected first-run experience for any self-signed
// admin console. Real trusted certs are what "install SSL cert" (InstallUploaded) is for.
func (m *CertManager) GenerateSelfSigned() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "fdtscout"
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostname, Organization: []string{"FDT.Scout (self-signed)"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		BasicConstraintsValid: true,
		DNSNames:     []string{hostname, "localhost"},
	}
	for _, ip := range localIPs() {
		template.IPAddresses = append(template.IPAddresses, ip)
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("creating self-signed certificate: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	if err := writeCertFiles(certPEM, keyPEM); err != nil {
		return err
	}
	return m.loadFromDisk()
}

// InstallUploaded validates a user-supplied cert+key pair actually parse and match each other
// before ever touching disk -- a bad upload should fail loudly in the UI, never leave the running
// HTTPS listener in a half-broken state.
func (m *CertManager) InstallUploaded(certPEM, keyPEM []byte) error {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("cert/key don't parse or don't match: %w", err)
	}
	if _, err := x509.ParseCertificate(cert.Certificate[0]); err != nil {
		return fmt.Errorf("cert doesn't parse as a valid X.509 certificate: %w", err)
	}
	if err := writeCertFiles(certPEM, keyPEM); err != nil {
		return err
	}
	m.current.Store(&cert)
	return nil
}

func (m *CertManager) Info() map[string]any {
	cert := m.current.Load()
	if cert == nil || len(cert.Certificate) == 0 {
		return map[string]any{"loaded": false}
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return map[string]any{"loaded": false, "error": err.Error()}
	}
	return map[string]any{
		"loaded":       true,
		"subject":      parsed.Subject.CommonName,
		"issuer":       parsed.Issuer.CommonName,
		"selfSigned":   parsed.Issuer.CommonName == parsed.Subject.CommonName,
		"notBefore":    parsed.NotBefore,
		"notAfter":     parsed.NotAfter,
		"dnsNames":     parsed.DNSNames,
	}
}

func writeCertFiles(certPEM, keyPEM []byte) error {
	if err := os.WriteFile(CertFile, certPEM, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(KeyFile, keyPEM, 0600); err != nil {
		return err
	}
	return nil
}

func localIPs() []net.IP {
	var out []net.IP
	out = append(out, net.ParseIP("127.0.0.1"))
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		out = append(out, ip)
	}
	return out
}
