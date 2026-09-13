//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/alert"
	"github.com/thev1ndu/certhealthz/pkg/output"
)

// TestAlertWebhookEndToEnd drives alert.Send over a real HTTP POST to a
// local httptest server, verifying only flagged (expiring/expired/error)
// rows are delivered and the JSON payload round-trips correctly.
func TestAlertWebhookEndToEnd(t *testing.T) {
	var received alert.Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decoding webhook body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rows := []output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "healthy", NotAfter: time.Now().Add(90 * 24 * time.Hour), Status: "ok"},
		{Source: "secret", Cluster: "prod", Namespace: "ns1", Name: "soon", NotAfter: time.Now().Add(3 * 24 * time.Hour), Status: "expiring"},
		{Source: "secret", Cluster: "prod", Namespace: "ns1", Name: "gone", NotAfter: time.Now().Add(-24 * time.Hour), Status: "expired"},
	}

	if err := alert.Send(server.URL, rows); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(received.Rows) != 2 {
		t.Fatalf("expected 2 flagged rows delivered, got %d: %+v", len(received.Rows), received.Rows)
	}
	names := map[string]bool{}
	for _, r := range received.Rows {
		names[r.Name] = true
	}
	if !names["soon"] || !names["gone"] {
		t.Errorf("expected flagged rows 'soon' and 'gone', got %+v", received.Rows)
	}
	if names["healthy"] {
		t.Errorf("expected healthy row to be excluded from alert payload")
	}
}

// TestAlertWebhookSkipsWhenNothingFlaggedEndToEnd asserts Send makes no
// HTTP call at all when there's nothing to alert on.
func TestAlertWebhookSkipsWhenNothingFlaggedEndToEnd(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rows := []output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "healthy", NotAfter: time.Now().Add(90 * 24 * time.Hour), Status: "ok"},
	}

	if err := alert.Send(server.URL, rows); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if called {
		t.Error("expected no webhook call when no rows are flagged")
	}
}
