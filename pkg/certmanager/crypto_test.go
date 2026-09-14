package certmanager

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

func TestWeakCryptoIssue(t *testing.T) {
	cases := []struct {
		name   string
		pubAlg string
		bits   int
		sigAlg string
		weak   bool
	}{
		{"rsa 1024 is weak", "RSA", 1024, "SHA256-RSA", true},
		{"rsa 2048 is fine", "RSA", 2048, "SHA256-RSA", false},
		{"rsa 4096 is fine", "RSA", 4096, "SHA256-RSA", false},
		{"sha1 signature is weak regardless of key", "RSA", 2048, "SHA1-RSA", true},
		{"md5 signature is weak", "RSA", 2048, "MD5-RSA", true},
		{"ecdsa 256 is fine (not RSA-bit-gated)", "ECDSA", 256, "ECDSA-SHA256", false},
		{"ed25519 is fine", "Ed25519", 256, "Ed25519", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			weak, issue := WeakCryptoIssue(tc.pubAlg, tc.bits, tc.sigAlg)
			if weak != tc.weak {
				t.Errorf("WeakCryptoIssue(%q, %d, %q) = weak=%v issue=%q, want weak=%v",
					tc.pubAlg, tc.bits, tc.sigAlg, weak, issue, tc.weak)
			}
			if weak && issue == "" {
				t.Error("expected a non-empty issue string when weak=true")
			}
		})
	}
}

// genCA creates a self-signed CA certificate and key.
func genCA(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing CA cert: %v", err)
	}
	return cert, key
}

// genLeaf creates a leaf certificate signed by ca (or self-signed if ca is nil).
func genLeaf(t *testing.T, cn string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating leaf key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	parent, signer := tmpl, key
	if ca != nil {
		parent, signer = ca, caKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatalf("creating leaf cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing leaf cert: %v", err)
	}
	return cert
}

func TestVerifyChain(t *testing.T) {
	ca, caKey := genCA(t, "test-ca")

	t.Run("self-signed leaf with no bundled chain is fine", func(t *testing.T) {
		selfSigned := genLeaf(t, "selfsigned.example.com", nil, nil)
		ok, issue := VerifyChain(selfSigned, nil)
		if !ok || issue != "" {
			t.Errorf("expected a self-signed leaf to be treated as intentional, got ok=%v issue=%q", ok, issue)
		}
	})

	t.Run("CA-issued leaf with the CA bundled verifies", func(t *testing.T) {
		leaf := genLeaf(t, "complete.example.com", ca, caKey)
		ok, issue := VerifyChain(leaf, []*x509.Certificate{ca})
		if !ok {
			t.Errorf("expected a complete leaf+CA bundle to verify, got issue=%q", issue)
		}
	})

	t.Run("CA-issued leaf with no chain bundled is a missing intermediate", func(t *testing.T) {
		leaf := genLeaf(t, "missing-intermediate.example.com", ca, caKey)
		ok, issue := VerifyChain(leaf, nil)
		if ok {
			t.Error("expected a non-self-signed leaf with an empty bundle to be flagged")
		}
		if issue == "" {
			t.Error("expected a non-empty issue explaining the missing link")
		}
	})

	t.Run("leaf bundled with the wrong CA fails", func(t *testing.T) {
		wrongCA, _ := genCA(t, "wrong-ca")
		leaf := genLeaf(t, "mismatched.example.com", ca, caKey)
		ok, issue := VerifyChain(leaf, []*x509.Certificate{wrongCA})
		if ok {
			t.Error("expected verification to fail when the bundled cert didn't actually sign the leaf")
		}
		if issue == "" {
			t.Error("expected a non-empty issue")
		}
	})
}
