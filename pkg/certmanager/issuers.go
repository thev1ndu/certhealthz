package certmanager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var issuerGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "issuers",
}

var clusterIssuerGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "clusterissuers",
}

// IssuerHealth is a flattened view of a cert-manager Issuer or
// ClusterIssuer's Ready condition — the authority every Certificate on the
// cluster ultimately depends on to actually issue/renew, but which was
// never scanned on its own before: a misconfigured ACME account or
// exhausted CA quota shows up here long before every Certificate it backs
// starts failing too.
type IssuerHealth struct {
	Cluster    string
	Namespace  string // empty for a cluster-scoped ClusterIssuer
	Name       string
	Kind       string // "Issuer" | "ClusterIssuer"
	Ready      bool
	FailReason string
}

// ScanIssuers lists every namespaced Issuer visible via the given dynamic
// client and flattens each one's Ready condition.
func ScanIssuers(ctx context.Context, cluster string, client dynamic.Interface) ([]IssuerHealth, error) {
	list, err := client.Resource(issuerGVR).Namespace("").List(ctx, metaListOpts())
	if err != nil {
		return nil, fmt.Errorf("listing cert-manager issuers on %s: %w", cluster, err)
	}
	out := make([]IssuerHealth, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, flattenIssuer(cluster, "Issuer", item))
	}
	return out, nil
}

// ScanClusterIssuers lists every cluster-scoped ClusterIssuer visible via
// the given dynamic client and flattens each one's Ready condition.
func ScanClusterIssuers(ctx context.Context, cluster string, client dynamic.Interface) ([]IssuerHealth, error) {
	list, err := client.Resource(clusterIssuerGVR).List(ctx, metaListOpts())
	if err != nil {
		return nil, fmt.Errorf("listing cert-manager cluster issuers on %s: %w", cluster, err)
	}
	out := make([]IssuerHealth, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, flattenIssuer(cluster, "ClusterIssuer", item))
	}
	return out, nil
}

func flattenIssuer(cluster, kind string, u unstructured.Unstructured) IssuerHealth {
	h := IssuerHealth{
		Cluster:   cluster,
		Namespace: u.GetNamespace(),
		Name:      u.GetName(),
		Kind:      kind,
	}
	h.Ready, h.FailReason = readyCondition(u)
	return h
}
