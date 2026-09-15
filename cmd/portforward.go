package cmd

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/thev1ndu/certhealthz/pkg/gateway"
	"github.com/thev1ndu/certhealthz/pkg/ingress"
	"github.com/thev1ndu/certhealthz/pkg/portforward"
)

// resolveIngressControllerService and resolveGatewayControllerService
// resolve the Service fronting the controller that would actually receive
// traffic for a route with no published LB/Gateway address, and
// attemptPortForward tunnels a local port to it — the fallback path for
// ClusterIP-only routes (e.g. a bare-metal cluster with no LoadBalancer
// support).
//
// This is deliberately scoped to exactly two controllers, chosen by the
// user rather than attempted for every GatewayClass/IngressClass this
// codebase otherwise recognizes (see pkg/gateway/status.go's
// controllerLabels): Envoy Gateway and ingress-nginx. Every other
// controller gets errUnsupportedController rather than a guessed Service
// name — a wrong guess would silently test the wrong thing.

// errUnsupportedController is returned by resolveIngressControllerService /
// resolveGatewayControllerService for any controller outside that list.
var errUnsupportedController = errors.New("unsupported controller for port-forward testing")

// envoyGatewayControllerName is the real controllerName Envoy Gateway's
// GatewayClass carries (see pkg/gateway/status.go's controllerLabels,
// which already maps this same string to "Envoy Gateway" for display).
const envoyGatewayControllerName = "gateway.envoyproxy.io/gatewayclass-controller"

// ingressNginxControllerName is the real controllerName ingress-nginx's
// IngressClass carries.
const ingressNginxControllerName = "k8s.io/ingress-nginx"

// resolveGatewayControllerService finds the Service Envoy Gateway generated
// for a route's Gateway, via the owning-gateway labels Envoy Gateway
// stamps onto it (gateway.envoyproxy.io/owning-gateway-name and
// -owning-gateway-namespace) — confirmed against Envoy Gateway's own docs
// (kubectl get svc --selector=gateway.envoyproxy.io/owning-gateway-namespace=...,gateway.envoyproxy.io/owning-gateway-name=...
// is exactly how its own "Gateway Address" doc page tells operators to find
// this Service), rather than guessing at a naming convention.
func resolveGatewayControllerService(ctx context.Context, cc ClusterClients, route gateway.Route) (namespace, name, controllerName string, err error) {
	if cc.Gateway == nil {
		return "", "", "", fmt.Errorf("no Gateway API client available")
	}
	v1 := cc.Gateway.GatewayV1()

	gw, err := v1.Gateways(route.GatewayNamespace).Get(ctx, route.GatewayName, metav1.GetOptions{})
	if err != nil {
		return "", "", "", fmt.Errorf("resolving Gateway %s/%s: %w", route.GatewayNamespace, route.GatewayName, err)
	}
	gc, err := v1.GatewayClasses().Get(ctx, string(gw.Spec.GatewayClassName), metav1.GetOptions{})
	if err != nil {
		return "", "", "", fmt.Errorf("resolving GatewayClass %s: %w", gw.Spec.GatewayClassName, err)
	}
	controllerName = string(gc.Spec.ControllerName)

	if controllerName != envoyGatewayControllerName {
		return "", "", controllerName, fmt.Errorf("%w: %q", errUnsupportedController, controllerName)
	}
	if cc.Typed == nil {
		return "", "", controllerName, fmt.Errorf("no typed client available to look up the Envoy Gateway Service")
	}

	selector := fmt.Sprintf(
		"gateway.envoyproxy.io/owning-gateway-namespace=%s,gateway.envoyproxy.io/owning-gateway-name=%s",
		route.GatewayNamespace, route.GatewayName,
	)
	svcs, err := cc.Typed.CoreV1().Services("").List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", "", controllerName, fmt.Errorf("listing Envoy Gateway Services: %w", err)
	}
	if len(svcs.Items) == 0 {
		return "", "", controllerName, fmt.Errorf("no Service found for Envoy Gateway %s/%s (selector %q)", route.GatewayNamespace, route.GatewayName, selector)
	}
	svc := svcs.Items[0]
	return svc.Namespace, svc.Name, controllerName, nil
}

// resolveIngressControllerService finds the Service fronting ingress-nginx
// for a route's Ingress. It resolves the Ingress's actual IngressClass (its
// own spec.ingressClassName, falling back to the cluster's default
// IngressClass if unset — matching how the ingress-nginx admission
// controller itself resolves an unset class) and checks that class's real
// controller string before trusting anything about it.
func resolveIngressControllerService(ctx context.Context, cc ClusterClients, route ingress.Route) (namespace, name, controllerName string, err error) {
	if cc.Typed == nil {
		return "", "", "", fmt.Errorf("no typed client available")
	}

	ing, err := cc.Typed.NetworkingV1().Ingresses(route.Namespace).Get(ctx, route.Ingress, metav1.GetOptions{})
	if err != nil {
		return "", "", "", fmt.Errorf("resolving Ingress %s/%s: %w", route.Namespace, route.Ingress, err)
	}

	className := ""
	if ing.Spec.IngressClassName != nil {
		className = *ing.Spec.IngressClassName
	} else if v, ok := ing.Annotations["kubernetes.io/ingress.class"]; ok {
		className = v
	}

	classes, err := cc.Typed.NetworkingV1().IngressClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", "", "", fmt.Errorf("listing IngressClasses: %w", err)
	}

	var class *networkingIngressClass
	for i := range classes.Items {
		ic := &classes.Items[i]
		if className != "" && ic.Name == className {
			class = &networkingIngressClass{Name: ic.Name, Controller: ic.Spec.Controller}
			break
		}
		if className == "" && ic.Annotations["ingressclass.kubernetes.io/is-default-class"] == "true" {
			class = &networkingIngressClass{Name: ic.Name, Controller: ic.Spec.Controller}
		}
	}
	if class == nil {
		return "", "", "", fmt.Errorf("could not resolve an IngressClass for Ingress %s/%s (ingressClassName %q)", route.Namespace, route.Ingress, className)
	}
	controllerName = class.Controller

	if controllerName != ingressNginxControllerName {
		return "", "", controllerName, fmt.Errorf("%w: %q", errUnsupportedController, controllerName)
	}

	// Prefer the label selector ingress-nginx's own install manifests use
	// over a hardcoded Service name, since a cluster can rename the
	// release/namespace: app.kubernetes.io/name=ingress-nginx +
	// app.kubernetes.io/component=controller.
	svcs, err := cc.Typed.CoreV1().Services("").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=ingress-nginx,app.kubernetes.io/component=controller",
	})
	if err == nil && len(svcs.Items) > 0 {
		svc := svcs.Items[0]
		return svc.Namespace, svc.Name, controllerName, nil
	}

	// Fall back to the conventional name/namespace ingress-nginx's own
	// quickstart docs install it under, for installs that dropped those
	// labels.
	if svc, getErr := cc.Typed.CoreV1().Services("ingress-nginx").Get(ctx, "ingress-nginx-controller", metav1.GetOptions{}); getErr == nil {
		return svc.Namespace, svc.Name, controllerName, nil
	}

	return "", "", controllerName, fmt.Errorf("could not locate the ingress-nginx controller Service (checked label selector and the conventional ingress-nginx/ingress-nginx-controller name)")
}

// networkingIngressClass is the small subset of a networking.k8s.io/v1
// IngressClass resolveIngressControllerService needs, kept as its own type
// only so the loop above doesn't have to juggle a pointer into the list
// items directly.
type networkingIngressClass struct {
	Name       string
	Controller string
}

// attemptPortForward resolves the target port on namespace/serviceName and
// opens a port-forward to a ready pod behind it (see pkg/portforward),
// returning the local address the synthetic route test should dial
// instead of the unreachable ClusterIP.
func attemptPortForward(ctx context.Context, cc ClusterClients, namespace, serviceName string) (localAddr string, stop func(), err error) {
	if cc.Rest == nil || cc.Typed == nil {
		return "", nil, fmt.Errorf("no REST config available for port-forward")
	}

	targetPort, err := portforward.ResolvePort(ctx, cc.Typed, namespace, serviceName)
	if err != nil {
		return "", nil, err
	}

	return portforward.Dial(ctx, cc.Rest, cc.Typed, namespace, serviceName, targetPort)
}
