package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/gateway"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/mesh"
	"github.com/thev1ndu/certhealthz/pkg/output"
	"github.com/thev1ndu/certhealthz/pkg/probe"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// warnScan logs a scan-step failure, unless it's just the caller's context
// being canceled or timing out — that means the client (a browser
// navigating away mid-scan, or a shutting-down server) walked away, not
// that anything actually broke, so it's not worth alarming an operator
// over. /api/certs' collect closure runs several of these sequentially per
// cluster (certs, issuers, clusterissuers, secrets, ingress); a request
// abandoned partway through would otherwise print a "warning:" for every
// step still in flight when the cancellation lands.
func warnScan(err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: %v\n", err)
}

// ClusterClients bundles the clients CollectRowsFromClients needs for one
// cluster, decoupling the scan loop from how those clients were built —
// real kubeconfig-backed clients in production, fakes in e2e tests.
type ClusterClients struct {
	Label   string
	Dyn     dynamic.Interface
	Typed   kubernetes.Interface
	Gateway gatewayclientset.Interface
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

		var gwClient gatewayclientset.Interface
		if kubeconfig != nil {
			gwClient, err = certmanager.NewGatewayClientFromBytes(kubeconfig)
		} else {
			gwClient, err = certmanager.NewGatewayClient(path)
		}
		if err != nil {
			return ClusterClients{}, fmt.Errorf("building gateway client for %s: %w", label, err)
		}
		cc.Gateway = gwClient
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
func collectRows(ctx context.Context, kubeconfigs []string, warnDays int, includeSecrets bool, requireLabels []string, mtlsSecretSelectors []string) ([]output.Row, error) {
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

	return CollectRowsFromClients(ctx, clients, warnDays, includeSecrets, requireLabels, mtlsSecretSelectors)
}

// mtlsRows scans Secrets matching selectors (label-selector syntax, e.g.
// "app=my-client") via the same parsing path as a normal
// kubernetes.io/tls Secret scan, but tags each result Source "mtls-client"
// with a "client certificate" note in Detail — these are mTLS client
// certificates tracked for their own expiry, not server certs backing a
// route.
func mtlsRows(ctx context.Context, clusterLabel string, typedClient kubernetes.Interface, selectors []string, warnDays int) []output.Row {
	var rows []output.Row
	for _, selector := range selectors {
		secrets, err := certmanager.ScanSecretsWithSelector(ctx, clusterLabel, typedClient, selector)
		if err != nil {
			warnScan(err)
			continue
		}
		for _, s := range secrets {
			row := output.Row{
				Source:    "mtls-client",
				Cluster:   s.Cluster,
				Namespace: s.Namespace,
				Name:      s.Name,
				NotAfter:  s.NotAfter,
			}
			row = classifySecret(row, s, warnDays)
			note := "client certificate"
			if row.Detail != "" {
				row.Detail += " | " + note
			} else {
				row.Detail = note
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// CollectRowsFromClients runs the actual cert-manager/Secret scan and
// classification logic against already-built clients, one per cluster. It's
// the seam that lets e2e tests exercise the real scan pipeline against fake
// clientsets instead of a real cluster.
func CollectRowsFromClients(ctx context.Context, targets []ClusterClients, warnDays int, includeSecrets bool, requireLabels []string, mtlsSecretSelectors []string) ([]output.Row, error) {
	var rows []output.Row
	var allSecrets []certmanager.SecretCert

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
				warnScan(err)
			}
			secretsByKey = make(map[string]certmanager.SecretCert, len(secrets))
			for _, s := range secrets {
				secretsByKey[s.Namespace+"/"+s.Name] = s
			}
			allSecrets = append(allSecrets, secrets...)
		}

		reqFailures, err := certmanager.ScanFailedRequests(ctx, clusterLabel, target.Dyn)
		if err != nil {
			warnScan(err)
		}

		certs, err := certmanager.Scan(ctx, clusterLabel, target.Dyn)
		if err != nil {
			warnScan(err)
		}
		for _, c := range certs {
			status := "ok"
			detail := ""
			switch {
			case !c.Ready:
				status = "error"
				detail = "not ready: " + c.FailReason
				if reason, ok := reqFailures[c.Namespace+"/"+c.Name]; ok {
					detail += fmt.Sprintf(" (CertificateRequest: %s)", reason)
				}
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
			if len(requireLabels) > 0 {
				if missing := missingLabels(c.Labels, requireLabels); len(missing) > 0 {
					note := fmt.Sprintf("missing required label(s): %s", strings.Join(missing, ", "))
					if row.Detail != "" {
						row.Detail += " | " + note
					} else {
						row.Detail = note
					}
				}
			}
			rows = append(rows, row)
		}

		issuers, err := certmanager.ScanIssuers(ctx, clusterLabel, target.Dyn)
		if err != nil {
			warnScan(err)
		}
		rows = append(rows, issuerRows(issuers)...)

		clusterIssuers, err := certmanager.ScanClusterIssuers(ctx, clusterLabel, target.Dyn)
		if err != nil {
			warnScan(err)
		}
		rows = append(rows, issuerRows(clusterIssuers)...)

		if includeSecrets {
			// Scanned before the secret rows below (rather than after, as
			// this used to run) so referencedByIngress/referencedByGateway
			// are available in time to flag orphaned Secrets — one whose
			// name isn't referenced by any Certificate's spec.secretName,
			// any Ingress route, or any Gateway API route is dead weight
			// (or a forgotten manual cert).
			routes, err := ingress.Scan(ctx, clusterLabel, target.Typed)
			if err != nil {
				warnScan(err)
			}
			referencedByIngress := make(map[string]bool, len(routes))
			for _, route := range routes {
				referencedByIngress[route.Namespace+"/"+route.SecretName] = true
			}

			gwRoutes, err := gateway.Scan(ctx, clusterLabel, target.Gateway, target.Typed)
			if err != nil {
				warnScan(err)
			}
			referencedByGateway := make(map[string]bool, len(gwRoutes))
			for _, route := range gwRoutes {
				if route.SecretName != "" {
					referencedByGateway[route.SecretNamespace+"/"+route.SecretName] = true
				}
			}

			gwStatusRows, err := gateway.ScanStatus(ctx, clusterLabel, target.Gateway, target.Typed)
			if err != nil {
				warnScan(err)
			}
			rows = append(rows, gwStatusRows...)

			istioRoutes, err := mesh.ScanIstio(ctx, clusterLabel, target.Dyn, target.Typed)
			if err != nil {
				warnScan(err)
			}
			traefikRoutes, err := mesh.ScanTraefik(ctx, clusterLabel, target.Dyn, target.Typed)
			if err != nil {
				warnScan(err)
			}
			for _, r := range istioRoutes {
				if r.SecretName != "" {
					referencedByGateway[r.SecretNamespace+"/"+r.SecretName] = true
				}
			}
			for _, r := range traefikRoutes {
				if r.SecretName != "" {
					referencedByGateway[r.SecretNamespace+"/"+r.SecretName] = true
				}
			}

			referencedByCert := make(map[string]bool, len(certs))
			for _, c := range certs {
				if c.SecretName != "" {
					referencedByCert[c.Namespace+"/"+c.SecretName] = true
				}
			}

			for _, s := range secrets {
				row := output.Row{
					Source:    "secret",
					Cluster:   s.Cluster,
					Namespace: s.Namespace,
					Name:      s.Name,
					NotAfter:  s.NotAfter,
				}
				row = classifySecret(row, s, warnDays)
				key := s.Namespace + "/" + s.Name
				if !referencedByCert[key] && !referencedByIngress[key] && !referencedByGateway[key] {
					note := "orphaned: no Certificate, Ingress, or Gateway route references this Secret"
					if row.Detail != "" {
						row.Detail += " | " + note
					} else {
						row.Detail = note
					}
				}
				rows = append(rows, row)
			}

			rows = append(rows, ingressRoutesRows(routes, secretsByKey, warnDays)...)
			rows = append(rows, gatewayRoutesRows(gwRoutes, secretsByKey, warnDays)...)
			rows = append(rows, meshRoutesRows(istioRoutes, traefikRoutes, secretsByKey, warnDays)...)

			if len(mtlsSecretSelectors) > 0 {
				rows = append(rows, mtlsRows(ctx, clusterLabel, target.Typed, mtlsSecretSelectors, warnDays)...)
			}
		}
	}

	if len(allSecrets) > 0 {
		findings := crossSecretFindings(allSecrets)
		for i := range rows {
			if rows[i].Source != "secret" {
				continue
			}
			if extra, ok := findings[rows[i].Namespace+"/"+rows[i].Name]; ok {
				if rows[i].Detail != "" {
					rows[i].Detail += " | " + extra
				} else {
					rows[i].Detail = extra
				}
			}
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
		if row.Status != "expired" && row.Status != "error" && r.WeakTLS {
			row.Status = "weak-crypto"
			row.Detail = r.TLSIssue
		}
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
	case !sc.ChainExpiry.IsZero() && time.Until(sc.ChainExpiry) <= time.Duration(warnDays)*24*time.Hour:
		row.Status = "broken-chain"
		row.Detail = fmt.Sprintf("chain certificate %q expires %s", sc.ChainExpirySubject, sc.ChainExpiry.Format(time.RFC3339))
	case !sc.ChainOK:
		row.Status = "broken-chain"
		row.Detail = sc.ChainIssue
	case sc.WeakCrypto:
		row.Status = "weak-crypto"
		row.Detail = sc.CryptoIssue
	}
	return row
}

// crossSecretFindings computes cross-Secret integrity findings — private
// key reuse and DNS name conflicts — that only make sense once every
// cluster's Secrets are known at once, unlike the per-secret checks in
// classifySecret. Returns extra Detail text keyed by "namespace/name",
// appended (not overriding Status) onto the matching "secret" row.
func crossSecretFindings(secrets []certmanager.SecretCert) map[string]string {
	byKeyHash := make(map[string][]string) // hash -> "namespace/name"
	byDNSName := make(map[string][]string) // dns name -> "namespace/name"
	for _, s := range secrets {
		key := s.Namespace + "/" + s.Name
		if s.PublicKeyHash != "" {
			byKeyHash[s.PublicKeyHash] = append(byKeyHash[s.PublicKeyHash], key)
		}
		for _, name := range s.DNSNames {
			byDNSName[name] = append(byDNSName[name], key)
		}
	}

	findings := make(map[string]string)
	appendFinding := func(key, text string) {
		if findings[key] != "" {
			findings[key] += " | " + text
		} else {
			findings[key] = text
		}
	}

	for _, keys := range byKeyHash {
		if len(keys) < 2 {
			continue
		}
		for _, key := range keys {
			others := otherKeys(keys, key)
			appendFinding(key, fmt.Sprintf("shares a private key with: %s", strings.Join(others, ", ")))
		}
	}
	for dnsName, keys := range byDNSName {
		unique := uniqueStrings(keys)
		if len(unique) < 2 {
			continue
		}
		for _, key := range unique {
			others := otherKeys(unique, key)
			appendFinding(key, fmt.Sprintf("hostname %s also claimed by: %s", dnsName, strings.Join(others, ", ")))
		}
	}
	return findings
}

// missingLabels returns which of the required label keys aren't present on
// labels, in the order given (values aren't checked — presence is the
// whole policy). Returns nil if every required key is present.
func missingLabels(labels map[string]string, required []string) []string {
	var missing []string
	for _, key := range required {
		if _, ok := labels[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}

func otherKeys(keys []string, exclude string) []string {
	out := make([]string, 0, len(keys)-1)
	for _, k := range keys {
		if k != exclude {
			out = append(out, k)
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
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

// gatewayRoutesRows cross-checks each Gateway API route's resolved TLS
// binding against its backing Secret, the same way ingressRoutesRows does
// for classic Ingress: a route whose Gateway wasn't Accepted, whose
// cross-namespace Secret reference has no permitting ReferenceGrant, or
// whose Secret is simply missing is flagged as an error; otherwise the row
// is classified by that Secret's real expiry. A route with no resolved
// Secret at all (e.g. attached to a plain-HTTP listener) is skipped — like
// an Ingress with no TLS block, there's nothing to cross-check.
func gatewayRoutesRows(routes []gateway.Route, secretsByKey map[string]certmanager.SecretCert, warnDays int) []output.Row {
	var rows []output.Row
	for _, route := range routes {
		name := fmt.Sprintf("%s/%s -> Gateway/%s", route.RouteKind, route.RouteName, route.GatewayName)

		switch {
		case route.CrossNamespaceBlocked:
			rows = append(rows, output.Row{
				Source:    "gateway",
				Cluster:   route.Cluster,
				Namespace: route.Namespace,
				Name:      name,
				Status:    "error",
				Detail:    fmt.Sprintf("Gateway %s/%s's TLS certificateRef crosses namespaces with no permitting ReferenceGrant", route.GatewayNamespace, route.GatewayName),
			})
			continue
		case !route.Accepted:
			rows = append(rows, output.Row{
				Source:    "gateway",
				Cluster:   route.Cluster,
				Namespace: route.Namespace,
				Name:      name,
				Status:    "error",
				Detail:    fmt.Sprintf("Gateway %s/%s has not accepted this route (Accepted condition not True)", route.GatewayNamespace, route.GatewayName),
			})
			continue
		case route.SecretName == "":
			// No TLS listener resolved for this route — nothing to
			// cross-check, mirroring an Ingress with no TLS block.
			continue
		}

		key := route.SecretNamespace + "/" + route.SecretName
		secret, ok := secretsByKey[key]

		hosts := route.Hosts
		if len(hosts) == 0 {
			hosts = []string{""}
		}
		for _, host := range hosts {
			rowName := name
			if host != "" {
				rowName = fmt.Sprintf("%s (%s)", name, host)
			}
			row := output.Row{
				Source:    "gateway",
				Cluster:   route.Cluster,
				Namespace: route.Namespace,
				Name:      rowName,
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

// meshRoutesRows cross-checks every Istio Gateway and Traefik IngressRoute
// TLS binding against its backing Secret, the same way ingressRoutesRows
// and gatewayRoutesRows do — a missing Secret or a host not covered by that
// Secret's SANs is an error; otherwise the row is classified by the
// Secret's real expiry.
func meshRoutesRows(istioRoutes []mesh.IstioRoute, traefikRoutes []mesh.TraefikRoute, secretsByKey map[string]certmanager.SecretCert, warnDays int) []output.Row {
	var rows []output.Row

	for _, route := range istioRoutes {
		rows = append(rows, meshRouteRows("istio", route.Cluster, route.Namespace, "Gateway/"+route.GatewayName, route.SecretNamespace, route.SecretName, route.Hosts, secretsByKey, warnDays)...)
	}
	for _, route := range traefikRoutes {
		rows = append(rows, meshRouteRows("traefik", route.Cluster, route.Namespace, "IngressRoute/"+route.IngressRoute, route.SecretNamespace, route.SecretName, route.Hosts, secretsByKey, warnDays)...)
	}
	return rows
}

// meshRouteRows is the shared per-route-per-host row builder behind
// meshRoutesRows, parameterized on the mesh source ("istio" | "traefik")
// since Istio Gateways and Traefik IngressRoutes otherwise resolve to the
// exact same shape: a namespace, a display name, a backing Secret, and the
// hosts it covers.
func meshRouteRows(source, cluster, namespace, displayName, secretNamespace, secretName string, hosts []string, secretsByKey map[string]certmanager.SecretCert, warnDays int) []output.Row {
	if secretName == "" {
		return nil
	}
	key := secretNamespace + "/" + secretName
	secret, ok := secretsByKey[key]

	if len(hosts) == 0 {
		hosts = []string{""}
	}

	var rows []output.Row
	for _, host := range hosts {
		name := displayName
		if host != "" {
			name = fmt.Sprintf("%s (%s)", displayName, host)
		}
		row := output.Row{
			Source:    source,
			Cluster:   cluster,
			Namespace: namespace,
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
	return rows
}
