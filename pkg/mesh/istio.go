// Package mesh scans mesh/ingress-controller-specific CRDs (Istio
// Gateway/VirtualService, Traefik IngressRoute) for TLS Secret references,
// the same role pkg/ingress and pkg/gateway play for classic Ingress and
// Gateway API. Every scan here is discovery-gated: most clusters don't run
// Istio or Traefik, and a missing CRD must return an empty result, not an
// error.
package mesh

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// IstioGroupVersion is the Istio networking API group/version this package
// scans for Gateway/VirtualService TLS bindings.
const IstioGroupVersion = "networking.istio.io/v1beta1"

var istioGatewayGVR = schema.GroupVersionResource{
	Group:    "networking.istio.io",
	Version:  "v1beta1",
	Resource: "gateways",
}

var istioVirtualServiceGVR = schema.GroupVersionResource{
	Group:    "networking.istio.io",
	Version:  "v1beta1",
	Resource: "virtualservices",
}

// IstioRoute is one Istio Gateway server's resolved TLS binding: the hosts
// it terminates TLS for and the Secret (credentialName) it uses.
type IstioRoute struct {
	Cluster         string
	Namespace       string
	GatewayName     string
	SecretNamespace string
	SecretName      string
	Hosts           []string
}

// CRDInstalled reports whether the Istio networking CRDs are registered on
// the cluster.
func CRDInstalled(kubeClient kubernetes.Interface) bool {
	if kubeClient == nil {
		return false
	}
	_, err := kubeClient.Discovery().ServerResourcesForGroupVersion(IstioGroupVersion)
	return err == nil
}

// ScanIstio lists every Istio Gateway resource's servers[].tls.credentialName
// across all namespaces and resolves it to a Secret. VirtualServices don't
// carry TLS material themselves (they route traffic already terminated by a
// Gateway), so they aren't separately scanned here — this mirrors how
// pkg/gateway resolves an HTTPRoute's TLS binding through its parent
// Gateway, not the route itself. If the Istio CRDs aren't installed,
// returns an empty result and no error.
func ScanIstio(ctx context.Context, cluster string, client dynamic.Interface, kubeClient kubernetes.Interface) ([]IstioRoute, error) {
	if client == nil || !CRDInstalled(kubeClient) {
		return nil, nil
	}

	list, err := client.Resource(istioGatewayGVR).Namespace("").List(ctx, listOpts())
	if err != nil {
		return nil, fmt.Errorf("listing istio gateways on %s: %w", cluster, err)
	}

	var routes []IstioRoute
	for _, item := range list.Items {
		servers, _, _ := unstructured.NestedSlice(item.Object, "spec", "servers")
		for _, raw := range servers {
			server, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			credentialName, _, _ := unstructured.NestedString(server, "tls", "credentialName")
			if credentialName == "" {
				continue
			}
			var hosts []string
			if h, ok, _ := unstructured.NestedStringSlice(server, "hosts"); ok {
				hosts = h
			}
			secretNamespace, secretName := resolveIstioCredential(item.GetNamespace(), credentialName)
			routes = append(routes, IstioRoute{
				Cluster:         cluster,
				Namespace:       item.GetNamespace(),
				GatewayName:     item.GetName(),
				SecretNamespace: secretNamespace,
				SecretName:      secretName,
				Hosts:           hosts,
			})
		}
	}
	return routes, nil
}

// resolveIstioCredential resolves a Gateway server's tls.credentialName into
// a namespace/name Secret reference. Istio's default form is a bare name,
// resolved in the Gateway's own namespace (conventionally istio-system);
// the "kubernetes-gateway://<namespace>/<name>" form (Istio 1.15+) names a
// Secret in a specific namespace explicitly, e.g. for cross-namespace
// termination via istiod's SDS bridge.
func resolveIstioCredential(gatewayNamespace, credentialName string) (namespace, name string) {
	const prefix = "kubernetes-gateway://"
	if rest, ok := strings.CutPrefix(credentialName, prefix); ok {
		if ns, n, found := strings.Cut(rest, "/"); found {
			return ns, n
		}
		return gatewayNamespace, rest
	}
	return gatewayNamespace, credentialName
}
