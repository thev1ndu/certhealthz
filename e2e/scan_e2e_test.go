//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/cmd"
)

const warnDays = 14

// TestScanEndToEnd drives the real cert-manager-scan + Secret-scan +
// classification pipeline (cmd.CollectRowsFromClients, the same function
// `scan` and `dashboard` call) against fake clientsets seeded with:
//   - a Ready cert-manager Certificate, healthy expiry
//   - a not-Ready cert-manager Certificate (broken renewal)
//   - a kubernetes.io/tls Secret expiring within the warn window
//   - a kubernetes.io/tls Secret already expired
func TestScanEndToEnd(t *testing.T) {
	healthyCert := generateCert(t, "healthy.example.com", time.Now().Add(90*24*time.Hour))
	expiringCert := generateCert(t, "expiring.example.com", time.Now().Add(5*24*time.Hour))
	expiredCert := generateCert(t, "expired.example.com", time.Now().Add(-24*time.Hour))

	readyCR := newCertificateCR("ns1", "healthy-cert", "healthy-tls", healthyCert.Leaf.NotAfter, true, "")
	notReadyCR := newCertificateCR("ns1", "broken-cert", "broken-tls", time.Time{}, false, "IssuingFailed")

	dyn := newFakeDynamicClient(readyCR, notReadyCR)
	typed := newFakeTypedClient(
		newTLSSecret("healthy-tls", healthyCert),
		newTLSSecret("expiring-tls", expiringCert),
		newTLSSecret("expired-tls", expiredCert),
	)

	targets := []cmd.ClusterClients{
		{Label: "test-cluster", Dyn: dyn, Typed: typed},
	}

	rows, err := cmd.CollectRowsFromClients(context.Background(), targets, warnDays, true, nil)
	if err != nil {
		t.Fatalf("CollectRowsFromClients: %v", err)
	}

	byName := make(map[string]struct {
		Source, Status, Detail string
	}, len(rows))
	for _, r := range rows {
		byName[r.Name] = struct{ Source, Status, Detail string }{r.Source, r.Status, r.Detail}
	}

	cases := []struct {
		name, source, status string
	}{
		{"healthy-cert", "cert-manager", "ok"},
		{"broken-cert", "cert-manager", "error"},
		{"expiring-tls", "secret", "expiring"},
		{"expired-tls", "secret", "expired"},
	}
	for _, c := range cases {
		got, ok := byName[c.name]
		if !ok {
			t.Errorf("expected row for %s, none found (rows: %+v)", c.name, rows)
			continue
		}
		if got.Source != c.source {
			t.Errorf("%s: expected source %q, got %q", c.name, c.source, got.Source)
		}
		if got.Status != c.status {
			t.Errorf("%s: expected status %q, got %q", c.name, c.status, got.Status)
		}
	}

	if got := byName["broken-cert"].Detail; got != "not ready: IssuingFailed" {
		t.Errorf("broken-cert: expected detail %q, got %q", "not ready: IssuingFailed", got)
	}
}
