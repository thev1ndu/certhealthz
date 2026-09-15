// Package ingress scans Ingress resources for their TLS blocks, so callers
// can cross-check what a route claims to serve against what its backing
// Secret's certificate actually covers.
package ingress

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Route is one Ingress TLS block: the Secret it references and the hosts
// it's terminating TLS for.
type Route struct {
	Cluster    string
	Namespace  string
	Ingress    string
	SecretName string
	Hosts      []string
}

// Scan lists every Ingress's TLS blocks across all namespaces. Ingress with
// no TLS block, or a TLS block with no secretName, is skipped — there's no
// Secret to cross-check.
func Scan(ctx context.Context, cluster string, client kubernetes.Interface) ([]Route, error) {
	list, err := client.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing ingresses on %s: %w", cluster, err)
	}

	var routes []Route
	for _, ing := range list.Items {
		for _, tls := range ing.Spec.TLS {
			if tls.SecretName == "" {
				continue
			}
			routes = append(routes, Route{
				Cluster:    cluster,
				Namespace:  ing.Namespace,
				Ingress:    ing.Name,
				SecretName: tls.SecretName,
				Hosts:      tls.Hosts,
			})
		}
	}
	return routes, nil
}

// RuleHost is one Ingress rule's hostname — used (independently of any TLS
// block) to detect an Ingress object that's still routing a host even
// though its TLS termination has already moved elsewhere, e.g. a stale
// Ingress left behind after a Gateway API cutover (see pkg/migration).
type RuleHost struct {
	Cluster   string
	Namespace string
	Ingress   string
	Host      string
}

// ScanRuleHosts lists every Ingress's spec.rules[].host across all
// namespaces, independent of whether that Ingress has a TLS block at all —
// unlike Scan, which only reports hosts inside a TLS block.
func ScanRuleHosts(ctx context.Context, cluster string, client kubernetes.Interface) ([]RuleHost, error) {
	list, err := client.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing ingresses on %s: %w", cluster, err)
	}

	var hosts []RuleHost
	for _, ing := range list.Items {
		for _, rule := range ing.Spec.Rules {
			if rule.Host == "" {
				continue
			}
			hosts = append(hosts, RuleHost{
				Cluster:   cluster,
				Namespace: ing.Namespace,
				Ingress:   ing.Name,
				Host:      rule.Host,
			})
		}
	}
	return hosts, nil
}

// HostCovered reports whether a certificate SAN covers host — an exact
// match, or a single-level wildcard ("*.example.com" covers "foo.example.com"
// but not "example.com" or "foo.bar.example.com", per RFC 6125).
func HostCovered(san, host string) bool {
	san = strings.ToLower(strings.TrimSuffix(san, "."))
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if san == host {
		return true
	}
	suffix, ok := strings.CutPrefix(san, "*.")
	if !ok {
		return false
	}
	label, rest, found := strings.Cut(host, ".")
	return found && label != "" && rest == suffix
}

// AnyHostCovered reports whether any SAN in sans covers host.
func AnyHostCovered(sans []string, host string) bool {
	for _, san := range sans {
		if HostCovered(san, host) {
			return true
		}
	}
	return false
}
