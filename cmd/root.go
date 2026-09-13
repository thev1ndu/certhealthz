package cmd

import (
	"github.com/spf13/cobra"
)

var kubeconfigPaths []string

// Version is the certhealthz release version. Overridden at build time via
// -ldflags "-X github.com/thev1ndu/certhealthz/cmd.Version=vX.Y.Z" (see
// .goreleaser.yaml); a source build or `go run` keeps the "dev" default.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:     "certhealthz",
	Short:   "CertHealthz - Live TLS certificate health across clusters",
	Version: Version,
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
