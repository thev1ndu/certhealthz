//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/thev1ndu/certhealthz/cmd"
)

// setCertificateSpecDNSNames sets spec.dnsNames on a Certificate CR fixture
// built by newCertificateCR, which doesn't take dnsNames itself (adding a
// 7th positional param would ripple through every existing call site for a
// field only this test needs).
func setCertificateSpecDNSNames(cr *unstructured.Unstructured, dnsNames []string) error {
	return unstructured.SetNestedStringSlice(cr.Object, dnsNames, "spec", "dnsNames")
}

// TestIssuerHealthEndToEnd seeds a healthy Issuer, an unhealthy
// ClusterIssuer, and an unrelated Certificate, then asserts
// CollectRowsFromClients reports the issuer/clusterissuer rows correctly
// (source, status, failure reason) without disturbing the existing
// Certificate row.
func TestIssuerHealthEndToEnd(t *testing.T) {
	ctx := context.Background()

	cert := generateCert(t, "tracked.example.com", time.Now().Add(60*24*time.Hour))
	certCR := newCertificateCR("ns1", "tracked-cert", "tracked-cert-tls", cert.Leaf.NotAfter, true, "")
	healthyIssuer := newIssuerCR("ns1", "letsencrypt-staging", true, "")
	brokenClusterIssuer := newClusterIssuerCR("letsencrypt-prod", false, "AccountRegistrationFailed")

	dyn := newFakeDynamicClient(certCR, healthyIssuer, brokenClusterIssuer)
	typed := newFakeTypedClient(newTLSSecret("tracked-cert-tls", cert))
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	byKey := make(map[string]struct {
		Status string
		Detail string
	}, len(rows))
	for _, r := range rows {
		byKey[r.Source+"/"+r.Name] = struct {
			Status string
			Detail string
		}{r.Status, r.Detail}
	}

	if got, ok := byKey["issuer/letsencrypt-staging"]; !ok || got.Status != "ok" {
		t.Errorf("expected healthy issuer row with status ok, got %+v (ok=%v)", got, ok)
	}
	if got, ok := byKey["clusterissuer/letsencrypt-prod"]; !ok || got.Status != "error" {
		t.Errorf("expected unhealthy clusterissuer row with status error, got %+v (ok=%v)", got, ok)
	} else if got.Detail != "not ready: AccountRegistrationFailed" {
		t.Errorf("expected detail to include the failure reason, got %q", got.Detail)
	}
	if got, ok := byKey["cert-manager/tracked-cert"]; !ok || got.Status != "ok" {
		t.Errorf("expected the existing Certificate row to be unaffected, got %+v (ok=%v)", got, ok)
	}
}

// TestSANDriftEndToEnd drives a real scan where a cert-manager Certificate's
// spec.dnsNames no longer matches its backing Secret's actual leaf SANs,
// and asserts it's classified as drift (the same mechanism NotAfter drift
// already used, extended rather than duplicated).
func TestSANDriftEndToEnd(t *testing.T) {
	ctx := context.Background()

	cert := generateCert(t, "actual.example.com", time.Now().Add(60*24*time.Hour))

	certCR := newCertificateCR("ns1", "san-drift-cert", "san-drift-cert-tls", cert.Leaf.NotAfter, true, "")
	// spec.dnsNames declares a different name than what's actually in the
	// Secret's leaf cert.
	if err := setCertificateSpecDNSNames(certCR, []string{"declared.example.com"}); err != nil {
		t.Fatalf("setting spec.dnsNames: %v", err)
	}

	dyn := newFakeDynamicClient(certCR)
	typed := newFakeTypedClient(newTLSSecret("san-drift-cert-tls", cert))
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	rows, err := cmd.CollectRowsFromClients(ctx, targets, warnDays, true)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	for _, r := range rows {
		if r.Source == "cert-manager" && r.Name == "san-drift-cert" {
			if r.Status != "drift" {
				t.Fatalf("expected san-drift-cert to be classified drift, got %q (detail: %q)", r.Status, r.Detail)
			}
			return
		}
	}
	t.Fatal("expected a cert-manager row for san-drift-cert, found none")
}
