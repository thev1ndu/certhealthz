package history

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

func TestRecordRunAndDiff(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	run1 := []output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "a", NotAfter: time.Now().Add(10 * 24 * time.Hour), Status: "ok"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "b", NotAfter: time.Now().Add(2 * 24 * time.Hour), Status: "expiring"},
	}
	if _, err := store.RecordRun(run1); err != nil {
		t.Fatalf("RecordRun 1: %v", err)
	}

	run2 := []output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "a", NotAfter: time.Now().Add(9 * 24 * time.Hour), Status: "ok"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "b", NotAfter: time.Now().Add(-1 * 24 * time.Hour), Status: "expired"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "c", NotAfter: time.Now().Add(30 * 24 * time.Hour), Status: "ok"},
	}
	if _, err := store.RecordRun(run2); err != nil {
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
	if !latestAt.After(previousAt) && !latestAt.Equal(previousAt) {
		t.Fatalf("expected latestAt >= previousAt, got %v / %v", latestAt, previousAt)
	}

	changes := Diff(latest, previous)

	byKey := make(map[string]Change, len(changes))
	for _, c := range changes {
		byKey[c.Identity.Name] = c
	}

	if c, ok := byKey["b"]; !ok || c.Kind != "status-change" {
		t.Errorf("expected status-change for b, got %+v (ok=%v)", c, ok)
	}
	if c, ok := byKey["c"]; !ok || c.Kind != "new" {
		t.Errorf("expected new for c, got %+v (ok=%v)", c, ok)
	}
	if _, ok := byKey["a"]; ok {
		t.Errorf("expected no change reported for a (only shifted by 1 day), got one")
	}
}

func TestLastTwoRunsEmpty(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	latest, previous, _, _, err := store.LastTwoRuns()
	if err != nil {
		t.Fatalf("LastTwoRuns: %v", err)
	}
	if latest != nil || previous != nil {
		t.Fatalf("expected nil/nil on empty db, got %v / %v", latest, previous)
	}
}
