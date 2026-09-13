//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/cmd"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"github.com/thev1ndu/certhealthz/pkg/probe"
	"github.com/thev1ndu/certhealthz/pkg/ui"
)

// apiRow mirrors the JSON shape cmd's (unexported) apiRow type serializes,
// so this package can decode /api/certs responses without needing access to
// cmd's internals beyond the exported seam.
type apiRow struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Days      *int   `json:"days"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
}

// TestDashboardCertsEndToEnd wires cmd.NewDashboardMux to a collect closure
// backed by fake clientsets (the same seam scan_e2e_test.go exercises
// directly), serves it over a real HTTP server, and asserts the JSON the
// frontend actually consumes matches the fixtures.
func TestDashboardCertsEndToEnd(t *testing.T) {
	healthyCert := generateCert(t, "healthy.example.com", time.Now().Add(90*24*time.Hour))
	expiredCert := generateCert(t, "expired.example.com", time.Now().Add(-24*time.Hour))

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(
		newTLSSecret("healthy-tls", healthyCert),
		newTLSSecret("expired-tls", expiredCert),
	)
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	collect := func(ctx context.Context) ([]output.Row, error) {
		return cmd.CollectRowsFromClients(ctx, targets, warnDays, true)
	}

	uiHandler, err := ui.Handler()
	if err != nil {
		t.Fatalf("ui.Handler: %v", err)
	}
	mux := cmd.NewDashboardMux(uiHandler, collect, cmd.NewClusterRegistry(), cmd.NewEndpointRegistry())
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/certs")
	if err != nil {
		t.Fatalf("GET /api/certs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var rows []apiRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decoding /api/certs response: %v", err)
	}

	byName := make(map[string]apiRow, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}

	healthy, ok := byName["healthy-tls"]
	if !ok {
		t.Fatalf("expected healthy-tls row, got %+v", rows)
	}
	if healthy.Status != "ok" || healthy.Source != "secret" || healthy.Days == nil || *healthy.Days < 80 {
		t.Errorf("unexpected healthy-tls row: %+v", healthy)
	}

	expired, ok := byName["expired-tls"]
	if !ok {
		t.Fatalf("expected expired-tls row, got %+v", rows)
	}
	if expired.Status != "expired" {
		t.Errorf("expected expired-tls status 'expired', got %q", expired.Status)
	}
}

// TestDashboardEndpointsEndToEnd drives POST/GET /api/endpoints against a
// real HTTP server, then wires a collect closure the same way runDashboard
// does (cluster rows + probed endpoint rows, merged and sorted) to assert a
// registered endpoint actually shows up in /api/certs.
func TestDashboardEndpointsEndToEnd(t *testing.T) {
	notAfter := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	cert := generateCert(t, "127.0.0.1", notAfter)
	addr := startTLSServer(t, cert)

	uiHandler, err := ui.Handler()
	if err != nil {
		t.Fatalf("ui.Handler: %v", err)
	}

	endpoints := cmd.NewEndpointRegistry()
	collect := func(ctx context.Context) ([]output.Row, error) {
		var rows []output.Row
		for _, ep := range endpoints.All() {
			result := probe.Probe(ep, 2*time.Second, probe.WithRootCAs(certPool(cert)))
			row := output.Row{Source: "endpoint", Name: result.Endpoint, Detail: result.Issuer}
			if result.Err != nil {
				row.Status = "error"
				row.Detail = result.Err.Error()
			} else {
				row.NotAfter = result.NotAfter
				row = output.Classify(row, 14)
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
	mux := cmd.NewDashboardMux(uiHandler, collect, cmd.NewClusterRegistry(), endpoints)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Before adding anything, the list is empty.
	resp, err := http.Get(server.URL + "/api/endpoints")
	if err != nil {
		t.Fatalf("GET /api/endpoints: %v", err)
	}
	var before []string
	if err := json.NewDecoder(resp.Body).Decode(&before); err != nil {
		t.Fatalf("decoding /api/endpoints response: %v", err)
	}
	resp.Body.Close()
	if len(before) != 0 {
		t.Fatalf("expected no endpoints configured yet, got %v", before)
	}

	// Add the local TLS listener as an endpoint.
	body, _ := json.Marshal(map[string]string{"endpoint": addr})
	resp, err = http.Post(server.URL+"/api/endpoints", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/endpoints: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Adding the same endpoint again is rejected.
	resp, err = http.Post(server.URL+"/api/endpoints", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/endpoints (duplicate): %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for duplicate endpoint, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// It now shows up as a probed row in /api/certs.
	resp, err = http.Get(server.URL + "/api/certs")
	if err != nil {
		t.Fatalf("GET /api/certs: %v", err)
	}
	defer resp.Body.Close()
	var rows []apiRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decoding /api/certs response: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Source == "endpoint" && r.Name == addr {
			found = true
			if r.Status != "ok" {
				t.Errorf("expected probed endpoint status 'ok', got %q", r.Status)
			}
		}
	}
	if !found {
		t.Fatalf("expected a probed endpoint row for %s, got %+v", addr, rows)
	}
}

// TestDashboardAddClusterRejectsBadKubeconfigEndToEnd asserts POST
// /api/clusters fails fast with 400 on an invalid uploaded kubeconfig,
// without needing a fake clientset (the validation happens before any
// client is used).
func TestDashboardAddClusterRejectsBadKubeconfigEndToEnd(t *testing.T) {
	uiHandler, err := ui.Handler()
	if err != nil {
		t.Fatalf("ui.Handler: %v", err)
	}
	noopCollect := func(ctx context.Context) ([]output.Row, error) { return nil, nil }
	mux := cmd.NewDashboardMux(uiHandler, noopCollect, cmd.NewClusterRegistry(), cmd.NewEndpointRegistry())
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("kubeconfig", "bad-config.yaml")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("not: a valid kubeconfig")); err != nil {
		t.Fatalf("writing form file part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	resp, err := http.Post(server.URL+"/api/clusters", w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST /api/clusters: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid kubeconfig upload, got %d", resp.StatusCode)
	}
}
