package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// ClusterClients bundles the clients CollectRowsFromClients needs for one
// cluster, decoupling the scan loop from how those clients were built —
// real kubeconfig-backed clients in production, fakes in e2e tests.
type ClusterClients struct {
	Label string
	Dyn   dynamic.Interface
	Typed kubernetes.Interface
}

// buildClusterClients builds the dynamic (and, if requested, typed) client
// for one cluster target. If kubeconfig is non-nil, the client is built
// from that in-memory content (e.g. a dashboard file upload); otherwise
// it's built from path (a filesystem path, or "" for the default loading
// rules).
func buildClusterClients(label string, kubeconfig []byte, path string, includeSecrets bool) (ClusterClients, error) {
	var dynClient dynamic.Interface
	var err error
	if kubeconfig != nil {
		dynClient, err = certmanager.NewDynamicClientFromBytes(kubeconfig)
	} else {
		dynClient, err = certmanager.NewDynamicClient(path)
	}
	if err != nil {
		return ClusterClients{}, fmt.Errorf("building client for %s: %w", label, err)
	}

	cc := ClusterClients{Label: label, Dyn: dynClient}

	if includeSecrets {
		var typedClient kubernetes.Interface
		if kubeconfig != nil {
			typedClient, err = certmanager.NewTypedClientFromBytes(kubeconfig)
		} else {
			typedClient, err = certmanager.NewTypedClient(path)
		}
		if err != nil {
			return ClusterClients{}, fmt.Errorf("building typed client for %s: %w", label, err)
		}
		cc.Typed = typedClient
	}

	return cc, nil
}

// collectRows scans every configured kubeconfig target (or the default
// context if none were given) for cert-manager Certificates and, if
// requested, raw kubernetes.io/tls Secrets, returning a unified, sorted
// list of rows. It's shared by `scan` and `dashboard` so both report the
// same data the same way.
func collectRows(ctx context.Context, kubeconfigs []string, warnDays int, includeSecrets bool) ([]output.Row, error) {
	targets := kubeconfigs
	if len(targets) == 0 {
		targets = []string{""} // empty => default loading rules
	}

	clients := make([]ClusterClients, 0, len(targets))
	for _, kc := range targets {
		clusterLabel := kc
		if clusterLabel == "" {
			clusterLabel = "default"
		}

		cc, err := buildClusterClients(clusterLabel, nil, kc, includeSecrets)
		if err != nil {
			return nil, err
		}

		clients = append(clients, cc)
	}

	return CollectRowsFromClients(ctx, clients, warnDays, includeSecrets)
}

// CollectRowsFromClients runs the actual cert-manager/Secret scan and
// classification logic against already-built clients, one per cluster. It's
// the seam that lets e2e tests exercise the real scan pipeline against fake
// clientsets instead of a real cluster.
func CollectRowsFromClients(ctx context.Context, targets []ClusterClients, warnDays int, includeSecrets bool) ([]output.Row, error) {
	var rows []output.Row

	for _, target := range targets {
		clusterLabel := target.Label

		certs, err := certmanager.Scan(ctx, clusterLabel, target.Dyn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		for _, c := range certs {
			status := "ok"
			detail := ""
			if !c.Ready {
				status = "error"
				detail = "not ready: " + c.FailReason
			}
			row := output.Row{
				Source:    "cert-manager",
				Cluster:   c.Cluster,
				Namespace: c.Namespace,
				Name:      c.Name,
				NotAfter:  c.NotAfter,
				Status:    status,
				Detail:    detail,
			}
			if status != "error" {
				row = output.Classify(row, warnDays)
			}
			rows = append(rows, row)
		}

		if includeSecrets {
			secrets, err := certmanager.ScanSecrets(ctx, clusterLabel, target.Typed)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
			for _, s := range secrets {
				row := output.Row{
					Source:    "secret",
					Cluster:   s.Cluster,
					Namespace: s.Namespace,
					Name:      s.Name,
					NotAfter:  s.NotAfter,
				}
				row = output.Classify(row, warnDays)
				rows = append(rows, row)
			}
		}
	}

	output.Sort(rows)
	return rows, nil
}
