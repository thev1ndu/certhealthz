//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/cmd"
)

// TestIngressCrossReferenceEndToEnd drives the real Ingress cross-reference
// path (cmd.CollectRowsFromClients) against fake clientsets seeded with:
//   - an Ingress whose host is actually covered by its Secret's cert
//   - an Ingress pointing at a Secret that doesn't exist
//   - an Ingress whose host isn't covered by its Secret's cert's SANs
func TestIngressCrossReferenceEndToEnd(t *testing.T) {
	okCert := generateCert(t, "api.example.com", time.Now().Add(90*24*time.Hour))
	mismatchCert := generateCert(t, "other.example.com", time.Now().Add(90*24*time.Hour))

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(
		newTLSSecret("api-tls", okCert),
		newTLSSecret("mismatch-tls", mismatchCert),
		newIngress("ns1", "api-ingress", "api-tls", "api.example.com"),
		newIngress("ns1", "missing-ingress", "does-not-exist-tls", "missing.example.com"),
		newIngress("ns1", "mismatch-ingress", "mismatch-tls", "api.example.com"),
	)

	targets := []cmd.ClusterClients{
		{Label: "test-cluster", Dyn: dyn, Typed: typed},
	}

	rows, err := cmd.CollectRowsFromClients(context.Background(), targets, warnDays, true)
	if err != nil {
		t.Fatalf("CollectRowsFromClients: %v", err)
	}

	byName := make(map[string]struct {
		Source, Status string
	}, len(rows))
	for _, r := range rows {
		if r.Source == "ingress" {
			byName[r.Name] = struct{ Source, Status string }{r.Source, r.Status}
		}
	}

	cases := []struct {
		name, status string
	}{
		{"api-ingress (api.example.com)", "ok"},
		{"missing-ingress (missing.example.com)", "error"},
		{"mismatch-ingress (api.example.com)", "error"},
	}
	for _, c := range cases {
		got, ok := byName[c.name]
		if !ok {
			t.Errorf("expected ingress row %q, none found (rows: %+v)", c.name, rows)
			continue
		}
		if got.Status != c.status {
			t.Errorf("%s: expected status %q, got %q", c.name, c.status, got.Status)
		}
	}
}
