package mesh

import (
	"context"
	"fmt"
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// TraefikGroupVersion is the Traefik CRD group/version this package scans
// for IngressRoute TLS bindings.
const TraefikGroupVersion = "traefik.io/v1alpha1"

var traefikIngressRouteGVR = schema.GroupVersionResource{
	Group:    "traefik.io",
	Version:  "v1alpha1",
	Resource: "ingressroutes",
}

// hostRuleRe extracts hostnames out of a Traefik IngressRoute match rule's
// Host(...)/HostSNI(...) matchers, e.g. `Host(\`example.com\`, \`www.example.com\`)`.
// Traefik's matcher language allows several backtick-quoted arguments per
// call and several calls combined with && / ||; this pulls every
// backtick-quoted string following any Host-like matcher, which covers the
// overwhelming majority of real-world rules without implementing a full
// matcher-language parser.
var hostRuleRe = regexp.MustCompile(`Host(?:SNI)?\(([^)]*)\)`)
var backtickArgRe = regexp.MustCompile("`([^`]*)`")

// TraefikRoute is one Traefik IngressRoute's resolved TLS binding.
type TraefikRoute struct {
	Cluster         string
	Namespace       string
	IngressRoute    string
	SecretNamespace string
	SecretName      string
	Hosts           []string
}

// TraefikCRDInstalled reports whether the Traefik CRDs are registered on
// the cluster.
func TraefikCRDInstalled(kubeClient kubernetes.Interface) bool {
	if kubeClient == nil {
		return false
	}
	_, err := kubeClient.Discovery().ServerResourcesForGroupVersion(TraefikGroupVersion)
	return err == nil
}

// ScanTraefik lists every Traefik IngressRoute's spec.tls.secretName across
// all namespaces and the hosts its routes' match rules reference. If the
// Traefik CRDs aren't installed, returns an empty result and no error.
func ScanTraefik(ctx context.Context, cluster string, client dynamic.Interface, kubeClient kubernetes.Interface) ([]TraefikRoute, error) {
	if client == nil || !TraefikCRDInstalled(kubeClient) {
		return nil, nil
	}

	list, err := client.Resource(traefikIngressRouteGVR).Namespace("").List(ctx, listOpts())
	if err != nil {
		return nil, fmt.Errorf("listing traefik ingressroutes on %s: %w", cluster, err)
	}

	var routes []TraefikRoute
	for _, item := range list.Items {
		secretName, _, _ := unstructured.NestedString(item.Object, "spec", "tls", "secretName")
		if secretName == "" {
			// No TLS block — nothing to cross-check, mirroring a plain
			// (non-TLS) Ingress or Gateway listener.
			continue
		}
		// Traefik's tls.secretName is always resolved in the IngressRoute's
		// own namespace; there is no cross-namespace form (unlike Gateway
		// API's certificateRefs).
		secretNamespace := item.GetNamespace()

		var hosts []string
		routesList, _, _ := unstructured.NestedSlice(item.Object, "spec", "routes")
		for _, raw := range routesList {
			r, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			match, _, _ := unstructured.NestedString(r, "match")
			hosts = append(hosts, extractHosts(match)...)
		}

		routes = append(routes, TraefikRoute{
			Cluster:         cluster,
			Namespace:       item.GetNamespace(),
			IngressRoute:    item.GetName(),
			SecretNamespace: secretNamespace,
			SecretName:      secretName,
			Hosts:           hosts,
		})
	}
	return routes, nil
}

// extractHosts pulls every backtick-quoted hostname out of a Traefik match
// rule's Host()/HostSNI() matchers.
func extractHosts(matchRule string) []string {
	var hosts []string
	for _, call := range hostRuleRe.FindAllStringSubmatch(matchRule, -1) {
		for _, arg := range backtickArgRe.FindAllStringSubmatch(call[1], -1) {
			hosts = append(hosts, arg[1])
		}
	}
	return hosts
}

func listOpts() metav1.ListOptions {
	return metav1.ListOptions{}
}
