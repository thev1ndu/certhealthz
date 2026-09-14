package certmanager

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// TriggerReissue manually triggers a Certificate reissuance — the same
// mechanism `cmctl renew` uses: upsert an `Issuing: True` condition onto
// the Certificate's status, which cert-manager's own trigger controller
// then acts on. This does NOT touch spec, delete the backing Secret, or
// bypass cert-manager in any way — it only asks cert-manager to do what it
// would do on its own once the renewal window arrives.
//
// Get-then-UpdateStatus (not a JSON merge patch) is deliberate: a merge
// patch on a list field replaces the whole list, which would silently wipe
// out the existing Ready condition (and anything else cert-manager has
// already recorded) instead of just adding Issuing alongside it.
func TriggerReissue(ctx context.Context, dyn dynamic.Interface, namespace, name string) error {
	obj, err := dyn.Resource(certGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("fetching certificate %s/%s: %w", namespace, name, err)
	}

	conditions, _, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return fmt.Errorf("reading status.conditions on %s/%s: %w", namespace, name, err)
	}

	issuing := map[string]any{
		"type":               "Issuing",
		"status":             "True",
		"reason":             "ManuallyTriggered",
		"message":            "Manually triggered from the certhealthz dashboard",
		"lastTransitionTime": time.Now().UTC().Format(time.RFC3339),
	}

	replaced := false
	for i, c := range conditions {
		if cond, ok := c.(map[string]any); ok && cond["type"] == "Issuing" {
			conditions[i] = issuing
			replaced = true
			break
		}
	}
	if !replaced {
		conditions = append(conditions, issuing)
	}

	if err := unstructured.SetNestedSlice(obj.Object, conditions, "status", "conditions"); err != nil {
		return fmt.Errorf("setting status.conditions on %s/%s: %w", namespace, name, err)
	}

	if _, err := dyn.Resource(certGVR).Namespace(namespace).UpdateStatus(ctx, obj, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating status on %s/%s: %w", namespace, name, err)
	}
	return nil
}
