// Package history persists scan results to SQLite so successive runs can be
// diffed against each other, turning a point-in-time snapshot into a trend.
package history

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

const schema = `
CREATE TABLE IF NOT EXISTS runs (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	ran_at TIMESTAMP NOT NULL
);
CREATE TABLE IF NOT EXISTS records (
	run_id         INTEGER NOT NULL REFERENCES runs(id),
	source         TEXT NOT NULL,
	cluster        TEXT NOT NULL,
	namespace      TEXT NOT NULL,
	name           TEXT NOT NULL,
	not_after      TIMESTAMP,
	days_remaining INTEGER,
	status         TEXT NOT NULL,
	detail         TEXT
);
CREATE INDEX IF NOT EXISTS idx_records_run ON records(run_id);
CREATE TABLE IF NOT EXISTS settings (
	id              INTEGER PRIMARY KEY CHECK (id = 1),
	warn_days       INTEGER NOT NULL,
	include_secrets BOOLEAN NOT NULL,
	webhook_url     TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS clusters (
	label      TEXT PRIMARY KEY,
	kubeconfig BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS endpoints (
	endpoint TEXT PRIMARY KEY
);
`

// Store wraps a SQLite-backed history database.
type Store struct {
	db        *sql.DB
	retention time.Duration // 0 = keep every run forever (the default)
}

// Open opens (creating if needed) the SQLite database at path and ensures
// the schema exists. path's parent directory is created if it doesn't
// exist yet — the default path lives under the OS's per-user config
// directory (see cmd's defaultDBPath), which is otherwise empty on
// first run.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("creating history db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening history db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("applying history schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// SetRetention bounds how long recorded runs are kept: RecordRun prunes
// anything older than d after each successful insert. Zero (the default)
// disables pruning entirely — used by the `scan --record`/`history diff`
// CLI path, which wants every run kept indefinitely; the dashboard's
// automatic recorder sets this to bound the now-continuously-growing
// table.
func (s *Store) SetRetention(d time.Duration) {
	s.retention = d
}

// RecordRun stores rows as a new run, stamped with the current time, then
// (if a retention window is set) prunes runs older than that window.
func (s *Store) RecordRun(rows []output.Row) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`INSERT INTO runs (ran_at) VALUES (?)`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("inserting run: %w", err)
	}
	runID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO records (run_id, source, cluster, namespace, name, not_after, days_remaining, status, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, r := range rows {
		var notAfter any
		if !r.NotAfter.IsZero() {
			notAfter = r.NotAfter.UTC()
		}
		var days any
		if !r.NotAfter.IsZero() {
			days = r.DaysRemaining()
		}
		if _, err := stmt.Exec(runID, r.Source, r.Cluster, r.Namespace, r.Name, notAfter, days, r.Status, r.Detail); err != nil {
			return 0, fmt.Errorf("inserting record: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	if s.retention > 0 {
		if err := s.pruneOlderThan(time.Now().UTC().Add(-s.retention)); err != nil {
			return runID, fmt.Errorf("pruning old runs: %w", err)
		}
	}

	return runID, nil
}

// pruneOlderThan deletes every run (and its records) stamped before cutoff.
func (s *Store) pruneOlderThan(cutoff time.Time) error {
	if _, err := s.db.Exec(`DELETE FROM records WHERE run_id IN (SELECT id FROM runs WHERE ran_at < ?)`, cutoff); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM runs WHERE ran_at < ?`, cutoff)
	return err
}

// Record is one persisted row from a past run.
type Record struct {
	Source        string
	Cluster       string
	Namespace     string
	Name          string
	DaysRemaining *int
	Status        string
	Detail        string
}

// Identity is the key used to match a record across runs.
func (r Record) Identity() string {
	return r.Source + "|" + r.Cluster + "|" + r.Namespace + "|" + r.Name
}

// LastTwoRuns returns the records for the two most recent runs, newest first.
// If fewer than two runs exist, the missing slots come back empty.
func (s *Store) LastTwoRuns() (latest, previous []Record, latestAt, previousAt time.Time, err error) {
	runRows, err := s.db.Query(`SELECT id, ran_at FROM runs ORDER BY id DESC LIMIT 2`)
	if err != nil {
		return nil, nil, time.Time{}, time.Time{}, fmt.Errorf("querying runs: %w", err)
	}
	defer runRows.Close()

	var ids []int64
	var ats []time.Time
	for runRows.Next() {
		var id int64
		var at time.Time
		if err := runRows.Scan(&id, &at); err != nil {
			return nil, nil, time.Time{}, time.Time{}, err
		}
		ids = append(ids, id)
		ats = append(ats, at)
	}

	if len(ids) == 0 {
		return nil, nil, time.Time{}, time.Time{}, nil
	}

	latest, err = s.recordsForRun(ids[0])
	if err != nil {
		return nil, nil, time.Time{}, time.Time{}, err
	}
	latestAt = ats[0]

	if len(ids) < 2 {
		return latest, nil, latestAt, time.Time{}, nil
	}

	previous, err = s.recordsForRun(ids[1])
	if err != nil {
		return nil, nil, time.Time{}, time.Time{}, err
	}
	previousAt = ats[1]

	return latest, previous, latestAt, previousAt, nil
}

func (s *Store) recordsForRun(runID int64) ([]Record, error) {
	rows, err := s.db.Query(`
		SELECT source, cluster, namespace, name, days_remaining, status, detail
		FROM records WHERE run_id = ?
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		var days sql.NullInt64
		if err := rows.Scan(&r.Source, &r.Cluster, &r.Namespace, &r.Name, &days, &r.Status, &r.Detail); err != nil {
			return nil, err
		}
		if days.Valid {
			d := int(days.Int64)
			r.DaysRemaining = &d
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// runsBefore returns up to n runs at or before before (or the n most recent
// runs if before is 0), newest first. before is inclusive — Events resumes
// paging from a run that was already used as the older half of a diffed
// pair, and must be able to pair it again as the newer half of the next
// one, or that pair would be silently skipped at every page boundary.
func (s *Store) runsBefore(before int64, n int) (ids []int64, ats []time.Time, err error) {
	var rows *sql.Rows
	if before == 0 {
		rows, err = s.db.Query(`SELECT id, ran_at FROM runs ORDER BY id DESC LIMIT ?`, n)
	} else {
		rows, err = s.db.Query(`SELECT id, ran_at FROM runs WHERE id <= ? ORDER BY id DESC LIMIT ?`, before, n)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("querying runs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		ats = append(ats, at)
	}
	return ids, ats, rows.Err()
}

// eventBatchSize is how many runs Events fetches per round trip while
// walking backward for changes — most consecutive runs have no changes at
// all (certs rarely move between scans), so a single batch, not one query
// per run, keeps that common case cheap.
const eventBatchSize = 50

// maxRunsPerEventsCall bounds how many runs a single Events call will walk
// looking for `limit` changes, so a long quiet stretch of history can't turn
// one request into an unbounded table scan — callers page further back via
// the returned nextBefore cursor instead.
const maxRunsPerEventsCall = 500

// Event is one detected change, in its recorded chronological context —
// the atomic unit of the dashboard's audit-trail view.
type Event struct {
	Change
	At    time.Time
	RunID int64
}

// Events returns up to limit Changes across consecutive recorded runs,
// walking backward from before (or the newest run, if before == 0), newest
// first, for the dashboard's audit-trail view. nextBefore is the cursor to
// pass back in to keep paging; it's 0 once there's no more history to walk.
func (s *Store) Events(limit int, before int64) (events []Event, nextBefore int64, err error) {
	cursor := before
	scanned := 0

	for len(events) < limit && scanned < maxRunsPerEventsCall {
		ids, ats, err := s.runsBefore(cursor, eventBatchSize+1)
		if err != nil {
			return nil, 0, err
		}
		if len(ids) < 2 {
			return events, 0, nil // fewer than one full pair left — history exhausted
		}

		for i := 0; i < len(ids)-1; i++ {
			cur, err := s.recordsForRun(ids[i])
			if err != nil {
				return nil, 0, err
			}
			prev, err := s.recordsForRun(ids[i+1])
			if err != nil {
				return nil, 0, err
			}
			for _, c := range Diff(cur, prev) {
				events = append(events, Event{Change: c, At: ats[i], RunID: ids[i]})
			}
			if len(events) >= limit {
				// ids[i+1] anchors the next page: it's the older half of the
				// pair we just diffed, so resuming from it neither skips nor
				// re-emits anything already returned.
				return events, ids[i+1], nil
			}
		}

		scanned += len(ids) - 1
		cursor = ids[len(ids)-1]
		if len(ids) <= eventBatchSize {
			return events, 0, nil // fetched fewer than requested — reached the end
		}
	}

	return events, cursor, nil
}

// Change describes how one certificate's tracked state moved between runs.
type Change struct {
	Identity  Record
	Kind      string // "new" | "removed" | "status-change" | "expiry-shift"
	FromDays  *int
	ToDays    *int
	FromState string
	ToState   string
}

// Diff compares latest against previous and returns every certificate whose
// status changed, whose expiry moved by more than a day, or that appeared
// or disappeared between the two runs.
func Diff(latest, previous []Record) []Change {
	prevByID := make(map[string]Record, len(previous))
	for _, r := range previous {
		prevByID[r.Identity()] = r
	}
	latestByID := make(map[string]Record, len(latest))
	for _, r := range latest {
		latestByID[r.Identity()] = r
	}

	var changes []Change

	for _, cur := range latest {
		id := cur.Identity()
		prev, ok := prevByID[id]
		if !ok {
			changes = append(changes, Change{Identity: cur, Kind: "new", ToDays: cur.DaysRemaining, ToState: cur.Status})
			continue
		}
		if prev.Status != cur.Status {
			changes = append(changes, Change{
				Identity: cur, Kind: "status-change",
				FromState: prev.Status, ToState: cur.Status,
				FromDays: prev.DaysRemaining, ToDays: cur.DaysRemaining,
			})
			continue
		}
		if daysShifted(prev.DaysRemaining, cur.DaysRemaining) {
			changes = append(changes, Change{
				Identity: cur, Kind: "expiry-shift",
				FromState: prev.Status, ToState: cur.Status,
				FromDays: prev.DaysRemaining, ToDays: cur.DaysRemaining,
			})
		}
	}

	for _, prev := range previous {
		if _, ok := latestByID[prev.Identity()]; !ok {
			changes = append(changes, Change{Identity: prev, Kind: "removed", FromDays: prev.DaysRemaining, FromState: prev.Status})
		}
	}

	return changes
}

// daysShifted reports a meaningful expiry-date change: a difference of more
// than one day (a bare re-scan a few hours later shouldn't count), or a
// renewal that flipped between having and not having a NotAfter at all.
func daysShifted(prev, cur *int) bool {
	if (prev == nil) != (cur == nil) {
		return true
	}
	if prev == nil {
		return false
	}
	delta := *cur - *prev
	if delta < 0 {
		delta = -delta
	}
	return delta > 1
}

// SaveSettings persists the dashboard's live-editable scan configuration so
// it survives a restart, replacing whatever was saved before.
func (s *Store) SaveSettings(warnDays int, includeSecrets bool, webhookURL string) error {
	_, err := s.db.Exec(`
		INSERT INTO settings (id, warn_days, include_secrets, webhook_url) VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET warn_days = excluded.warn_days,
			include_secrets = excluded.include_secrets, webhook_url = excluded.webhook_url
	`, warnDays, includeSecrets, webhookURL)
	if err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	return nil
}

// LoadSettings returns the persisted settings, if any. ok is false when
// nothing has been saved yet (e.g. first run against a fresh database).
func (s *Store) LoadSettings() (warnDays int, includeSecrets bool, webhookURL string, ok bool, err error) {
	row := s.db.QueryRow(`SELECT warn_days, include_secrets, webhook_url FROM settings WHERE id = 1`)
	if err := row.Scan(&warnDays, &includeSecrets, &webhookURL); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, "", false, nil
		}
		return 0, false, "", false, fmt.Errorf("loading settings: %w", err)
	}
	return warnDays, includeSecrets, webhookURL, true, nil
}

// StoredCluster is a persisted cluster added through the "Add cluster" UI.
type StoredCluster struct {
	Label      string
	Kubeconfig []byte
}

// SaveCluster persists a cluster's kubeconfig, keyed by label, so it's
// reloaded on the next run instead of only lasting until restart.
func (s *Store) SaveCluster(label string, kubeconfig []byte) error {
	_, err := s.db.Exec(`
		INSERT INTO clusters (label, kubeconfig) VALUES (?, ?)
		ON CONFLICT(label) DO UPDATE SET kubeconfig = excluded.kubeconfig
	`, label, kubeconfig)
	if err != nil {
		return fmt.Errorf("saving cluster %s: %w", label, err)
	}
	return nil
}

// ListClusters returns every persisted cluster upload.
func (s *Store) ListClusters() ([]StoredCluster, error) {
	rows, err := s.db.Query(`SELECT label, kubeconfig FROM clusters ORDER BY label`)
	if err != nil {
		return nil, fmt.Errorf("listing clusters: %w", err)
	}
	defer rows.Close()

	var out []StoredCluster
	for rows.Next() {
		var c StoredCluster
		if err := rows.Scan(&c.Label, &c.Kubeconfig); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCluster removes a persisted cluster upload, so it's no longer
// reloaded on the next run. No-op (no error) if the label isn't there.
func (s *Store) DeleteCluster(label string) error {
	if _, err := s.db.Exec(`DELETE FROM clusters WHERE label = ?`, label); err != nil {
		return fmt.Errorf("deleting cluster %s: %w", label, err)
	}
	return nil
}

// SaveEndpoint persists a probed endpoint so it's reloaded on the next run.
func (s *Store) SaveEndpoint(endpoint string) error {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO endpoints (endpoint) VALUES (?)`, endpoint); err != nil {
		return fmt.Errorf("saving endpoint %s: %w", endpoint, err)
	}
	return nil
}

// ListEndpoints returns every persisted endpoint.
func (s *Store) ListEndpoints() ([]string, error) {
	rows, err := s.db.Query(`SELECT endpoint FROM endpoints ORDER BY endpoint`)
	if err != nil {
		return nil, fmt.Errorf("listing endpoints: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteEndpoint removes a persisted endpoint, so it's no longer reloaded
// on the next run. No-op (no error) if the endpoint isn't there.
func (s *Store) DeleteEndpoint(endpoint string) error {
	if _, err := s.db.Exec(`DELETE FROM endpoints WHERE endpoint = ?`, endpoint); err != nil {
		return fmt.Errorf("deleting endpoint %s: %w", endpoint, err)
	}
	return nil
}
