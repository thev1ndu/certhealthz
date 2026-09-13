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
	mux := cmd.NewDashboardMux(uiHandler, collect, cmd.NewClusterRegistry())
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
	mux := cmd.NewDashboardMux(uiHandler, noopCollect, cmd.NewClusterRegistry())
	server := httptest.NewServer(mux)
	defer server.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("kubeconfig", "bad-config.yaml")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	part.Write([]byte("not: a valid kubeconfig"))
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
