package cmd

import (
	"github.com/spf13/cobra"
)

var kubeconfigPaths []string

var rootCmd = &cobra.Command{
	Use:   "certhealthz",
	Short: "certhealthz — multi-cluster TLS/cert-manager expiry radar",
	Long: `certhealthz scans cert-manager Certificates, raw kubernetes.io/tls Secrets,
and live TLS endpoints across one or more clusters, and reports certificates
approaching expiry or in a broken renewal state.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringSliceVar(&kubeconfigPaths, "kubeconfig", nil,
		"path(s) to kubeconfig file(s); repeat flag for multiple clusters (default: $KUBECONFIG or ~/.kube/config)")
}
