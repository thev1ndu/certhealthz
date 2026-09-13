package output

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"
)

// Row is a single unified expiry-report line, regardless of whether it
// came from a cert-manager Certificate, a raw Secret, or a live endpoint probe.
type Row struct {
	Source    string // "cert-manager" | "secret" | "endpoint"
	Cluster   string
	Namespace string
	Name      string
	NotAfter  time.Time
	Status    string // "ok" | "expiring" | "expired" | "error"
	Detail    string
}

// DaysRemaining returns whole days until NotAfter, negative if already past.
func (r Row) DaysRemaining() int {
	return int(time.Until(r.NotAfter).Hours() / 24)
}

// Sort orders rows soonest-to-expire first; zero NotAfter (errors) sink last.
func Sort(rows []Row) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].NotAfter.IsZero() {
			return false
		}
		if rows[j].NotAfter.IsZero() {
			return true
		}
		return rows[i].NotAfter.Before(rows[j].NotAfter)
	})
}

// Table prints a human-readable table to stdout.
func Table(rows []Row) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "SOURCE\tCLUSTER\tNAMESPACE\tNAME\tDAYS LEFT\tSTATUS\tDETAIL")
	for _, r := range rows {
		days := "?"
		if !r.NotAfter.IsZero() {
			days = fmt.Sprintf("%d", r.DaysRemaining())
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Source, r.Cluster, r.Namespace, r.Name, days, r.Status, r.Detail)
	}
	_ = w.Flush()
}

// Classify assigns a status tier based on days remaining against the
// given warning threshold in days.
func Classify(r Row, warnDays int) Row {
	if r.NotAfter.IsZero() {
		r.Status = "error"
		return r
	}
	days := r.DaysRemaining()
	switch {
	case days < 0:
		r.Status = "expired"
	case days <= warnDays:
		r.Status = "expiring"
	default:
		r.Status = "ok"
	}
	return r
}
