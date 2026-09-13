package output

import (
	"fmt"
	"io"
	"strings"
)

// Prometheus writes rows as a single gauge metric, cert_expiry_days,
// in text exposition format — pluggable straight into an existing
// Prometheus/Grafana stack via a file_sd or pushgateway scrape.
func Prometheus(w io.Writer, rows []Row) {
	fmt.Fprintln(w, "# HELP cert_expiry_days Days remaining until certificate expiry (negative if expired)")
	fmt.Fprintln(w, "# TYPE cert_expiry_days gauge")
	for _, r := range rows {
		if r.NotAfter.IsZero() {
			continue
		}
		fmt.Fprintf(w, "cert_expiry_days{source=%q,cluster=%q,namespace=%q,name=%q} %d\n",
			r.Source, r.Cluster, sanitize(r.Namespace), r.Name, r.DaysRemaining())
	}
}

func sanitize(s string) string {
	if s == "" {
		return "-"
	}
	return strings.ReplaceAll(s, `"`, `'`)
}
