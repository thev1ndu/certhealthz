package certmanager

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSoonestChainExpiry(t *testing.T) {
	t.Run("empty chain reports no expiry", func(t *testing.T) {
		soonest, subject := soonestChainExpiry(nil)
		if !soonest.IsZero() || subject != "" {
			t.Errorf("expected zero/empty for no chain, got soonest=%v subject=%q", soonest, subject)
		}
	})

	t.Run("picks the soonest-expiring chain cert, not the first", func(t *testing.T) {
		ca, caKey := genCA(t, "root-ca")
		intermediate := genLeaf(t, "intermediate-ca", ca, caKey)
		intermediate.NotAfter = time.Now().Add(48 * time.Hour)
		ca.NotAfter = time.Now().Add(24 * time.Hour)

		soonest, subject := soonestChainExpiry([]*x509.Certificate{intermediate, ca})
		if subject != "root-ca" {
			t.Errorf("expected the sooner-expiring root-ca to win, got subject=%q", subject)
		}
		if !soonest.Equal(ca.NotAfter) {
			t.Errorf("expected soonest=%v, got %v", ca.NotAfter, soonest)
		}
	})
}

// pemSecret builds a kubernetes.io/tls Secret carrying a fresh self-signed
// leaf certificate signed with its own generated key, for exercising
// parseSecret's public-key-hash computation end to end.
func pemSecret(t *testing.T, namespace, name, cn string) *corev1.Secret {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
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
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: certPEM},
	}
}

// pemSecretWithKey is pemSecret but signs with an explicit key, so two
// Secrets can be built sharing the same key material.
func pemSecretWithKey(t *testing.T, namespace, name, cn string, key *ecdsa.PrivateKey) *corev1.Secret {
	t.Helper()
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
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: certPEM},
	}
}

func TestParseSecretPublicKeyHash(t *testing.T) {
	t.Run("two secrets with independently generated keys hash differently", func(t *testing.T) {
		a, ok := parseSecret("c1", *pemSecret(t, "ns", "a", "a.example.com"))
		if !ok {
			t.Fatal("expected a to parse")
		}
		b, ok := parseSecret("c1", *pemSecret(t, "ns", "b", "b.example.com"))
		if !ok {
			t.Fatal("expected b to parse")
		}
		if a.PublicKeyHash == "" || b.PublicKeyHash == "" {
			t.Fatal("expected both secrets to have a non-empty public key hash")
		}
		if a.PublicKeyHash == b.PublicKeyHash {
			t.Error("expected independently generated keys to hash differently")
		}
	})

	t.Run("two secrets signed with the same key hash identically", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generating shared key: %v", err)
		}
		a, ok := parseSecret("c1", *pemSecretWithKey(t, "ns", "a", "a.example.com", key))
		if !ok {
			t.Fatal("expected a to parse")
		}
		b, ok := parseSecret("c1", *pemSecretWithKey(t, "ns", "b", "b.example.com", key))
		if !ok {
			t.Fatal("expected b to parse")
		}
		if a.PublicKeyHash != b.PublicKeyHash {
			t.Error("expected certs sharing a private key to hash identically")
		}
	})
}
