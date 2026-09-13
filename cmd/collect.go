package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/output"
)

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

	var rows []output.Row

	for _, kc := range targets {
		clusterLabel := kc
		if clusterLabel == "" {
			clusterLabel = "default"
		}

		dynClient, err := certmanager.NewDynamicClient(kc)
		if err != nil {
			return nil, fmt.Errorf("building client for %s: %w", clusterLabel, err)
		}

		certs, err := certmanager.Scan(ctx, clusterLabel, dynClient)
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
			typedClient, err := certmanager.NewTypedClient(kc)
			if err != nil {
				return nil, fmt.Errorf("building typed client for %s: %w", clusterLabel, err)
			}
			secrets, err := certmanager.ScanSecrets(ctx, clusterLabel, typedClient)
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
