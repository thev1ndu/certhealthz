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

func TestEventsPagination(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	now := time.Now()
	record := func(rows []output.Row) {
		t.Helper()
		if _, err := store.RecordRun(rows); err != nil {
			t.Fatalf("RecordRun: %v", err)
		}
	}

	// run1: a, b
	record([]output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "a", NotAfter: now.Add(10 * 24 * time.Hour), Status: "ok"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "b", NotAfter: now.Add(2 * 24 * time.Hour), Status: "expiring"},
	})
	// run2: b expires, c appears -> 2 changes (status-change b, new c)
	record([]output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "a", NotAfter: now.Add(9 * 24 * time.Hour), Status: "ok"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "b", NotAfter: now.Add(-1 * 24 * time.Hour), Status: "expired"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "c", NotAfter: now.Add(30 * 24 * time.Hour), Status: "ok"},
	})
	// run3: b removed, d appears -> 2 changes (removed b, new d)
	record([]output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "a", NotAfter: now.Add(8 * 24 * time.Hour), Status: "ok"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "c", NotAfter: now.Add(29 * 24 * time.Hour), Status: "ok"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "d", NotAfter: now.Add(5 * 24 * time.Hour), Status: "ok"},
	})
	// run4: c starts expiring -> 1 change (status-change c)
	record([]output.Row{
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "a", NotAfter: now.Add(7 * 24 * time.Hour), Status: "ok"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "c", NotAfter: now.Add(1 * 24 * time.Hour), Status: "expiring"},
		{Source: "cert-manager", Cluster: "prod", Namespace: "ns1", Name: "d", NotAfter: now.Add(4 * 24 * time.Hour), Status: "ok"},
	})

	// 3 consecutive pairs: (run4,run3)=1 event, (run3,run2)=2 events, (run2,run1)=2 events = 5 total.
	all, nextBefore, err := store.Events(100, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 total events, got %d: %+v", len(all), all)
	}
	if nextBefore != 0 {
		t.Errorf("expected nextBefore 0 once history is exhausted, got %d", nextBefore)
	}
	if all[0].Identity.Name != "c" || all[0].Kind != "status-change" {
		t.Errorf("expected newest event to be c's status-change, got %+v", all[0])
	}

	// Page through 2 at a time and confirm the concatenation matches the
	// unpaged result exactly, in the same order.
	var paged []Event
	cursor := int64(0)
	for {
		page, next, err := store.Events(2, cursor)
		if err != nil {
			t.Fatalf("Events(2, %d): %v", cursor, err)
		}
		if len(page) == 0 {
			break
		}
		paged = append(paged, page...)
		cursor = next
		if next == 0 {
			break
		}
	}
	if len(paged) != len(all) {
		t.Fatalf("paged walk collected %d events, expected %d", len(paged), len(all))
	}
	for i := range all {
		if paged[i].Identity.Name != all[i].Identity.Name || paged[i].Kind != all[i].Kind {
			t.Errorf("event %d mismatch: paged=%+v unpaged=%+v", i, paged[i], all[i])
		}
	}
}

func TestPruneOlderThan(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "prune.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	oldRunID, err := store.RecordRun([]output.Row{
		{Source: "secret", Cluster: "prod", Namespace: "ns1", Name: "old", NotAfter: time.Now().Add(24 * time.Hour), Status: "ok"},
	})
	if err != nil {
		t.Fatalf("RecordRun old: %v", err)
	}
	// Backdate the run well outside any retention window under test.
	if _, err := store.db.Exec(`UPDATE runs SET ran_at = ? WHERE id = ?`, time.Now().Add(-48*time.Hour), oldRunID); err != nil {
		t.Fatalf("backdating run: %v", err)
	}

	// Retention set before this RecordRun, so the insert itself should prune
	// the backdated run as a side effect — this exercises the RecordRun ->
	// pruneOlderThan integration, not just pruneOlderThan in isolation.
	store.SetRetention(24 * time.Hour)
	if _, err := store.RecordRun([]output.Row{
		{Source: "secret", Cluster: "prod", Namespace: "ns1", Name: "new", NotAfter: time.Now().Add(24 * time.Hour), Status: "ok"},
	}); err != nil {
		t.Fatalf("RecordRun new: %v", err)
	}

	var runCount, recordCount int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&runCount); err != nil {
		t.Fatalf("counting runs: %v", err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM records WHERE run_id = ?`, oldRunID).Scan(&recordCount); err != nil {
		t.Fatalf("counting old records: %v", err)
	}
	if runCount != 1 {
		t.Errorf("expected 1 run left after pruning, got %d", runCount)
	}
	if recordCount != 0 {
		t.Errorf("expected old run's records pruned, found %d", recordCount)
	}
}
