//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/cmd"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"github.com/thev1ndu/certhealthz/pkg/ui"
)

// apiCertDetail mirrors cmd's (unexported) apiCertDetail type, so this
// package can decode /api/certs/detail responses the same way apiRow
// mirrors apiRow for /api/certs.
type apiCertDetail struct {
	ID                 string   `json:"id"`
	Source             string   `json:"source"`
	Subject            string   `json:"subject"`
	SubjectCommonName  string   `json:"subjectCommonName"`
	Issuer             string   `json:"issuer"`
	IssuerCommonName   string   `json:"issuerCommonName"`
	SerialNumber       string   `json:"serialNumber"`
	DNSNames           []string `json:"dnsNames"`
	SignatureAlgorithm string   `json:"signatureAlgorithm"`
	PublicKeyAlgorithm string   `json:"publicKeyAlgorithm"`
	PublicKeyBits      int      `json:"publicKeyBits"`
	FingerprintSHA256  string   `json:"fingerprintSha256"`
	IsCA               bool     `json:"isCA"`
}

// TestCertDetailEndToEnd wires cmd.NewUIMux with a fake cert-manager
// Certificate (backed by a fake Secret) and a plain fake Secret, then
// asserts GET /api/certs/detail returns the full parsed x509 fields for
// each — subject, issuer, serial, SANs, signature/key algorithm, and
// fingerprint — not just the summary fields /api/certs exposes.
func TestCertDetailEndToEnd(t *testing.T) {
	notAfter := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
	managedCert := generateCert(t, "managed.example.com", notAfter)
	secretCert := generateCert(t, "raw-secret.example.com", notAfter)

	certCR := newCertificateCR("ns1", "managed-cert", "managed-cert-tls", notAfter, true, "")
	dyn := newFakeDynamicClient(certCR)
	typed := newFakeTypedClient(
		newTLSSecret("managed-cert-tls", managedCert),
		newTLSSecret("raw-secret-tls", secretCert),
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
	byName := make(map[string]apiRow, len(rows))
	for _, r := range rows {
		byName[r.Name] = r
	}

	cases := []struct {
		rowName    string
		wantHost   string
		wantSource string
	}{
		{"managed-cert", "managed.example.com", "cert-manager"},
		{"raw-secret-tls", "raw-secret.example.com", "secret"},
	}

	for _, tc := range cases {
		row, ok := byName[tc.rowName]
		if !ok {
			t.Fatalf("expected row %q, got %+v", tc.rowName, rows)
		}
		if row.Source != tc.wantSource {
			t.Fatalf("row %q: expected source %q, got %q", tc.rowName, tc.wantSource, row.Source)
		}

		detail := fetchCertDetail(t, server.URL, row.ID)
		if detail.SubjectCommonName != tc.wantHost {
			t.Errorf("%s: expected subject CN %q, got %q", tc.rowName, tc.wantHost, detail.SubjectCommonName)
		}
		if len(detail.DNSNames) != 1 || detail.DNSNames[0] != tc.wantHost {
			t.Errorf("%s: expected DNS SAN [%q], got %v", tc.rowName, tc.wantHost, detail.DNSNames)
		}
		if detail.PublicKeyAlgorithm != "ECDSA" || detail.PublicKeyBits != 256 {
			t.Errorf("%s: expected ECDSA/256, got %s/%d", tc.rowName, detail.PublicKeyAlgorithm, detail.PublicKeyBits)
		}
		if detail.FingerprintSHA256 == "" {
			t.Errorf("%s: expected non-empty SHA256 fingerprint", tc.rowName)
		}
		if detail.SerialNumber == "" {
			t.Errorf("%s: expected non-empty serial number", tc.rowName)
		}
	}
}

func fetchAPIRows(t testing.TB, baseURL string) []apiRow {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/certs")
	if err != nil {
		t.Fatalf("GET /api/certs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/certs: expected 200, got %d", resp.StatusCode)
	}
	var rows []apiRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decoding /api/certs response: %v", err)
	}
	return rows
}

func fetchCertDetail(t testing.TB, baseURL, id string) apiCertDetail {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/certs/detail?id=" + url.QueryEscape(id))
	if err != nil {
		t.Fatalf("GET /api/certs/detail: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/certs/detail?id=%s: expected 200, got %d", id, resp.StatusCode)
	}
	var detail apiCertDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decoding /api/certs/detail response: %v", err)
	}
	return detail
}
