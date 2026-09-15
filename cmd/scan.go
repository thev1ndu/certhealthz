package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thev1ndu/certhealthz/pkg/alert"
	"github.com/thev1ndu/certhealthz/pkg/cloudcert"
	"github.com/thev1ndu/certhealthz/pkg/history"
	"github.com/thev1ndu/certhealthz/pkg/output"
)

var (
	scanWarnDays           int
	scanWebhookURL         string
	scanPrometheus         bool
	scanIncludeRaw         bool
	scanRecord             bool
	scanDBPath             string
	scanRequireLabels      []string
	scanAWSRegions         []string
	scanGCPProject         string
	scanAzureVaultURLs     []string
	scanMTLSSecretSelector []string
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
	scanCmd.Flags().StringSliceVar(&scanRequireLabels, "require-label", nil, "label key every cert-manager Certificate must carry (value not checked); repeat flag for multiple. Unset disables the check")
	scanCmd.Flags().StringSliceVar(&scanAWSRegions, "aws-region", nil, "AWS region to scan Certificate Manager (ACM) in, using the default AWS credential chain; repeat flag for multiple. Unset disables AWS scanning entirely")
	scanCmd.Flags().StringVar(&scanGCPProject, "gcp-project", "", "GCP project to scan Certificate Manager in, using Application Default Credentials. Unset disables GCP scanning entirely")
	scanCmd.Flags().StringSliceVar(&scanAzureVaultURLs, "azure-vault-url", nil, "Azure Key Vault URL to scan for certificates, using azidentity's default credential chain; repeat flag for multiple. Unset disables Azure scanning entirely")
	scanCmd.Flags().StringSliceVar(&scanMTLSSecretSelector, "mtls-secret-selector", nil, "label selector (e.g. app=my-client) matching kubernetes.io/tls Secrets that hold mTLS client certificates, tracked separately from server certs; repeat flag for multiple. Unset disables mTLS tracking")
	rootCmd.AddCommand(scanCmd)
}

func runScan(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	rows, err := collectRows(ctx, kubeconfigPaths, scanWarnDays, scanIncludeRaw, scanRequireLabels, scanMTLSSecretSelector)
	if err != nil {
		return err
	}

	for _, region := range scanAWSRegions {
		awsRows, err := cloudcert.ScanACM(ctx, region, scanWarnDays)
		if err != nil {
			warnScan(err)
			continue
		}
		rows = append(rows, awsRows...)
	}
	if scanGCPProject != "" {
		gcpRows, err := cloudcert.ScanGCP(ctx, scanGCPProject, scanWarnDays)
		if err != nil {
			warnScan(err)
		} else {
			rows = append(rows, gcpRows...)
		}
	}
	for _, vaultURL := range scanAzureVaultURLs {
		azureRows, err := cloudcert.ScanAzureKeyVault(ctx, vaultURL, scanWarnDays)
		if err != nil {
			warnScan(err)
			continue
		}
		rows = append(rows, azureRows...)
	}
	output.Sort(rows)

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
