package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/alert"
	"github.com/thev1ndu/certhealthz/pkg/history"
	"github.com/thev1ndu/certhealthz/pkg/output"
)

var (
	scanWarnDays   int
	scanWebhookURL string
	scanPrometheus bool
	scanIncludeRaw bool
	scanRecord     bool
	scanDBPath     string
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan cert-manager Certificates and kubernetes.io/tls Secrets across one or more clusters",
	RunE:  runScan,
}

func init() {
	scanCmd.Flags().IntVar(&scanWarnDays, "warn-days", 14, "flag certificates expiring within this many days")
	scanCmd.Flags().StringVar(&scanWebhookURL, "webhook", "", "webhook URL to POST flagged certificates to (Slack/ServiceNow/custom)")
	scanCmd.Flags().BoolVar(&scanPrometheus, "prometheus", false, "print Prometheus exposition format instead of a table")
	scanCmd.Flags().BoolVar(&scanIncludeRaw, "include-secrets", true, "also scan raw kubernetes.io/tls Secrets, for Certificate drift detection and Ingress cross-referencing")
	scanCmd.Flags().BoolVar(&scanRecord, "record", false, "persist this scan to the history database for trend diffing (see: certhealthz history diff)")
	scanCmd.Flags().StringVar(&scanDBPath, "db", defaultDBPath(), "path to the SQLite database used by --record and history diff")
	rootCmd.AddCommand(scanCmd)
}

func runScan(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	rows, err := collectRows(ctx, kubeconfigPaths, scanWarnDays, scanIncludeRaw)
	if err != nil {
		return err
	}

	if scanPrometheus {
		output.Prometheus(os.Stdout, rows)
	} else {
		output.Table(rows)
	}

	if scanWebhookURL != "" {
		if err := alert.Send(scanWebhookURL, rows); err != nil {
			return fmt.Errorf("sending alert: %w", err)
		}
	}

	if scanRecord {
		store, err := history.Open(scanDBPath)
		if err != nil {
			return fmt.Errorf("opening history db: %w", err)
		}
		defer store.Close()
		if _, err := store.RecordRun(rows); err != nil {
			return fmt.Errorf("recording run: %w", err)
		}
	}

	return nil
}
