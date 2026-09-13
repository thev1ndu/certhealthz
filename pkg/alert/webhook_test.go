package alert

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

func TestFlagged(t *testing.T) {
	rows := []output.Row{
		{Name: "a", Status: "ok"},
		{Name: "b", Status: "expiring"},
		{Name: "c", Status: "expired"},
		{Name: "d", Status: "error"},
		{Name: "e", Status: "drift"},
	}
	flagged := Flagged(rows)
	if len(flagged) != 4 {
		t.Fatalf("expected 4 flagged rows, got %d: %+v", len(flagged), flagged)
	}
	for _, r := range flagged {
		if r.Status == "ok" {
			t.Errorf("row %q with status 'ok' should not be flagged", r.Name)
		}
	}
}

func TestSendSkipsWhenNothingFlagged(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	if err := Send(server.URL, []output.Row{{Name: "a", Status: "ok"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if called {
		t.Error("expected Send to skip the request when nothing is flagged")
	}
}

func TestSendPostsFlaggedRows(t *testing.T) {
	var received Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decoding webhook payload: %v", err)
		}
	}))
	defer server.Close()

	rows := []output.Row{
		{Name: "healthy", Status: "ok"},
		{Name: "expiring-soon", Status: "expiring"},
	}
	if err := Send(server.URL, rows); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(received.Rows) != 1 || received.Rows[0].Name != "expiring-soon" {
		t.Errorf("expected only the flagged row in the payload, got %+v", received.Rows)
	}
}
