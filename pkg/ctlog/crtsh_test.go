package ctlog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueryURL(t *testing.T) {
	const body = `[
		{"id": 100, "issuer_name": "C=US, O=Let's Encrypt, CN=R11", "name_value": "api.example.com", "not_before": "2026-09-01T00:00:00", "not_after": "2026-12-01T00:00:00"},
		{"id": 100, "issuer_name": "C=US, O=Let's Encrypt, CN=R11", "name_value": "api.example.com", "not_before": "2026-09-01T00:00:00", "not_after": "2026-12-01T00:00:00"},
		{"id": 101, "issuer_name": "C=US, O=DigiCert Inc, CN=DigiCert TLS RSA SHA256 2020 CA1", "name_value": "api.example.com\nwww.example.com", "not_before": "2026-01-01T00:00:00", "not_after": "2027-01-01T00:00:00"}
	]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	entries, err := queryURL(context.Background(), server.URL, "example.com")
	if err != nil {
		t.Fatalf("queryURL: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 deduplicated entries, got %d: %+v", len(entries), entries)
	}

	if entries[0].ID != 100 || entries[0].Issuer != "C=US, O=Let's Encrypt, CN=R11" {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	wantNotBefore := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if !entries[0].NotBefore.Equal(wantNotBefore) {
		t.Errorf("expected NotBefore %v, got %v", wantNotBefore, entries[0].NotBefore)
	}

	if entries[1].ID != 101 || entries[1].NameValue != "api.example.com\nwww.example.com" {
		t.Errorf("unexpected second entry: %+v", entries[1])
	}
}

func TestQueryURLNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	if _, err := queryURL(context.Background(), server.URL, "example.com"); err == nil {
		t.Fatal("expected an error on non-200 response, got nil")
	}
}
