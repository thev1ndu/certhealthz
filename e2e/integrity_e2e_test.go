//go:build e2e

package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/thev1ndu/certhealthz/cmd"
)

// selfSignedCertPEM creates a self-signed leaf certificate for host, signed
// by key, and returns its PEM encoding. Used here (rather than
// fixtures_test.go's generateCert, which always generates its own key) so
// two Secrets can be built sharing the same private key.
func selfSignedCertPEM(t *testing.T, host string, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generating serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(60 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func newTLSSecretPEM(namespace, name string, certPEM []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: certPEM},
	}
}

// TestPrivateKeyReuseEndToEnd seeds two unrelated Secrets (different
// namespaces/names/hosts) signed with the same private key, and asserts
// both "secret" rows get flagged with a Detail note naming the other —
// classic sign of a key copy-pasted between Secrets instead of freshly
// issued.
func TestPrivateKeyReuseEndToEnd(t *testing.T) {
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating shared key: %v", err)
	}
	secretA := newTLSSecretPEM("ns1", "reused-key-a", selfSignedCertPEM(t, "a.example.com", key))
	secretB := newTLSSecretPEM("ns2", "reused-key-b", selfSignedCertPEM(t, "b.example.com", key))

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(secretA, secretB)
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true, nil, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	details := make(map[string]string)
	for _, r := range rows {
		if r.Source == "secret" {
			details[r.Namespace+"/"+r.Name] = r.Detail
		}
	}

	detailA, ok := details["ns1/reused-key-a"]
	if !ok {
		t.Fatal("expected a secret row for ns1/reused-key-a")
	}
	if !strings.Contains(detailA, "shares a private key with") || !strings.Contains(detailA, "ns2/reused-key-b") {
		t.Errorf("expected ns1/reused-key-a's detail to name ns2/reused-key-b as sharing a key, got %q", detailA)
	}

	detailB, ok := details["ns2/reused-key-b"]
	if !ok {
		t.Fatal("expected a secret row for ns2/reused-key-b")
	}
	if !strings.Contains(detailB, "shares a private key with") || !strings.Contains(detailB, "ns1/reused-key-a") {
		t.Errorf("expected ns2/reused-key-b's detail to name ns1/reused-key-a as sharing a key, got %q", detailB)
	}
}

// TestDNSNameConflictEndToEnd seeds two unrelated Secrets claiming the same
// DNS name, and asserts both "secret" rows are flagged noting the conflict
// — usually a leftover from a migration or a misconfigured Ingress.
func TestDNSNameConflictEndToEnd(t *testing.T) {
	ctx := context.Background()

	certA := generateCert(t, "shared.example.com", time.Now().Add(60*24*time.Hour))
	certB := generateCert(t, "shared.example.com", time.Now().Add(60*24*time.Hour))
	secretA := newTLSSecret("conflict-a", certA)
	secretB := newTLSSecret("conflict-b", certB)
	secretA.Namespace, secretB.Namespace = "ns1", "ns2"

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(secretA, secretB)
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true, nil, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	details := make(map[string]string)
	for _, r := range rows {
		if r.Source == "secret" {
			details[r.Namespace+"/"+r.Name] = r.Detail
		}
	}

	detailA, ok := details["ns1/conflict-a"]
	if !ok {
		t.Fatal("expected a secret row for ns1/conflict-a")
	}
	if !strings.Contains(detailA, "shared.example.com also claimed by") || !strings.Contains(detailA, "ns2/conflict-b") {
		t.Errorf("expected ns1/conflict-a's detail to note the conflict with ns2/conflict-b, got %q", detailA)
	}
}

// TestChainCertExpiryEndToEnd seeds a Secret whose leaf is healthy for
// months but whose bundled intermediate expires within the warn window, and
// asserts the secret row is classified broken-chain (the same tier a
// structurally-broken bundle uses) naming the expiring chain cert rather
// than being reported healthy just because the leaf looks fine.
func TestChainCertExpiryEndToEnd(t *testing.T) {
	ctx := context.Background()

	ca, caKey := genCAForE2E(t, "expiring-intermediate")
	ca.NotAfter = time.Now().Add(3 * 24 * time.Hour) // inside the 14-day warn window
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("re-signing intermediate with its own shortened NotAfter: %v", err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parsing re-signed intermediate: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating leaf key: %v", err)
	}
	leafSerial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	leafTmpl := &x509.Certificate{
		SerialNumber: leafSerial,
		Subject:      pkix.Name{CommonName: "chain-expiry.example.com"},
		DNSNames:     []string{"chain-expiry.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(300 * 24 * time.Hour), // leaf itself is healthy for months
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating leaf signed by the soon-to-expire intermediate: %v", err)
	}

	certPEM := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...,
	)
	secret := newTLSSecretPEM("ns1", "chain-expiry-secret", certPEM)

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(secret)
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true, nil, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	for _, r := range rows {
		if r.Source == "secret" && r.Name == "chain-expiry-secret" {
			if r.Status != "broken-chain" {
				t.Fatalf("expected broken-chain classification for an expiring chain cert, got %q (detail: %q)", r.Status, r.Detail)
			}
			if !strings.Contains(r.Detail, "chain certificate") || !strings.Contains(r.Detail, "expiring-intermediate") {
				t.Errorf("expected detail to name the expiring chain cert, got %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("expected a secret row for chain-expiry-secret, found none")
}

// genCAForE2E creates a self-signed CA certificate template and key, for
// tests here that need to control/re-sign the CA's own NotAfter (unlike
// fixtures_test.go's generateCert, which is leaf-only).
func genCAForE2E(t *testing.T, cn string) (*x509.Certificate, *ecdsa.PrivateKey) {
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
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	return tmpl, key
}

// TestOrphanedSecretEndToEnd seeds a Secret with no owning Certificate and
// no Ingress route referencing it, alongside a normal Certificate-backed
// Secret, and asserts only the truly unreferenced one is flagged orphaned.
func TestOrphanedSecretEndToEnd(t *testing.T) {
	ctx := context.Background()

	managedCert := generateCert(t, "managed.example.com", time.Now().Add(60*24*time.Hour))
	orphanCert := generateCert(t, "orphan.example.com", time.Now().Add(60*24*time.Hour))

	certCR := newCertificateCR("ns1", "managed-cert", "managed-cert-tls", managedCert.Leaf.NotAfter, true, "")
	dyn := newFakeDynamicClient(certCR)
	typed := newFakeTypedClient(
		newTLSSecret("managed-cert-tls", managedCert),
		newTLSSecret("orphan-tls", orphanCert),
	)
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true, nil, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	details := make(map[string]string)
	for _, r := range rows {
		if r.Source == "secret" {
			details[r.Name] = r.Detail
		}
	}

	if detail, ok := details["managed-cert-tls"]; !ok {
		t.Fatal("expected a secret row for managed-cert-tls")
	} else if strings.Contains(detail, "orphaned") {
		t.Errorf("expected managed-cert-tls (owned by a Certificate) not to be flagged orphaned, got %q", detail)
	}

	detail, ok := details["orphan-tls"]
	if !ok {
		t.Fatal("expected a secret row for orphan-tls")
	}
	if !strings.Contains(detail, "orphaned: no Certificate, Ingress, or Gateway route references this Secret") {
		t.Errorf("expected orphan-tls to be flagged orphaned, got %q", detail)
	}
}

// TestRequiredLabelEndToEnd seeds a cert-manager Certificate missing a
// required label alongside one carrying it, and asserts only the
// non-compliant one is flagged — a compliance-review hook, not a
// status-changing finding.
func TestRequiredLabelEndToEnd(t *testing.T) {
	ctx := context.Background()

	cert := generateCert(t, "compliant.example.com", time.Now().Add(60*24*time.Hour))
	compliantCR := newCertificateCR("ns1", "compliant-cert", "compliant-cert-tls", cert.Leaf.NotAfter, true, "")
	compliantCR.SetLabels(map[string]string{"team": "platform", "environment": "prod"})

	nonCompliantCert := generateCert(t, "noncompliant.example.com", time.Now().Add(60*24*time.Hour))
	nonCompliantCR := newCertificateCR("ns1", "noncompliant-cert", "noncompliant-cert-tls", nonCompliantCert.Leaf.NotAfter, true, "")
	nonCompliantCR.SetLabels(map[string]string{"team": "platform"})

	dyn := newFakeDynamicClient(compliantCR, nonCompliantCR)
	typed := newFakeTypedClient(
		newTLSSecret("compliant-cert-tls", cert),
		newTLSSecret("noncompliant-cert-tls", nonCompliantCert),
	)
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true, []string{"team", "environment"}, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	details := make(map[string]string)
	statuses := make(map[string]string)
	for _, r := range rows {
		if r.Source == "cert-manager" {
			details[r.Name] = r.Detail
			statuses[r.Name] = r.Status
		}
	}

	if detail, ok := details["compliant-cert"]; !ok {
		t.Fatal("expected a cert-manager row for compliant-cert")
	} else if strings.Contains(detail, "missing required label") {
		t.Errorf("expected compliant-cert (has both labels) not to be flagged, got %q", detail)
	}

	detail, ok := details["noncompliant-cert"]
	if !ok {
		t.Fatal("expected a cert-manager row for noncompliant-cert")
	}
	if !strings.Contains(detail, "missing required label(s): environment") {
		t.Errorf("expected noncompliant-cert to be flagged missing the environment label, got %q", detail)
	}
	if statuses["noncompliant-cert"] != "ok" {
		t.Errorf("expected a missing-label finding to stay Detail-only (status ok), got status %q", statuses["noncompliant-cert"])
	}
}

// TestCertificateRequestFailureEndToEnd seeds a not-ready Certificate
// alongside its most recent CertificateRequest (which carries the real
// ACME/webhook failure reason via the cert-manager.io/certificate-name
// label), and asserts the cert-manager row's Detail is enriched with that
// reason rather than just "not ready".
func TestCertificateRequestFailureEndToEnd(t *testing.T) {
	ctx := context.Background()

	cert := generateCert(t, "failing.example.com", time.Now().Add(60*24*time.Hour))
	certCR := newCertificateCR("ns1", "failing-cert", "failing-cert-tls", cert.Leaf.NotAfter, false, "NotReady")
	oldReq := newCertificateRequestCR("ns1", "failing-cert-1", "failing-cert", time.Now().Add(-time.Hour), false, "RateLimited")
	latestReq := newCertificateRequestCR("ns1", "failing-cert-2", "failing-cert", time.Now(), false, "ChallengeFailed")

	dyn := newFakeDynamicClient(certCR, oldReq, latestReq)
	typed := newFakeTypedClient(newTLSSecret("failing-cert-tls", cert))
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true, nil, nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	for _, r := range rows {
		if r.Source == "cert-manager" && r.Name == "failing-cert" {
			if !strings.Contains(r.Detail, "ChallengeFailed") {
				t.Errorf("expected the most recent CertificateRequest's failure reason (ChallengeFailed) in detail, got %q", r.Detail)
			}
			if strings.Contains(r.Detail, "RateLimited") {
				t.Errorf("expected only the most recent CertificateRequest's reason, got stale reason too: %q", r.Detail)
			}
			return
		}
	}
	t.Fatal("expected a cert-manager row for failing-cert, found none")
}
