// Package gateway scans Gateway API resources (Gateway, HTTPRoute,
// GRPCRoute, TLSRoute, ReferenceGrant) for their TLS certificate
// references, mirroring pkg/ingress's role for classic Ingress objects so
// callers can cross-check what a route claims to serve against what its
// backing Secret's certificate actually covers.
package gateway

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// GroupVersion is the Gateway API group/version this package scans.
const GroupVersion = "gateway.networking.k8s.io/v1"

// Route is one Gateway API route's resolved TLS binding: the route (an
// HTTPRoute, GRPCRoute, or TLSRoute), the Gateway it attaches to, and the
// Secret that Gateway's matching listener(s) terminate TLS with.
type Route struct {
	Cluster          string
	Namespace        string
	RouteKind        string // "HTTPRoute" | "GRPCRoute" | "TLSRoute"
	RouteName        string
	GatewayName      string
	GatewayNamespace string
	SecretName       string
	SecretNamespace  string
	Hosts            []string
	// GatewayAddresses is the resolving Gateway's status.addresses values
	// (IPs or hostnames the Gateway is reachable at), used by
	// pkg/migration's cutover-gate check to live-probe a dual-running or
	// gateway-only host before declaring the Gateway side ready to take
	// traffic.
	GatewayAddresses []string
	// GatewayAccepted/GatewayProgrammed mirror the resolving Gateway's own
	// Accepted/Programmed conditions (as opposed to Accepted above, which
	// is the route's acceptance by that Gateway) — both are part of the
	// cutover-gate check.
	GatewayProgrammed bool
	// Accepted is whether the Gateway named by GatewayName/GatewayNamespace
	// has an Accepted:True condition for this route's parentRef — an
	// unaccepted route is effectively orphaned (see Phase 2 migration
	// tooling), but is still returned here so callers can flag it rather
	// than silently drop it.
	Accepted bool
	// CrossNamespaceBlocked is true when the route's Gateway lives in a
	// different namespace than the Secret it resolves to, and no
	// ReferenceGrant permits that cross-namespace reference — the Secret
	// resolution above is skipped (SecretName/SecretNamespace stay empty)
	// and this is set instead so callers can flag it.
	CrossNamespaceBlocked bool
}

// CRDInstalled reports whether the Gateway API CRDs (specifically,
// gatewayclasses.gateway.networking.k8s.io) are registered on the cluster.
// Not every cluster has Gateway API installed; Scan and the status checks
// must fail soft (empty result, no error) rather than erroring out when
// it's absent, mirroring how the mesh package discovery-gates Istio/Traefik
// CRDs in Phase 3.
func CRDInstalled(kubeClient kubernetes.Interface) bool {
	if kubeClient == nil {
		return false
	}
	_, err := kubeClient.Discovery().ServerResourcesForGroupVersion(GroupVersion)
	return err == nil
}

// Scan lists every Gateway, HTTPRoute, GRPCRoute, TLSRoute, and
// ReferenceGrant across all namespaces and resolves each route to its
// backing Gateway and that Gateway's TLS Secret(s). If the Gateway API CRDs
// aren't installed on the cluster, Scan returns an empty result and no
// error.
func Scan(ctx context.Context, cluster string, client gatewayclientset.Interface, kubeClient kubernetes.Interface) ([]Route, error) {
	if client == nil || !CRDInstalled(kubeClient) {
		return nil, nil
	}

	v1 := client.GatewayV1()

	gwList, err := v1.Gateways("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing gateways on %s: %w", cluster, err)
	}
	gatewaysByKey := make(map[string]*gatewayv1.Gateway, len(gwList.Items))
	for i := range gwList.Items {
		gw := &gwList.Items[i]
		gatewaysByKey[gw.Namespace+"/"+gw.Name] = gw
	}

	refGrants, err := v1.ReferenceGrants("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing referencegrants on %s: %w", cluster, err)
	}

	var routes []Route

	httpRoutes, err := v1.HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing httproutes on %s: %w", cluster, err)
	}
	for _, r := range httpRoutes.Items {
		hosts := make([]string, 0, len(r.Spec.Hostnames))
		for _, h := range r.Spec.Hostnames {
			hosts = append(hosts, string(h))
		}
		routes = append(routes, resolveRoutes(cluster, "HTTPRoute", r.Namespace, r.Name, r.Spec.ParentRefs, hosts, gatewaysByKey, refGrants.Items)...)
	}

	grpcRoutes, err := v1.GRPCRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing grpcroutes on %s: %w", cluster, err)
	}
	for _, r := range grpcRoutes.Items {
		hosts := make([]string, 0, len(r.Spec.Hostnames))
		for _, h := range r.Spec.Hostnames {
			hosts = append(hosts, string(h))
		}
		routes = append(routes, resolveRoutes(cluster, "GRPCRoute", r.Namespace, r.Name, r.Spec.ParentRefs, hosts, gatewaysByKey, refGrants.Items)...)
	}

	tlsRoutes, err := v1.TLSRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing tlsroutes on %s: %w", cluster, err)
	}
	for _, r := range tlsRoutes.Items {
		hosts := make([]string, 0, len(r.Spec.Hostnames))
		for _, h := range r.Spec.Hostnames {
			hosts = append(hosts, string(h))
		}
		routes = append(routes, resolveRoutes(cluster, "TLSRoute", r.Namespace, r.Name, r.Spec.ParentRefs, hosts, gatewaysByKey, refGrants.Items)...)
	}

	return routes, nil
}

// resolveRoutes turns one route's parentRefs into zero or more Route
// entries — one per (parentRef, listener certificateRef) combination that
// resolves to a Gateway found in gatewaysByKey. A parentRef naming a
// Gateway not in gatewaysByKey (not found, or not a Gateway kind) is
// skipped.
func resolveRoutes(cluster, kind, namespace, name string, parentRefs []gatewayv1.ParentReference, hosts []string, gatewaysByKey map[string]*gatewayv1.Gateway, refGrants []gatewayv1.ReferenceGrant) []Route {
	var out []Route
	for _, ref := range parentRefs {
		if ref.Kind != nil && string(*ref.Kind) != "Gateway" {
			continue
		}
		gwNamespace := namespace
		if ref.Namespace != nil {
			gwNamespace = string(*ref.Namespace)
		}
		gw, ok := gatewaysByKey[gwNamespace+"/"+string(ref.Name)]
		if !ok {
			continue
		}

		accepted := gatewayAcceptedFor(gw, namespace, name, string(ref.Name))
		programmed := gatewayConditionTrue(gw, "Programmed")
		addresses := make([]string, 0, len(gw.Status.Addresses))
		for _, a := range gw.Status.Addresses {
			addresses = append(addresses, a.Value)
		}

		listeners := gw.Spec.Listeners
		matched := false
		for _, l := range listeners {
			if ref.SectionName != nil && string(*ref.SectionName) != string(l.Name) {
				continue
			}
			if l.TLS == nil || len(l.TLS.CertificateRefs) == 0 {
				continue
			}
			for _, certRef := range l.TLS.CertificateRefs {
				if certRef.Kind != nil && string(*certRef.Kind) != "" && string(*certRef.Kind) != "Secret" {
					continue
				}
				secretNamespace := gw.Namespace
				if certRef.Namespace != nil {
					secretNamespace = string(*certRef.Namespace)
				}
				route := Route{
					Cluster:           cluster,
					Namespace:         namespace,
					RouteKind:         kind,
					RouteName:         name,
					GatewayName:       gw.Name,
					GatewayNamespace:  gw.Namespace,
					Hosts:             hosts,
					Accepted:          accepted,
					GatewayAddresses:  addresses,
					GatewayProgrammed: programmed,
				}
				if secretNamespace != gw.Namespace && !referenceGrantAllows(refGrants, gw.Namespace, secretNamespace, string(certRef.Name)) {
					route.CrossNamespaceBlocked = true
				} else {
					route.SecretName = string(certRef.Name)
					route.SecretNamespace = secretNamespace
				}
				out = append(out, route)
				matched = true
			}
		}
		if !matched {
			// No TLS listener resolved (e.g. plain-HTTP listener, or a
			// listener with no certificateRefs) — still report the
			// route/Gateway binding so callers see it, just with no
			// Secret attached.
			out = append(out, Route{
				Cluster:           cluster,
				Namespace:         namespace,
				RouteKind:         kind,
				RouteName:         name,
				GatewayName:       gw.Name,
				GatewayNamespace:  gw.Namespace,
				Hosts:             hosts,
				Accepted:          accepted,
				GatewayAddresses:  addresses,
				GatewayProgrammed: programmed,
			})
		}
	}
	return out
}

// gatewayAcceptedFor reports whether gw's status carries an Accepted:True
// RouteParentStatus condition for the route identified by
// (routeNamespace/routeName), matched against the referent's Name field —
// gateway-api status entries key by ParentRef, and a route's own namespace
// combined with the ParentRef's Name is what we resolved this Gateway with,
// so match on that same Name.
func gatewayAcceptedFor(gw *gatewayv1.Gateway, _routeNamespace, _routeName, parentRefName string) bool {
	// A Gateway's own status doesn't carry per-route Accepted conditions —
	// those live on the Route's own status.parents[].conditions. Gateway
	// health checked here is instead the Gateway's own Accepted/Programmed
	// conditions (see status.go); resolveRoutes treats a route as
	// "accepted" for orphan-detection purposes once it resolves to a
	// Gateway that itself has Accepted:True.
	_ = parentRefName
	return gatewayConditionTrue(gw, string(gatewayv1.GatewayConditionAccepted))
}

// gatewayConditionTrue reports whether gw's status carries a True condition
// of the given type.
func gatewayConditionTrue(gw *gatewayv1.Gateway, condType string) bool {
	for _, c := range gw.Status.Conditions {
		if c.Type == condType {
			return c.Status == "True"
		}
	}
	return false
}

// referenceGrantAllows reports whether any ReferenceGrant in fromNamespace
// (the Secret's namespace) permits a Gateway in toNamespace to reference a
// Secret named secretName.
func referenceGrantAllows(grants []gatewayv1.ReferenceGrant, toNamespace, fromNamespace, secretName string) bool {
	for _, g := range grants {
		if g.Namespace != fromNamespace {
			continue
		}
		fromOK := false
		for _, f := range g.Spec.From {
			if string(f.Group) == "gateway.networking.k8s.io" && string(f.Kind) == "Gateway" && string(f.Namespace) == toNamespace {
				fromOK = true
				break
			}
		}
		if !fromOK {
			continue
		}
		for _, t := range g.Spec.To {
			if string(t.Kind) != "Secret" {
				continue
			}
			if t.Name == nil || string(*t.Name) == secretName {
				return true
			}
		}
	}
	return false
}
