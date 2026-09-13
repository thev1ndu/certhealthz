package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/alert"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"github.com/thev1ndu/certhealthz/pkg/probe"
)

var (
	probeWarnDays   int
	probeWebhookURL string
	probePrometheus bool
	probeTimeout    time.Duration
)

var probeCmd = &cobra.Command{
	Use:   "probe [endpoint...]",
	Short: "Probe live TLS endpoints (host or host:port) and report leaf certificate expiry",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runProbe,
}

func init() {
	probeCmd.Flags().IntVar(&probeWarnDays, "warn-days", 14, "flag certificates expiring within this many days")
	probeCmd.Flags().StringVar(&probeWebhookURL, "webhook", "", "webhook URL to POST flagged certificates to")
	probeCmd.Flags().BoolVar(&probePrometheus, "prometheus", false, "print Prometheus exposition format instead of a table")
	probeCmd.Flags().DurationVar(&probeTimeout, "timeout", 5*time.Second, "per-endpoint dial timeout")
	rootCmd.AddCommand(probeCmd)
}

func runProbe(_ *cobra.Command, endpoints []string) error {
	results := probe.ProbeAll(endpoints, probeTimeout)

	var rows []output.Row
	for _, r := range results {
		row := output.Row{
			Source: "endpoint",
			Name:   r.Endpoint,
			Detail: r.Issuer,
		}
		if r.Err != nil {
			row.Status = "error"
			row.Detail = r.Err.Error()
			rows = append(rows, row)
			continue
		}
		row.NotAfter = r.NotAfter
		row = output.Classify(row, probeWarnDays)
		rows = append(rows, row)
	}

	output.Sort(rows)

	if probePrometheus {
		output.Prometheus(os.Stdout, rows)
	} else {
		output.Table(rows)
	}

	if probeWebhookURL != "" {
		if err := alert.Send(probeWebhookURL, rows); err != nil {
			return fmt.Errorf("sending alert: %w", err)
		}
	}

	return nil
}
