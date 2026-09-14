package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"github.com/thev1ndu/certhealthz/pkg/probe"
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

	// Prefer the kubeconfig's own current-context cluster name (e.g.
	// "kubernetes") over label, which is just the kubeconfig's path or
	// upload filename (e.g. "admin.conf") and rarely matches the cluster.
	clusterLabel := label
	if kubeconfig != nil {
		if name := certmanager.ClusterNameFromBytes(kubeconfig); name != "" {
			clusterLabel = name
		}
	} else if name := certmanager.ClusterName(path); name != "" {
		clusterLabel = name
	}

	cc := ClusterClients{Label: clusterLabel, Dyn: dynClient}

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

// buildAllClusterClients builds ClusterClients for every configured cluster
// entry (falling back to a single default-loading-rules entry if none are
// configured). Shared by the dashboard's collect closure and the certificate
// detail endpoint, which both need to re-derive each cluster's real label
// (buildClusterClients may rename it from the kubeconfig's own context) to
// find the right one.
func buildAllClusterClients(entries []ClusterEntry, includeSecrets bool) ([]ClusterClients, error) {
	if len(entries) == 0 {
		entries = []ClusterEntry{{}} // empty label => default loading rules
	}

	clients := make([]ClusterClients, 0, len(entries))
	for _, e := range entries {
		label := e.Label
		if label == "" {
			label = "default"
		}
		cc, err := buildClusterClients(label, e.Kubeconfig, e.Label, includeSecrets)
		if err != nil {
			return nil, err
		}
		clients = append(clients, cc)
	}
	return clients, nil
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

		// Scanned up front (when requested) so the cert-manager loop below can
		// cross-check each Ready Certificate against its backing Secret's
		// actual leaf cert, not just trust the Certificate's own status.
		var secrets []certmanager.SecretCert
		var secretsByKey map[string]certmanager.SecretCert
		if includeSecrets {
			var err error
			secrets, err = certmanager.ScanSecrets(ctx, clusterLabel, target.Typed)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
			secretsByKey = make(map[string]certmanager.SecretCert, len(secrets))
			for _, s := range secrets {
				secretsByKey[s.Namespace+"/"+s.Name] = s
			}
		}

		certs, err := certmanager.Scan(ctx, clusterLabel, target.Dyn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		for _, c := range certs {
			status := "ok"
			detail := ""
			switch {
			case !c.Ready:
				status = "error"
				detail = "not ready: " + c.FailReason
			case includeSecrets:
				if drifted, why := certmanager.CheckDrift(c, secretsByKey); drifted {
					status = "drift"
					detail = why
				}
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
			switch {
			case status == "error" || status == "drift":
				// keep the reason already set above
			case includeSecrets:
				if sc, ok := secretsByKey[c.Namespace+"/"+c.SecretName]; ok {
					row = classifySecret(row, sc, warnDays)
				} else {
					row = output.Classify(row, warnDays)
				}
			default:
				row = output.Classify(row, warnDays)
			}
			rows = append(rows, row)
		}

		issuers, err := certmanager.ScanIssuers(ctx, clusterLabel, target.Dyn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		rows = append(rows, issuerRows(issuers)...)

		clusterIssuers, err := certmanager.ScanClusterIssuers(ctx, clusterLabel, target.Dyn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		rows = append(rows, issuerRows(clusterIssuers)...)

		if includeSecrets {
			for _, s := range secrets {
				row := output.Row{
					Source:    "secret",
					Cluster:   s.Cluster,
					Namespace: s.Namespace,
					Name:      s.Name,
					NotAfter:  s.NotAfter,
				}
				row = classifySecret(row, s, warnDays)
				rows = append(rows, row)
			}

			routes, err := ingress.Scan(ctx, clusterLabel, target.Typed)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
			rows = append(rows, ingressRoutesRows(routes, secretsByKey, warnDays)...)
		}
	}

	output.Sort(rows)
	return rows, nil
}

// probeRows runs probe.ProbeAll against endpoints and converts the results
// into classified rows. Shared by `probe` and the dashboard's live
// endpoint list so both report a probed endpoint the same way.
func probeRows(endpoints []string, timeout time.Duration, warnDays int) []output.Row {
	results := probe.ProbeAll(endpoints, timeout)
	rows := make([]output.Row, 0, len(results))
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
		row = output.Classify(row, warnDays)
		rows = append(rows, row)
	}
	return rows
}

// issuerRows converts scanned Issuer/ClusterIssuer health into rows. These
// have no expiry — NotAfter stays zero — which is already a fully tolerated
// shape end-to-end: output.Sort already sinks zero-NotAfter rows last, and
// the frontend already renders "—" for a null Days (the same shape a
// not-Ready cert-manager Certificate already produces today).
func issuerRows(issuers []certmanager.IssuerHealth) []output.Row {
	rows := make([]output.Row, 0, len(issuers))
	for _, h := range issuers {
		source := "issuer"
		namespace := h.Namespace
		if h.Kind == "ClusterIssuer" {
			source = "clusterissuer"
			namespace = ""
		}
		row := output.Row{
			Source:    source,
			Cluster:   h.Cluster,
			Namespace: namespace,
			Name:      h.Name,
			Status:    "ok",
		}
		if !h.Ready {
			row.Status = "error"
			row.Detail = "not ready: " + h.FailReason
		}
		rows = append(rows, row)
	}
	return rows
}

// classifySecret runs the normal expiry-based Classify, then — only if the
// cert isn't already expired or erroring — folds in the crypto/chain
// findings a Secret's parsed cert already carries. Expiry always wins over
// crypto findings: an expired cert is failing handshakes right now, which
// is more urgent than a lower-priority latent risk like a weak key.
func classifySecret(row output.Row, sc certmanager.SecretCert, warnDays int) output.Row {
	row = output.Classify(row, warnDays)
	if row.Status == "expired" || row.Status == "error" {
		return row
	}
	switch {
	case !sc.ChainOK:
		row.Status = "broken-chain"
		row.Detail = sc.ChainIssue
	case sc.WeakCrypto:
		row.Status = "weak-crypto"
		row.Detail = sc.CryptoIssue
	}
	return row
}

// ingressRoutesRows cross-checks each Ingress TLS route against its backing
// Secret: missing entirely, or present but not actually covering the
// route's host (wrong/no matching SAN). One row per route×host, or one row
// for the route itself if it declares no hosts. A route whose Secret is
// present and covers every declared host is still reported, classified by
// that Secret's real expiry — the same tiers as a plain secret row.
func ingressRoutesRows(routes []ingress.Route, secretsByKey map[string]certmanager.SecretCert, warnDays int) []output.Row {
	var rows []output.Row
	for _, route := range routes {
		key := route.Namespace + "/" + route.SecretName
		secret, ok := secretsByKey[key]

		hosts := route.Hosts
		if len(hosts) == 0 {
			hosts = []string{""}
		}

		for _, host := range hosts {
			name := route.Ingress
			if host != "" {
				name = fmt.Sprintf("%s (%s)", route.Ingress, host)
			}
			row := output.Row{
				Source:    "ingress",
				Cluster:   route.Cluster,
				Namespace: route.Namespace,
				Name:      name,
			}
			switch {
			case !ok:
				row.Status = "error"
				row.Detail = fmt.Sprintf("Secret %s not found", key)
			case host != "" && !ingress.AnyHostCovered(secret.DNSNames, host):
				row.NotAfter = secret.NotAfter
				row.Status = "error"
				row.Detail = fmt.Sprintf("host %s not covered by Secret %s's certificate SANs %v", host, key, secret.DNSNames)
			default:
				row.NotAfter = secret.NotAfter
				row = classifySecret(row, secret, warnDays)
			}
			rows = append(rows, row)
		}
	}
	return rows
}
