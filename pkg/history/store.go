// Package history persists scan results to SQLite so successive runs can be
// diffed against each other, turning a point-in-time snapshot into a trend.
package history

import (
	"database/sql"
	"fmt"
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
`

// Store wraps a SQLite-backed history database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and ensures
// the schema exists.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening history db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying history schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// RecordRun stores rows as a new run, stamped with the current time.
func (s *Store) RecordRun(rows []output.Row) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

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
	return runID, nil
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
