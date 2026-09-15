package cmd

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/thev1ndu/certhealthz/pkg/certmanager"
	"github.com/thev1ndu/certhealthz/pkg/gateway"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/migration"
	"github.com/thev1ndu/certhealthz/pkg/probe"
)

// maxTestRouteBodySize bounds the POST /api/routes/test request body —
// four short identity fields, never legitimately more than a few hundred
// bytes.
const maxTestRouteBodySize = 4 << 10 // 4 KiB

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

// apiTestRouteRequest is the JSON body of POST /api/routes/test — the same
// identity fields apiRoute already exposes to the frontend, so the
// dashboard can echo a route row's own fields straight back rather than
// needing a new ID-encoding scheme. This is deliberately narrow: the
// handler only ever dials an address it re-derives from a route the
// scanner already discovered, never an address or URL supplied directly by
// the client — this must not become an open proxy.
type apiTestRouteRequest struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

// apiTestRouteResponse is the JSON shape of POST /api/routes/test's result.
// Via is never ambiguous to the caller: "direct" means dialAddr was a
// published LB/Gateway address; "portforward" means the connection tunneled
// through a port-forward to the controller's Service (see pkg/portforward);
// "unreachable" means neither was available and no network test ran at all
// (Steps then holds a single synthetic step explaining why).
type apiTestRouteResponse struct {
	Via   string                `json:"via"`
	OK    bool                  `json:"ok"`
	Steps []probe.RouteTestStep `json:"steps"`
}

// routeKindFromJSON reverses routeKindJSON, so handleTestRoute can match a
// request's "kind" field back against gateway.Route.RouteKind.
func routeKindFromJSON(kind string) string {
	switch kind {
	case "httproute":
		return "HTTPRoute"
	case "grpcroute":
		return "GRPCRoute"
	case "tlsroute":
		return "TLSRoute"
	default:
		return kind
	}
}

// findIngressRoute locates the ingress.Route matching a POST
// /api/routes/test request's identity fields.
func findIngressRoute(routes []ingress.Route, req apiTestRouteRequest) (ingress.Route, bool) {
	for _, route := range routes {
		if route.Cluster == req.Cluster && route.Namespace == req.Namespace && route.Ingress == req.Name {
			return route, true
		}
	}
	return ingress.Route{}, false
}

// findGatewayRoute locates the gateway.Route matching a POST
// /api/routes/test request's identity fields. A host may resolve to several
// Route entries (multiple hosts, or dual attachment to more than one
// Gateway); the first match is used, matching how the dashboard already
// displays one representative row per route identity.
func findGatewayRoute(routes []gateway.Route, req apiTestRouteRequest) (gateway.Route, bool) {
	wantKind := routeKindFromJSON(req.Kind)
	for _, route := range routes {
		if route.Cluster == req.Cluster && route.Namespace == req.Namespace && route.RouteKind == wantKind && route.RouteName == req.Name {
			return route, true
		}
	}
	return gateway.Route{}, false
}

// resolveExpectedCert fetches and parses the Secret a route claims to back
// it with, for TestRoute's cert_match step. Returns (nil, nil) — not an
// error — if the Secret can't be found or parsed, since a synthetic route
// test should still run its other steps and simply skip cert comparison
// rather than fail outright over a Secret lookup problem that drift
// detection already reports separately.
func resolveExpectedCert(ctx context.Context, cc ClusterClients, namespace, secretName string) *x509.Certificate {
	if cc.Typed == nil || secretName == "" {
		return nil
	}
	secret, err := cc.Typed.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	leaf, _, err := certmanager.ParseSecretChain(*secret)
	if err != nil {
		return nil
	}
	return leaf
}

// unreachableResult builds the JSON response for a route with no direct
// address and no successful port-forward fallback — a clear, explicit
// result rather than an HTTP error, since "this route can't be reached from
// here" is itself a meaningful test outcome.
func unreachableResult(reason string) apiTestRouteResponse {
	return apiTestRouteResponse{
		Via: "unreachable",
		OK:  false,
		Steps: []probe.RouteTestStep{
			{Name: "resolve_address", OK: false, Detail: reason},
		},
	}
}

// handleTestRoute serves POST /api/routes/test: an on-demand synthetic
// connectivity check (TCP/TLS/cert-match/HTTP) against one already-scanned
// Ingress or Gateway API route. It re-scans live (like handleRoutes),
// re-finds the matching route by identity, and resolves a dial address:
// preferring a published external LB/Gateway address, falling back to a
// port-forward through the route's controller Service (see
// attemptPortForward, cmd/portforward.go) for the small set of controllers
// that support it, and otherwise returning an explicit "unreachable"
// result.
func handleTestRoute(deps UIDeps, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body := io.LimitReader(r.Body, maxTestRouteBodySize)
	var req apiTestRouteRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Cluster == "" || req.Namespace == "" || req.Kind == "" || req.Name == "" {
		http.Error(w, "cluster, namespace, kind, and name are all required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	ingressRoutes, gatewayRoutes, _, err := collectAllRoutes(ctx, deps.Clusters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cc, err := deps.ClusterClientsFor(req.Cluster)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var (
		hosts           []string
		dialAddrs       []string
		secretNamespace string
		secretName      string
		controllerName  string
		svcNamespace    string
		svcName         string
	)

	switch req.Kind {
	case "ingress":
		route, ok := findIngressRoute(ingressRoutes, req)
		if !ok {
			http.Error(w, "route not found", http.StatusNotFound)
			return
		}
		hosts = route.Hosts
		dialAddrs = route.LoadBalancerAddresses
		secretNamespace = route.Namespace
		secretName = route.SecretName
		svcNamespace, svcName, controllerName, err = resolveIngressControllerService(ctx, cc, route)
	case "httproute", "grpcroute", "tlsroute":
		route, ok := findGatewayRoute(gatewayRoutes, req)
		if !ok {
			http.Error(w, "route not found", http.StatusNotFound)
			return
		}
		hosts = route.Hosts
		dialAddrs = route.GatewayAddresses
		secretNamespace = route.SecretNamespace
		secretName = route.SecretName
		svcNamespace, svcName, controllerName, err = resolveGatewayControllerService(ctx, cc, route)
	default:
		http.Error(w, "unknown route kind", http.StatusBadRequest)
		return
	}

	if len(hosts) == 0 {
		http.Error(w, "route has no hostname to test", http.StatusUnprocessableEntity)
		return
	}
	host := hosts[0]
	expectedCert := resolveExpectedCert(ctx, cc, secretNamespace, secretName)

	var resp apiTestRouteResponse
	switch {
	case len(dialAddrs) > 0:
		dialAddr := net.JoinHostPort(dialAddrs[0], "443")
		result := probe.TestRoute(ctx, dialAddr, host, host, expectedCert, uiProbeTimeout)
		resp = apiTestRouteResponse{Via: "direct", OK: result.OK, Steps: result.Steps}
	case err == nil && svcName != "":
		localAddr, stop, pfErr := attemptPortForward(ctx, cc, svcNamespace, svcName)
		if pfErr != nil {
			resp = unreachableResult(fmt.Sprintf("no direct address published, and port-forward to %s/%s failed: %v", svcNamespace, svcName, pfErr))
		} else {
			defer stop()
			result := probe.TestRoute(ctx, localAddr, host, host, expectedCert, uiProbeTimeout)
			resp = apiTestRouteResponse{Via: "portforward", OK: result.OK, Steps: result.Steps}
		}
	case err != nil:
		resp = unreachableResult(fmt.Sprintf("no direct address published, and no port-forward fallback available: %v", err))
	default:
		reason := "no direct address published, and no port-forward fallback available for this route"
		if controllerName != "" {
			reason = fmt.Sprintf("no direct address published, and port-forward testing is unsupported for controller %q", controllerName)
		}
		resp = unreachableResult(reason)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		fmt.Printf("encoding /api/routes/test response: %v\n", err)
	}
}
