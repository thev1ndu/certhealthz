package gateway

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/thev1ndu/certhealthz/pkg/output"
)

// controllerLabels maps a GatewayClass's spec.controllerName to a
// human-readable implementation label, surfaced in status rows' Detail so
// an operator immediately knows which controller a failing GatewayClass
// belongs to. Values are the real controllerName strings each project
// documents for its GatewayClass.
var controllerLabels = map[string]string{
	"gateway.envoyproxy.io/gatewayclass-controller":         "Envoy Gateway",
	"gateway.nginx.org/nginx-gateway-controller":            "NGINX Gateway Fabric",
	"projectcontour.io/gateway-controller":                  "Contour",
	"istio.io/gateway-controller":                           "Istio",
	"konghq.com/kic-gateway-controller":                     "Kong Ingress Controller",
	"traefik.io/gateway-controller":                         "Traefik",
	"networking.gke.io/gateway":                             "GKE Gateway",
	"application-networking.k8s.aws/gateway-api-controller": "AWS Gateway API Controller (VPC Lattice)",
	"alb.ingress.k8s.aws/gateway-controller":                "AWS Load Balancer Controller",
	"linkerd.io/gateway-api":                                "Linkerd",
}

// controllerLabel returns a human label for a GatewayClass controllerName,
// falling back to the raw string if it isn't one of the known
// implementations above.
func controllerLabel(controllerName string) string {
	if label, ok := controllerLabels[controllerName]; ok {
		return label
	}
	return controllerName
}

// ScanStatus checks every GatewayClass and Gateway's status conditions and
// returns one Row per resource whose Accepted/Programmed/Ready condition is
// False, mirroring how pkg/certmanager flags a not-Ready Issuer or
// ClusterIssuer. If the Gateway API CRDs aren't installed, returns an empty
// result and no error.
func ScanStatus(ctx context.Context, cluster string, client gatewayclientset.Interface, kubeClient kubernetes.Interface) ([]output.Row, error) {
	if client == nil || !CRDInstalled(kubeClient) {
		return nil, nil
	}

	v1 := client.GatewayV1()
	var rows []output.Row

	classes, err := v1.GatewayClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing gatewayclasses on %s: %w", cluster, err)
	}
	classLabel := make(map[string]string, len(classes.Items))
	for _, gc := range classes.Items {
		classLabel[gc.Name] = controllerLabel(string(gc.Spec.ControllerName))
		if ok, reason, msg := failedCondition(gc.Status.Conditions, "Accepted"); !ok {
			rows = append(rows, output.Row{
				Source:    "gateway-status",
				Cluster:   cluster,
				Namespace: "",
				Name:      "GatewayClass/" + gc.Name,
				Status:    "error",
				Detail:    fmt.Sprintf("controller %s: condition Accepted=False (%s): %s", controllerLabel(string(gc.Spec.ControllerName)), reason, msg),
			})
		}
	}

	gws, err := v1.Gateways("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing gateways on %s: %w", cluster, err)
	}
	for _, gw := range gws.Items {
		label := classLabel[string(gw.Spec.GatewayClassName)]
		for _, condType := range []string{"Accepted", "Programmed"} {
			if ok, reason, msg := failedCondition(gw.Status.Conditions, condType); !ok {
				detail := fmt.Sprintf("condition %s=False (%s): %s", condType, reason, msg)
				if label != "" {
					detail = fmt.Sprintf("controller %s: %s", label, detail)
				}
				rows = append(rows, output.Row{
					Source:    "gateway-status",
					Cluster:   cluster,
					Namespace: gw.Namespace,
					Name:      "Gateway/" + gw.Name,
					Status:    "error",
					Detail:    detail,
				})
			}
		}
	}

	return rows, nil
}

// failedCondition reports whether conditions carries an entry of the given
// type set to True (ok=true), or explains why not: a False/Unknown status
// (ok=false, with its reason/message), or the condition being entirely
// absent (ok=false, reason "Missing" — an implementation that hasn't
// reported this condition yet is not the same as one reporting success).
func failedCondition(conditions []metav1.Condition, condType string) (ok bool, reason, message string) {
	for _, c := range conditions {
		if c.Type != condType {
			continue
		}
		if c.Status == "True" {
			return true, "", ""
		}
		return false, c.Reason, c.Message
	}
	return false, "Missing", fmt.Sprintf("no %s condition reported", condType)
}
