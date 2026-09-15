//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/cmd"
	"github.com/thev1ndu/certhealthz/pkg/history"
)

// TestHistoryRoundTripEndToEnd drives collect -> RecordRun -> RecordRun ->
// LastTwoRuns -> Diff, the same pipeline `scan --record` + `history diff`
// use, against a real (temp-file) SQLite database and fake clientsets whose
// certs change state between the two runs.
func TestHistoryRoundTripEndToEnd(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "e2e-history.db")

	store, err := history.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Run 1: one healthy cert, one about to expire.
	staleCert := generateCert(t, "stable.example.com", time.Now().Add(60*24*time.Hour))
	decayingCert := generateCert(t, "decaying.example.com", time.Now().Add(20*24*time.Hour))

	run1Dyn := newFakeDynamicClient()
	run1Typed := newFakeTypedClient(
		newTLSSecret("stable-tls", staleCert),
		newTLSSecret("decaying-tls", decayingCert),
	)
	run1Targets := []cmd.ClusterClients{{Label: "prod", Dyn: run1Dyn, Typed: run1Typed}}

	run1Rows, err := cmd.CollectRowsFromClients(ctx, run1Targets, warnDays, true, nil, nil)
	if err != nil {
		t.Fatalf("collect run1: %v", err)
	}
	if _, err := store.RecordRun(run1Rows); err != nil {
		t.Fatalf("RecordRun 1: %v", err)
	}

	// Run 2: "stable" unchanged, "decaying" has now crossed into expiring,
	// and a brand-new cert shows up.
	decayingCertNow := generateCert(t, "decaying.example.com", time.Now().Add(3*24*time.Hour))
	newCert := generateCert(t, "new.example.com", time.Now().Add(45*24*time.Hour))

	run2Dyn := newFakeDynamicClient()
	run2Typed := newFakeTypedClient(
		newTLSSecret("stable-tls", staleCert),
		newTLSSecret("decaying-tls", decayingCertNow),
		newTLSSecret("new-tls", newCert),
	)
	run2Targets := []cmd.ClusterClients{{Label: "prod", Dyn: run2Dyn, Typed: run2Typed}}

	run2Rows, err := cmd.CollectRowsFromClients(ctx, run2Targets, warnDays, true, nil, nil)
	if err != nil {
		t.Fatalf("collect run2: %v", err)
	}
	if _, err := store.RecordRun(run2Rows); err != nil {
		t.Fatalf("RecordRun 2: %v", err)
	}

	latest, previous, latestAt, previousAt, err := store.LastTwoRuns()
	if err != nil {
		t.Fatalf("LastTwoRuns: %v", err)
	}
	if len(latest) != 3 {
		t.Fatalf("expected 3 latest records, got %d", len(latest))
	}
	if len(previous) != 2 {
		t.Fatalf("expected 2 previous records, got %d", len(previous))
	}
	if latestAt.Before(previousAt) {
		t.Fatalf("expected latestAt >= previousAt, got %v / %v", latestAt, previousAt)
	}

	changes := history.Diff(latest, previous)
	byName := make(map[string]history.Change, len(changes))
	for _, c := range changes {
		byName[c.Identity.Name] = c
	}

	if c, ok := byName["decaying-tls"]; !ok || c.Kind != "status-change" {
		t.Errorf("expected status-change for decaying-tls, got %+v (ok=%v)", c, ok)
	}
	if c, ok := byName["new-tls"]; !ok || c.Kind != "new" {
		t.Errorf("expected new for new-tls, got %+v (ok=%v)", c, ok)
	}
	if _, ok := byName["stable-tls"]; ok {
		t.Errorf("expected no change reported for stable-tls, got one")
	}
}
