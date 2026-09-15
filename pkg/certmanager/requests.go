package certmanager

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var certificateRequestGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "certificaterequests",
}

// certificateNameLabel is the label cert-manager stamps on every
// CertificateRequest it creates on behalf of a Certificate, linking the
// request back to its owner.
const certificateNameLabel = "cert-manager.io/certificate-name"

// ScanFailedRequests lists every CertificateRequest and returns the failure
// reason of the most recently created not-Ready request per Certificate
// (keyed by "namespace/certificate-name"), so a not-ready Certificate's row
// can be enriched with *why* — the actual ACME/webhook error — rather than
// just that it isn't ready. Requests missing the owning-Certificate label,
// or with no failure reason, are skipped rather than guessed at.
func ScanFailedRequests(ctx context.Context, cluster string, client dynamic.Interface) (map[string]string, error) {
	list, err := client.Resource(certificateRequestGVR).Namespace("").List(ctx, metaListOpts())
	if err != nil {
		return nil, fmt.Errorf("listing cert-manager certificate requests on %s: %w", cluster, err)
	}

	type failure struct {
		reason    string
		createdAt string
	}
	latest := make(map[string]failure)

	for _, item := range list.Items {
		certName := item.GetLabels()[certificateNameLabel]
		if certName == "" {
			continue
		}
		ready, reason := readyCondition(item)
		if ready || reason == "" {
			continue
		}
		key := item.GetNamespace() + "/" + certName
		createdAt := item.GetCreationTimestamp().String()
		if existing, ok := latest[key]; !ok || createdAt > existing.createdAt {
			latest[key] = failure{reason: reason, createdAt: createdAt}
		}
	}

	out := make(map[string]string, len(latest))
	for key, f := range latest {
		out[key] = f.reason
	}
	return out, nil
}
