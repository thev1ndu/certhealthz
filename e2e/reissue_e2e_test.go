//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/thev1ndu/certhealthz/cmd"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"github.com/thev1ndu/certhealthz/pkg/ui"
)

// TestReissueCertEndToEnd drives POST /api/certs/reissue against a fake
// cert-manager Certificate and asserts an Issuing:True condition lands in
// its status.conditions alongside the existing Ready condition (not
// replacing it — see certmanager.TriggerReissue's Get+UpdateStatus
// approach), then asserts a "secret"-sourced id is rejected.
func TestReissueCertEndToEnd(t *testing.T) {
	notAfter := time.Now().Add(60 * 24 * time.Hour)
	cert := generateCert(t, "reissue.example.com", notAfter)

	certCR := newCertificateCR("ns1", "reissue-cert", "reissue-cert-tls", notAfter, true, "")
	dyn := newFakeDynamicClient(certCR)
	typed := newFakeTypedClient(newTLSSecret("reissue-cert-tls", cert))
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	collect := func(ctx context.Context) ([]output.Row, error) {
		return cmd.CollectRowsFromClients(ctx, targets, warnDays, true)
	}

	uiHandler, err := ui.Handler()
	if err != nil {
		t.Fatalf("ui.Handler: %v", err)
	}
	mux := cmd.NewUIMux(uiHandler, cmd.UIDeps{
		Collect:   collect,
		Clusters:  cmd.NewClusterRegistry(),
		Endpoints: cmd.NewEndpointRegistry(),
		History:   newTestHistoryStore(t),
		Settings:  cmd.NewSettings(warnDays, true, ""),
		ClusterClientsFor: func(cluster string) (cmd.ClusterClients, error) {
			for _, tgt := range targets {
				if tgt.Label == cluster {
					return tgt, nil
				}
			}
			return cmd.ClusterClients{}, fmt.Errorf("cluster %q not found", cluster)
		},
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	rows := fetchAPIRows(t, server.URL)
	var certManagerID, secretID string
	for _, r := range rows {
		switch r.Source {
		case "cert-manager":
			certManagerID = r.ID
		case "secret":
			secretID = r.ID
		}
	}
	if certManagerID == "" || secretID == "" {
		t.Fatalf("expected both a cert-manager and a secret row, got %+v", rows)
	}

	t.Run("cert-manager source triggers reissue", func(t *testing.T) {
		resp, err := http.Post(server.URL+"/api/certs/reissue?id="+certManagerID, "application/json", nil)
		if err != nil {
			t.Fatalf("POST /api/certs/reissue: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		obj, err := dyn.Resource(certGVR).Namespace("ns1").Get(context.Background(), "reissue-cert", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("fetching patched Certificate: %v", err)
		}
		conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")

		var sawReady, sawIssuing bool
		for _, c := range conditions {
			cond, ok := c.(map[string]any)
			if !ok {
				continue
			}
			switch cond["type"] {
			case "Ready":
				sawReady = true
			case "Issuing":
				sawIssuing = true
				if cond["status"] != "True" {
					t.Errorf("expected Issuing condition status True, got %v", cond["status"])
				}
				if cond["reason"] != "ManuallyTriggered" {
					t.Errorf("expected reason ManuallyTriggered, got %v", cond["reason"])
				}
			}
		}
		if !sawReady {
			t.Error("expected the existing Ready condition to survive the reissue (Get+UpdateStatus, not a clobbering merge patch)")
		}
		if !sawIssuing {
			t.Error("expected an Issuing condition to be added")
		}
	})

	t.Run("secret source is rejected", func(t *testing.T) {
		resp, err := http.Post(server.URL+"/api/certs/reissue?id="+secretID, "application/json", nil)
		if err != nil {
			t.Fatalf("POST /api/certs/reissue: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for a secret-sourced id, got %d", resp.StatusCode)
		}
	})
}
