package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/alert"
	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/ctlog"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/output"
)

var (
	ctSince      time.Duration
	ctWarnDays   int
	ctWebhookURL string
	ctPrometheus bool
)

var ctCmd = &cobra.Command{
	Use:   "ct <domain...>",
	Short: "Check Certificate Transparency logs for certs issued for your domains outside any known cluster",
	Long: `Queries crt.sh for certs recently logged for the given domains. If
--kubeconfig is set, each result is also checked against the DNS names on
your configured clusters' kubernetes.io/tls Secrets: a domain no Secret
actually covers is flagged as possible shadow/rogue issuance. Without
--kubeconfig, results are still reported (informationally) but without that
known/unknown distinction — there's nothing local to compare against.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCT,
}

func init() {
	ctCmd.Flags().DurationVar(&ctSince, "since", 24*time.Hour, "only report certs logged within this window")
	ctCmd.Flags().IntVar(&ctWarnDays, "warn-days", 14, "flag certificates expiring within this many days")
	ctCmd.Flags().StringVar(&ctWebhookURL, "webhook", "", "webhook URL to POST flagged rows to")
	ctCmd.Flags().BoolVar(&ctPrometheus, "prometheus", false, "print Prometheus exposition format instead of a table")
	rootCmd.AddCommand(ctCmd)
}

// knownDNSNames flattens the DNS names on every kubernetes.io/tls Secret
// across all configured --kubeconfig clusters, so a CT log entry can be
// checked against what's actually deployed. Returns nil (not an error) if
// no --kubeconfig was given — that just means the caller can't tell known
// from unknown, not that the command should fail.
func knownDNSNames(ctx context.Context) []string {
	var names []string
	for _, kc := range kubeconfigPaths {
		client, err := certmanager.NewTypedClient(kc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: building client for %s: %v\n", kc, err)
			continue
		}
		secrets, err := certmanager.ScanSecrets(ctx, kc, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: scanning secrets on %s: %v\n", kc, err)
			continue
		}
		for _, s := range secrets {
			names = append(names, s.DNSNames...)
		}
	}
	return names
}

// ctRows queries CT logs for each domain and converts the results into
// rows, sorted and classified. known is the flattened DNS names of Secrets
// the caller already trusts (from knownDNSNames or its dashboard
// equivalent); a nil/empty known skips the known/unknown check entirely and
// every result is just classified by its real expiry.
func ctRows(ctx context.Context, domains []string, since time.Duration, warnDays int, known []string) []output.Row {
	cutoff := time.Now().Add(-since)

	var rows []output.Row
	for _, domain := range domains {
		entries, err := ctlog.Query(ctx, domain)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			continue
		}
		for _, e := range entries {
			if e.NotBefore.Before(cutoff) {
				continue
			}

			// NameValue is newline-separated when a cert covers multiple SANs;
			// flatten it so it doesn't break the table's column alignment.
			name := strings.ReplaceAll(e.NameValue, "\n", ", ")

			row := output.Row{
				Source:   "ct-log",
				Name:     name,
				NotAfter: e.NotAfter,
			}
			if len(known) > 0 && !ingress.AnyHostCovered(known, domain) {
				row.Status = "error"
				row.Detail = fmt.Sprintf(
					"%s not covered by any configured cluster's Secret — possible shadow/rogue issuance (issuer: %s, logged %s)",
					domain, e.Issuer, e.NotBefore.Format(time.RFC3339),
				)
			} else {
				row.Detail = fmt.Sprintf("issuer: %s, logged %s", e.Issuer, e.NotBefore.Format(time.RFC3339))
				row = output.Classify(row, warnDays)
			}
			rows = append(rows, row)
		}
	}

	output.Sort(rows)
	return rows
}

func runCT(cmd *cobra.Command, domains []string) error {
	ctx := cmd.Context()
	known := knownDNSNames(ctx)
	rows := ctRows(ctx, domains, ctSince, ctWarnDays, known)

	if ctPrometheus {
		output.Prometheus(os.Stdout, rows)
	} else {
		output.Table(rows)
	}

	if ctWebhookURL != "" {
		if err := alert.Send(ctWebhookURL, rows); err != nil {
			return fmt.Errorf("sending alert: %w", err)
		}
	}

	return nil
}
