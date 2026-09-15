package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/thev1ndu/certhealthz/pkg/gateway"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/migration"
)

// apiRoute is the JSON shape of one entry in GET /api/routes — a unified
// view of an Ingress or Gateway API route, regardless of which produced it.
type apiRoute struct {
	Kind             string   `json:"kind"` // "ingress" | "httproute" | "grpcroute" | "tlsroute"
	Cluster          string   `json:"cluster"`
	Namespace        string   `json:"namespace"`
	Name             string   `json:"name"`
	GatewayName      string   `json:"gatewayName,omitempty"`
	GatewayNamespace string   `json:"gatewayNamespace,omitempty"`
	SecretNamespace  string   `json:"secretNamespace"`
	SecretName       string   `json:"secretName"`
	Hosts            []string `json:"hosts"`
	Accepted         bool     `json:"accepted"`
}

// apiHostCoverage is the JSON shape of one entry in GET
// /api/routes/coverage.
type apiHostCoverage struct {
	Host           string   `json:"host"`
	State          string   `json:"state"`
	IngressSecret  string   `json:"ingressSecret,omitempty"`
	GatewaySecret  string   `json:"gatewaySecret,omitempty"`
	SecretDrift    bool     `json:"secretDrift"`
	CutoverReady   bool     `json:"cutoverReady"`
	CutoverDetail  string   `json:"cutoverDetail,omitempty"`
	StaleIngresses []string `json:"staleIngresses,omitempty"`
}

// collectAllRoutes scans Ingress and Gateway API routes across every
// configured cluster, following the same buildAllClusterClients +
// per-cluster-scan shape collectRows uses for certificates.
func collectAllRoutes(ctx context.Context, clusters *ClusterRegistry) (ingressRoutes []ingress.Route, gatewayRoutes []gateway.Route, ruleHosts []ingress.RuleHost, err error) {
	clients, err := buildAllClusterClients(clusters.All(), true)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, cc := range clients {
		ir, err := ingress.Scan(ctx, cc.Label, cc.Typed)
		if err != nil {
			warnScan(err)
		}
		ingressRoutes = append(ingressRoutes, ir...)

		gr, err := gateway.Scan(ctx, cc.Label, cc.Gateway, cc.Typed)
		if err != nil {
			warnScan(err)
		}
		gatewayRoutes = append(gatewayRoutes, gr...)

		rh, err := ingress.ScanRuleHosts(ctx, cc.Label, cc.Typed)
		if err != nil {
			warnScan(err)
		}
		ruleHosts = append(ruleHosts, rh...)
	}
	return ingressRoutes, gatewayRoutes, ruleHosts, nil
}

// toAPIRoutes flattens ingress/gateway routes into the unified apiRoute
// shape /api/routes returns.
func toAPIRoutes(ingressRoutes []ingress.Route, gatewayRoutes []gateway.Route) []apiRoute {
	out := make([]apiRoute, 0, len(ingressRoutes)+len(gatewayRoutes))
	for _, r := range ingressRoutes {
		out = append(out, apiRoute{
			Kind:            "ingress",
			Cluster:         r.Cluster,
			Namespace:       r.Namespace,
			Name:            r.Ingress,
			SecretNamespace: r.Namespace,
			SecretName:      r.SecretName,
			Hosts:           r.Hosts,
			Accepted:        true, // Ingress has no Accepted-style condition; presence == accepted
		})
	}
	for _, r := range gatewayRoutes {
		out = append(out, apiRoute{
			Kind:             routeKindJSON(r.RouteKind),
			Cluster:          r.Cluster,
			Namespace:        r.Namespace,
			Name:             r.RouteName,
			GatewayName:      r.GatewayName,
			GatewayNamespace: r.GatewayNamespace,
			SecretNamespace:  r.SecretNamespace,
			SecretName:       r.SecretName,
			Hosts:            r.Hosts,
			Accepted:         r.Accepted,
		})
	}
	return out
}

func routeKindJSON(routeKind string) string {
	switch routeKind {
	case "HTTPRoute":
		return "httproute"
	case "GRPCRoute":
		return "grpcroute"
	case "TLSRoute":
		return "tlsroute"
	default:
		return routeKind
	}
}

func toAPIHostCoverage(cov []migration.HostCoverage) []apiHostCoverage {
	out := make([]apiHostCoverage, 0, len(cov))
	for _, hc := range cov {
		out = append(out, apiHostCoverage{
			Host:           hc.Host,
			State:          string(hc.State),
			IngressSecret:  hc.IngressSecret,
			GatewaySecret:  hc.GatewaySecret,
			SecretDrift:    hc.SecretDrift,
			CutoverReady:   hc.CutoverReady,
			CutoverDetail:  hc.CutoverDetail,
			StaleIngresses: hc.StaleIngresses,
		})
	}
	return out
}

// handleRoutes serves GET /api/routes: the unified Ingress+Gateway API
// route list across every configured cluster.
func handleRoutes(clusters *ClusterRegistry, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ingressRoutes, gatewayRoutes, _, err := collectAllRoutes(r.Context(), clusters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toAPIRoutes(ingressRoutes, gatewayRoutes)); err != nil {
		fmt.Printf("encoding /api/routes response: %v\n", err)
	}
}

// handleRouteCoverage serves GET /api/routes/coverage: the Ingress ->
// Gateway API migration coverage report across every configured cluster.
func handleRouteCoverage(clusters *ClusterRegistry, probeTimeout time.Duration, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ingressRoutes, gatewayRoutes, ruleHosts, err := collectAllRoutes(r.Context(), clusters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cov := migration.CoverageWithStaleIngresses(ingressRoutes, gatewayRoutes, ruleHosts, probeTimeout)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(toAPIHostCoverage(cov)); err != nil {
		fmt.Printf("encoding /api/routes/coverage response: %v\n", err)
	}
}
