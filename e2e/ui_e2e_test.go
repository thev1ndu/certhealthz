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

// TestUICertsEndToEnd wires cmd.NewUIMux to a collect closure
// backed by fake clientsets (the same seam scan_e2e_test.go exercises
// directly), serves it over a real HTTP server, and asserts the JSON the
// frontend actually consumes matches the fixtures.
func TestUICertsEndToEnd(t *testing.T) {
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
	mux := cmd.NewUIMux(uiHandler, cmd.UIDeps{
		Collect:   collect,
		Clusters:  cmd.NewClusterRegistry(),
		Endpoints: cmd.NewEndpointRegistry(),
		History:   newTestHistoryStore(t),
		Settings:  cmd.NewSettings(warnDays, true, ""),
	})
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

// TestUIEndpointsEndToEnd drives POST/GET /api/endpoints against a
// real HTTP server, then wires a collect closure the same way runUI
// does (cluster rows + probed endpoint rows, merged and sorted) to assert a
// registered endpoint actually shows up in /api/certs.
func TestUIEndpointsEndToEnd(t *testing.T) {
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
	mux := cmd.NewUIMux(uiHandler, cmd.UIDeps{
		Collect:   collect,
		Clusters:  cmd.NewClusterRegistry(),
		Endpoints: endpoints,
		History:   newTestHistoryStore(t),
		Settings:  cmd.NewSettings(14, true, ""),
	})
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

// TestUISettingsEndToEnd asserts a POST /api/settings change is
// actually picked up by the next collect — the whole point of Settings
// being read live rather than captured once at startup.
func TestUISettingsEndToEnd(t *testing.T) {
	notAfter := time.Now().Add(10 * 24 * time.Hour).Truncate(time.Second) // 10 days out
	cert := generateCert(t, "soon.example.com", notAfter)

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(newTLSSecret("soon-tls", cert))
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}

	settings := cmd.NewSettings(5, true, "") // warnDays=5: 10 days out is not yet "expiring"
	collect := func(ctx context.Context) ([]output.Row, error) {
		warnDays, includeSecrets, _ := settings.Get()
		return cmd.CollectRowsFromClients(ctx, targets, warnDays, includeSecrets)
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
		Settings:  settings,
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	statusFor := func() string {
		resp, err := http.Get(server.URL + "/api/certs")
		if err != nil {
			t.Fatalf("GET /api/certs: %v", err)
		}
		defer resp.Body.Close()
		var rows []apiRow
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decoding /api/certs response: %v", err)
		}
		for _, r := range rows {
			if r.Name == "soon-tls" {
				return r.Status
			}
		}
		t.Fatalf("expected a soon-tls row, got %+v", rows)
		return ""
	}

	if got := statusFor(); got != "ok" {
		t.Fatalf("expected 'ok' at warnDays=5, got %q", got)
	}

	// Raise warnDays past 10 — the same cert should now read "expiring".
	body, _ := json.Marshal(map[string]any{"warnDays": 30, "includeSecrets": true, "webhookURL": ""})
	resp, err := http.Post(server.URL+"/api/settings", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/settings: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	if got := statusFor(); got != "expiring" {
		t.Fatalf("expected 'expiring' after raising warnDays to 30, got %q", got)
	}

	// A non-positive warnDays is rejected rather than silently accepted.
	badBody, _ := json.Marshal(map[string]any{"warnDays": 0, "includeSecrets": true})
	resp, err = http.Post(server.URL+"/api/settings", "application/json", bytes.NewReader(badBody))
	if err != nil {
		t.Fatalf("POST /api/settings (invalid): %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for non-positive warnDays, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestUIAlertEndToEnd configures a webhook URL via /api/settings
// pointing at a local httptest server, triggers POST /api/alert, and
// asserts the fake webhook actually received the flagged row.
func TestUIAlertEndToEnd(t *testing.T) {
	expiredCert := generateCert(t, "expired.example.com", time.Now().Add(-24*time.Hour))

	dyn := newFakeDynamicClient()
	typed := newFakeTypedClient(newTLSSecret("expired-tls", expiredCert))
	targets := []cmd.ClusterClients{{Label: "test-cluster", Dyn: dyn, Typed: typed}}
	collect := func(ctx context.Context) ([]output.Row, error) {
		return cmd.CollectRowsFromClients(ctx, targets, warnDays, true)
	}

	var received int
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
	}))
	defer webhook.Close()

	settings := cmd.NewSettings(warnDays, true, "")

	uiHandler, err := ui.Handler()
	if err != nil {
		t.Fatalf("ui.Handler: %v", err)
	}
	mux := cmd.NewUIMux(uiHandler, cmd.UIDeps{
		Collect:   collect,
		Clusters:  cmd.NewClusterRegistry(),
		Endpoints: cmd.NewEndpointRegistry(),
		History:   newTestHistoryStore(t),
		Settings:  settings,
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	// No webhook configured yet — rejected.
	resp, err := http.Post(server.URL+"/api/alert", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/alert (unconfigured): %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 with no webhook configured, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Configure the webhook, then trigger it.
	settingsBody, _ := json.Marshal(map[string]any{"warnDays": warnDays, "includeSecrets": true, "webhookURL": webhook.URL})
	resp, err = http.Post(server.URL+"/api/settings", "application/json", bytes.NewReader(settingsBody))
	if err != nil {
		t.Fatalf("POST /api/settings: %v", err)
	}
	resp.Body.Close()

	resp, err = http.Post(server.URL+"/api/alert", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/alert: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Flagged int  `json:"flagged"`
		Sent    bool `json:"sent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding /api/alert response: %v", err)
	}
	if !result.Sent || result.Flagged != 1 {
		t.Errorf("expected {sent: true, flagged: 1}, got %+v", result)
	}
	if received != 1 {
		t.Errorf("expected the fake webhook to receive exactly 1 request, got %d", received)
	}
}

// TestUIHistoryEndToEnd records two snapshots with different fixture
// state and asserts GET /api/history/diff reports the expected change.
func TestUIHistoryEndToEnd(t *testing.T) {
	dyn := newFakeDynamicClient()

	firstCert := generateCert(t, "tracked.example.com", time.Now().Add(60*24*time.Hour))
	typed := newFakeTypedClient(newTLSSecret("tracked-tls", firstCert))
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
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	// Before any recording, the diff endpoint says so rather than erroring.
	resp, err := http.Get(server.URL + "/api/history/diff")
	if err != nil {
		t.Fatalf("GET /api/history/diff: %v", err)
	}
	var diff struct {
		Changes []map[string]any `json:"changes"`
		Message string           `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&diff); err != nil {
		t.Fatalf("decoding /api/history/diff response: %v", err)
	}
	resp.Body.Close()
	if diff.Message == "" {
		t.Errorf("expected a message before any run is recorded, got %+v", diff)
	}

	record := func() {
		resp, err := http.Post(server.URL+"/api/history/record", "application/json", nil)
		if err != nil {
			t.Fatalf("POST /api/history/record: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	}
	record()

	// Swap the fixture out for one where the same cert now reads "expired",
	// then record a second run.
	expiredCert := generateCert(t, "tracked.example.com", time.Now().Add(-24*time.Hour))
	typed2 := newFakeTypedClient(newTLSSecret("tracked-tls", expiredCert))
	targets[0].Typed = typed2
	record()

	resp, err = http.Get(server.URL + "/api/history/diff")
	if err != nil {
		t.Fatalf("GET /api/history/diff (after 2 runs): %v", err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&diff); err != nil {
		t.Fatalf("decoding /api/history/diff response: %v", err)
	}
	found := false
	for _, c := range diff.Changes {
		if c["name"] == "tracked-tls" && c["toState"] == "expired" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a status-change entry for tracked-tls -> expired, got %+v", diff.Changes)
	}
}

// TestUICTValidationEndToEnd exercises POST /api/ct's input
// validation without hitting the real crt.sh (that path is already covered
// by pkg/ctlog's httptest-based unit tests, and the ct CLI command was
// smoke-tested live against crt.sh separately).
func TestUICTValidationEndToEnd(t *testing.T) {
	uiHandler, err := ui.Handler()
	if err != nil {
		t.Fatalf("ui.Handler: %v", err)
	}
	noopCollect := func(ctx context.Context) ([]output.Row, error) { return nil, nil }
	mux := cmd.NewUIMux(uiHandler, cmd.UIDeps{
		Collect:   noopCollect,
		Clusters:  cmd.NewClusterRegistry(),
		Endpoints: cmd.NewEndpointRegistry(),
		History:   newTestHistoryStore(t),
		Settings:  cmd.NewSettings(14, true, ""),
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	// No domains — rejected before any network call.
	body, _ := json.Marshal(map[string]any{"domains": []string{}})
	resp, err := http.Post(server.URL+"/api/ct", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/ct (empty domains): %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for empty domains, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Invalid since duration — also rejected before any network call.
	body, _ = json.Marshal(map[string]any{"domains": []string{"example.com"}, "since": "not-a-duration"})
	resp, err = http.Post(server.URL+"/api/ct", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/ct (bad since): %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an invalid since duration, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestUIAddClusterRejectsBadKubeconfigEndToEnd asserts POST
// /api/clusters fails fast with 400 on an invalid uploaded kubeconfig,
// without needing a fake clientset (the validation happens before any
// client is used).
func TestUIAddClusterRejectsBadKubeconfigEndToEnd(t *testing.T) {
	uiHandler, err := ui.Handler()
	if err != nil {
		t.Fatalf("ui.Handler: %v", err)
	}
	noopCollect := func(ctx context.Context) ([]output.Row, error) { return nil, nil }
	mux := cmd.NewUIMux(uiHandler, cmd.UIDeps{
		Collect:   noopCollect,
		Clusters:  cmd.NewClusterRegistry(),
		Endpoints: cmd.NewEndpointRegistry(),
		History:   newTestHistoryStore(t),
		Settings:  cmd.NewSettings(14, true, ""),
	})
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
