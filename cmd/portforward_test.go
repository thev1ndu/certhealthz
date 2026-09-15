package cmd

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/thev1ndu/certhealthz/pkg/gateway"
	"github.com/thev1ndu/certhealthz/pkg/ingress"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

// newFakeGatewayClient builds a Gateway API fake clientset with the given
// GatewayClasses/Gateways created through the client itself (Create), not
// pre-seeded via NewSimpleClientset(objects...) — the generated gentype-
// based fake for this clientset silently drops pre-seeded objects (its
// object tracker never surfaces them to List/Get), so tests must Create
// through the client the way a real caller would instead.
func newFakeGatewayClient(t *testing.T, classes []*gatewayv1.GatewayClass, gateways []*gatewayv1.Gateway) *gatewayfake.Clientset {
	t.Helper()
	client := gatewayfake.NewSimpleClientset()
	ctx := context.Background()
	for _, gc := range classes {
		if _, err := client.GatewayV1().GatewayClasses().Create(ctx, gc, metav1.CreateOptions{}); err != nil {
			t.Fatalf("creating GatewayClass %s: %v", gc.Name, err)
		}
	}
	for _, gw := range gateways {
		if _, err := client.GatewayV1().Gateways(gw.Namespace).Create(ctx, gw, metav1.CreateOptions{}); err != nil {
			t.Fatalf("creating Gateway %s/%s: %v", gw.Namespace, gw.Name, err)
		}
	}
	return client
}

func TestResolveGatewayControllerService_EnvoyGateway(t *testing.T) {
	gwClient := newFakeGatewayClient(t,
		[]*gatewayv1.GatewayClass{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "eg"},
				Spec:       gatewayv1.GatewayClassSpec{ControllerName: envoyGatewayControllerName},
			},
		},
		[]*gatewayv1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "my-gw", Namespace: "default"},
				Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
			},
		},
	)
	typed := kubernetesfake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "envoy-my-gw-abc123",
			Namespace: "envoy-gateway-system",
			Labels: map[string]string{
				"gateway.envoyproxy.io/owning-gateway-name":      "my-gw",
				"gateway.envoyproxy.io/owning-gateway-namespace": "default",
			},
		},
	})

	cc := ClusterClients{Typed: typed, Gateway: gwClient}
	route := gateway.Route{GatewayName: "my-gw", GatewayNamespace: "default"}

	ns, name, controller, err := resolveGatewayControllerService(context.Background(), cc, route)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if controller != envoyGatewayControllerName {
		t.Errorf("expected controller %q, got %q", envoyGatewayControllerName, controller)
	}
	if ns != "envoy-gateway-system" || name != "envoy-my-gw-abc123" {
		t.Errorf("expected envoy-gateway-system/envoy-my-gw-abc123, got %s/%s", ns, name)
	}
}

func TestResolveGatewayControllerService_UnsupportedController(t *testing.T) {
	gwClient := newFakeGatewayClient(t,
		[]*gatewayv1.GatewayClass{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "istio"},
				Spec:       gatewayv1.GatewayClassSpec{ControllerName: "istio.io/gateway-controller"},
			},
		},
		[]*gatewayv1.Gateway{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "my-gw", Namespace: "default"},
				Spec:       gatewayv1.GatewaySpec{GatewayClassName: "istio"},
			},
		},
	)
	cc := ClusterClients{Typed: kubernetesfake.NewSimpleClientset(), Gateway: gwClient}
	route := gateway.Route{GatewayName: "my-gw", GatewayNamespace: "default"}

	_, _, controller, err := resolveGatewayControllerService(context.Background(), cc, route)
	if !errors.Is(err, errUnsupportedController) {
		t.Fatalf("expected errUnsupportedController, got %v", err)
	}
	if controller != "istio.io/gateway-controller" {
		t.Errorf("expected the real controller name in the return value even on failure, got %q", controller)
	}
}

func TestResolveIngressControllerService_IngressNginx(t *testing.T) {
	className := "nginx"
	typed := kubernetesfake.NewSimpleClientset(
		&networkingv1.IngressClass{
			ObjectMeta: metav1.ObjectMeta{Name: "nginx"},
			Spec:       networkingv1.IngressClassSpec{Controller: ingressNginxControllerName},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ingress", Namespace: "apps"},
			Spec:       networkingv1.IngressSpec{IngressClassName: &className},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingress-nginx-controller",
				Namespace: "ingress-nginx",
				Labels: map[string]string{
					"app.kubernetes.io/name":      "ingress-nginx",
					"app.kubernetes.io/component": "controller",
				},
			},
		},
	)
	cc := ClusterClients{Typed: typed}
	route := ingress.Route{Namespace: "apps", Ingress: "my-ingress"}

	ns, name, controller, err := resolveIngressControllerService(context.Background(), cc, route)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if controller != ingressNginxControllerName {
		t.Errorf("expected controller %q, got %q", ingressNginxControllerName, controller)
	}
	if ns != "ingress-nginx" || name != "ingress-nginx-controller" {
		t.Errorf("expected ingress-nginx/ingress-nginx-controller, got %s/%s", ns, name)
	}
}

func TestResolveIngressControllerService_FallsBackToConventionalName(t *testing.T) {
	className := "nginx"
	typed := kubernetesfake.NewSimpleClientset(
		&networkingv1.IngressClass{
			ObjectMeta: metav1.ObjectMeta{Name: "nginx"},
			Spec:       networkingv1.IngressClassSpec{Controller: ingressNginxControllerName},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ingress", Namespace: "apps"},
			Spec:       networkingv1.IngressSpec{IngressClassName: &className},
		},
		// No labels this time — only the conventional name/namespace.
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "ingress-nginx-controller", Namespace: "ingress-nginx"},
		},
	)
	cc := ClusterClients{Typed: typed}
	route := ingress.Route{Namespace: "apps", Ingress: "my-ingress"}

	ns, name, _, err := resolveIngressControllerService(context.Background(), cc, route)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "ingress-nginx" || name != "ingress-nginx-controller" {
		t.Errorf("expected ingress-nginx/ingress-nginx-controller, got %s/%s", ns, name)
	}
}

func TestResolveIngressControllerService_UnsupportedController(t *testing.T) {
	className := "traefik"
	typed := kubernetesfake.NewSimpleClientset(
		&networkingv1.IngressClass{
			ObjectMeta: metav1.ObjectMeta{Name: "traefik"},
			Spec:       networkingv1.IngressClassSpec{Controller: "traefik.io/ingress-controller"},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ingress", Namespace: "apps"},
			Spec:       networkingv1.IngressSpec{IngressClassName: &className},
		},
	)
	cc := ClusterClients{Typed: typed}
	route := ingress.Route{Namespace: "apps", Ingress: "my-ingress"}

	_, _, controller, err := resolveIngressControllerService(context.Background(), cc, route)
	if !errors.Is(err, errUnsupportedController) {
		t.Fatalf("expected errUnsupportedController, got %v", err)
	}
	if controller != "traefik.io/ingress-controller" {
		t.Errorf("expected the real controller name, got %q", controller)
	}
}

func TestResolveIngressControllerService_DefaultClassWhenUnset(t *testing.T) {
	typed := kubernetesfake.NewSimpleClientset(
		&networkingv1.IngressClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "nginx",
				Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"},
			},
			Spec: networkingv1.IngressClassSpec{Controller: ingressNginxControllerName},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "my-ingress", Namespace: "apps"},
			// No IngressClassName set at all.
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ingress-nginx-controller",
				Namespace: "ingress-nginx",
				Labels: map[string]string{
					"app.kubernetes.io/name":      "ingress-nginx",
					"app.kubernetes.io/component": "controller",
				},
			},
		},
	)
	cc := ClusterClients{Typed: typed}
	route := ingress.Route{Namespace: "apps", Ingress: "my-ingress"}

	_, _, controller, err := resolveIngressControllerService(context.Background(), cc, route)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if controller != ingressNginxControllerName {
		t.Errorf("expected the default IngressClass's controller to be used, got %q", controller)
	}
}
