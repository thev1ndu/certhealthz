//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/cmd"
	"github.com/thev1ndu/certhealthz/pkg/output"
)

// generateRSACert creates a self-signed leaf certificate with an
// undersized RSA key, to drive the scan pipeline's weak-crypto
// classification through the real code path instead of asserting against
// pkg/certmanager's unit-tested helpers directly.
func generateRSACert(t testing.TB, host string, bits int, notAfter time.Time) generatedCert {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("generating %d-bit RSA key: %v", bits, err)
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generating serial: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		DNSNames:     []string{host},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("building tls.Certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing generated certificate: %v", err)
	}

	return generatedCert{Leaf: leaf, CertPEM: certPEM, KeyPEM: keyPEM, TLS: tlsCert}
}

// TestWeakCryptoClassificationEndToEnd drives a real scan against a Secret
// backed by an undersized RSA key and asserts it comes back "weak-crypto",
// while a normal ECDSA fixture cert alongside it stays "ok" — confirming
// the classification only fires for the actually-weak cert, not every
// secret in the scan.
func TestWeakCryptoClassificationEndToEnd(t *testing.T) {
	ctx := context.Background()

	healthyCert := generateCert(t, "healthy.example.com", time.Now().Add(90*24*time.Hour))
	weakCert := generateRSACert(t, "weak.example.com", 1024, time.Now().Add(90*24*time.Hour))

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(
		newTLSSecret("healthy-tls", healthyCert),
		newTLSSecret("weak-tls", weakCert),
	)
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	byName := make(map[string]output.Row, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}

	if r, ok := byName["weak-tls"]; !ok || r.Status != "weak-crypto" {
		t.Errorf("expected weak-tls to be classified weak-crypto, got %+v (ok=%v)", r, ok)
	}
	if r, ok := byName["healthy-tls"]; !ok || r.Status != "ok" {
		t.Errorf("expected healthy-tls to stay ok, got %+v (ok=%v)", r, ok)
	}
}
