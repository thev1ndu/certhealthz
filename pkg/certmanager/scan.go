package certmanager

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var certGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "certificates",
}

// Certificate is a flattened view of a cert-manager Certificate object,
// carrying only the fields CertCTL needs to judge expiry/renewal health.
type Certificate struct {
	Cluster     string
	Namespace   string
	Name        string
	SecretName  string
	Ready       bool
	NotAfter    time.Time
	RenewalTime time.Time
	FailReason  string
}

// Scan lists all cert-manager Certificate resources visible via the given
// dynamic client and flattens their status into Certificate structs.
func Scan(ctx context.Context, cluster string, client dynamic.Interface) ([]Certificate, error) {
	list, err := client.Resource(certGVR).Namespace("").List(ctx, metaListOpts())
	if err != nil {
		return nil, fmt.Errorf("listing cert-manager certificates on %s: %w", cluster, err)
	}

	certs := make([]Certificate, 0, len(list.Items))
	for _, item := range list.Items {
		certs = append(certs, flatten(cluster, item))
	}
	return certs, nil
}

func flatten(cluster string, u unstructured.Unstructured) Certificate {
	c := Certificate{
		Cluster:   cluster,
		Namespace: u.GetNamespace(),
		Name:      u.GetName(),
	}

	secretName, _, _ := unstructured.NestedString(u.Object, "spec", "secretName")
	c.SecretName = secretName

	notAfterStr, _, _ := unstructured.NestedString(u.Object, "status", "notAfter")
	if t, err := time.Parse(time.RFC3339, notAfterStr); err == nil {
		c.NotAfter = t
	}

	renewalStr, _, _ := unstructured.NestedString(u.Object, "status", "renewalTime")
	if t, err := time.Parse(time.RFC3339, renewalStr); err == nil {
		c.RenewalTime = t
	}

	conditions, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, raw := range conditions {
		cond, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Ready" {
			c.Ready = cond["status"] == "True"
			if !c.Ready {
				if reason, ok := cond["reason"].(string); ok {
					c.FailReason = reason
				}
			}
		}
	}

	return c
}
